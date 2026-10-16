// Package hookrelay lets agents in Workbenches report their status to Orca.
//
// Orca's agent hook server listens on the Sandbox's loopback only, on a port
// that changes each time Orca starts. Agent hook scripts in a Workbench POST
// to host.docker.internal (Docker's bridge gateway) on that port instead;
// the relay, a systemd unit in the Sandbox, listens there and forwards to
// the loopback, following Orca to its new port after a restart. The hook
// token still authenticates every POST.
package hookrelay

import (
	"bufio"
	"context"
	"fmt"
	"github.com/Betree/Orcasm/internal/stack"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Relay forwards Bridge:<port> to 127.0.0.1:<port>, <port> being the one in
// Orca's Endpoint file.
type Relay struct {
	// Endpoint is Orca's endpoint.env (orca.Runtime.HookEndpointFile).
	Endpoint string
	// Bridge returns the address to listen on (DockerBridge).
	Bridge func(ctx context.Context) (string, error)
	// Log gets a line per listener change or failure.
	Log io.Writer
	// Poll is how often the Endpoint file is checked (0 = 2s).
	Poll time.Duration
}

// Run relays until ctx is done, then returns ctx's error.
func (r Relay) Run(ctx context.Context) error {
	poll := r.Poll
	if poll == 0 {
		poll = 2 * time.Second
	}
	var (
		l       net.Listener
		current string // "bridge:port" l listens on
	)
	defer func() {
		if l != nil {
			l.Close()
		}
	}()
	for {
		if want, err := r.address(ctx); err != nil {
			fmt.Fprintf(r.Log, "hook relay: %v\n", err)
		} else if want != current {
			if l != nil {
				l.Close()
				l, current = nil, ""
			}
			if want != "" {
				nl, err := net.Listen("tcp", want)
				if err != nil {
					// The bridge may not be up yet: retry next time.
					fmt.Fprintf(r.Log, "hook relay: %v\n", err)
				} else {
					fmt.Fprintf(r.Log, "hook relay: %s -> Orca\n", want)
					l, current = nl, want
					_, port, _ := net.SplitHostPort(want)
					go serve(nl, net.JoinHostPort("127.0.0.1", port))
				}
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(poll):
		}
	}
}

// address is where to listen now: "" while Orca has written no port.
func (r Relay) address(ctx context.Context) (string, error) {
	port, err := endpointPort(r.Endpoint)
	if err != nil || port == "" {
		return "", err
	}
	ip, err := r.Bridge(ctx)
	if err != nil {
		return "", err
	}
	return net.JoinHostPort(ip, port), nil
}

// endpointPort reads ORCA_AGENT_HOOK_PORT from Orca's endpoint file; ""
// if there is none yet.
func endpointPort(path string) (string, error) {
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return "", nil
	} else if err != nil {
		return "", err
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	for s.Scan() {
		if v, ok := strings.CutPrefix(s.Text(), "ORCA_AGENT_HOOK_PORT="); ok {
			if _, err := strconv.ParseUint(v, 10, 16); err != nil {
				return "", fmt.Errorf("%s: bad port %q", path, v)
			}
			return v, nil
		}
	}
	return "", s.Err()
}

// serve forwards each connection accepted on l to target, until l closes.
func serve(l net.Listener, target string) {
	for {
		c, err := l.Accept()
		if err != nil {
			return
		}
		go forward(c, target)
	}
}

func forward(c net.Conn, target string) {
	defer c.Close()
	t, err := net.DialTimeout("tcp", target, 5*time.Second)
	if err != nil {
		return
	}
	defer t.Close()
	var wg sync.WaitGroup
	wg.Add(2)
	pipe := func(dst, src net.Conn) {
		defer wg.Done()
		io.Copy(dst, src)
		if tc, ok := dst.(*net.TCPConn); ok {
			tc.CloseWrite()
		}
	}
	go pipe(t, c)
	go pipe(c, t)
	wg.Wait()
}

// DockerBridge finds, through docker, the gateway address of Docker's
// default bridge network, which host.docker.internal (host-gateway)
// resolves to in containers.
func DockerBridge(docker stack.Docker) func(ctx context.Context) (string, error) {
	return func(ctx context.Context) (string, error) {
		out, err := docker(ctx, "network", "inspect", "bridge", "--format", "{{range .IPAM.Config}}{{.Gateway}} {{end}}")
		if err != nil {
			return "", fmt.Errorf("finding Docker's bridge address: %w", err)
		}
		for _, f := range strings.Fields(string(out)) {
			if ip := net.ParseIP(f); ip != nil && ip.To4() != nil {
				return f, nil
			}
		}
		return "", fmt.Errorf("Docker's bridge network has no IPv4 gateway (%q)", strings.TrimSpace(string(out)))
	}
}
