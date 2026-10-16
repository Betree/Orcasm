package hookrelay_test

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/Betree/Orcasm/internal/hookrelay"
)

// bridge stands in for Docker's bridge address: another loopback address,
// so the relay and the fake Orca can share a port number as in the Sandbox.
const bridge = "127.0.0.2"

// orca is a fake Orca hook server on the loopback only, answering each line
// with its name and the line.
func orca(t *testing.T, name string) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				line, _ := bufio.NewReader(c).ReadString('\n')
				fmt.Fprintf(c, "%s got %s", name, line)
			}()
		}
	}()
	return l.Addr().(*net.TCPAddr).Port
}

func writeEndpoint(t *testing.T, path string, port int) {
	t.Helper()
	body := "ORCA_AGENT_HOOK_PORT=" + strconv.Itoa(port) + "\nORCA_AGENT_HOOK_TOKEN=tok\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func start(t *testing.T, endpoint string) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- hookrelay.Relay{
			Endpoint: endpoint,
			Bridge:   func(context.Context) (string, error) { return bridge, nil },
			Log:      io.Discard,
			Poll:     10 * time.Millisecond,
		}.Run(ctx)
	}()
	t.Cleanup(func() {
		cancel()
		if err := <-done; err != nil && err != context.Canceled {
			t.Errorf("Run: %v", err)
		}
	})
}

// post sends a line to the bridge at port, retrying while the relay comes
// up, and returns the answer.
func post(t *testing.T, port int) string {
	t.Helper()
	addr := net.JoinHostPort(bridge, strconv.Itoa(port))
	deadline := time.Now().Add(3 * time.Second)
	for {
		c, err := net.Dial("tcp", addr)
		if err == nil {
			defer c.Close()
			fmt.Fprintln(c, "hook")
			out, _ := io.ReadAll(c)
			return string(out)
		}
		if time.Now().After(deadline) {
			t.Fatalf("nothing listens on %s: %v", addr, err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestHooksPostedToTheBridgeReachOrcasLoopbackServer(t *testing.T) {
	endpoint := filepath.Join(t.TempDir(), "endpoint.env")
	port := orca(t, "orca")
	writeEndpoint(t, endpoint, port)
	start(t, endpoint)

	if got := post(t, port); got != "orca got hook\n" {
		t.Errorf("got %q", got)
	}
}

func TestTheRelayFollowsOrcaToItsNewPortAfterARestart(t *testing.T) {
	endpoint := filepath.Join(t.TempDir(), "endpoint.env")
	// Orca not started yet: no endpoint file.
	start(t, endpoint)
	first := orca(t, "first")
	writeEndpoint(t, endpoint, first)
	if got := post(t, first); got != "first got hook\n" {
		t.Fatalf("got %q", got)
	}

	second := orca(t, "second")
	writeEndpoint(t, endpoint, second)
	if got := post(t, second); got != "second got hook\n" {
		t.Errorf("got %q", got)
	}
	// The old port is released.
	deadline := time.Now().Add(3 * time.Second)
	for {
		c, err := net.Dial("tcp", net.JoinHostPort(bridge, strconv.Itoa(first)))
		if err != nil {
			break
		}
		c.Close()
		if time.Now().After(deadline) {
			t.Fatal("the relay still listens on Orca's old port")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
