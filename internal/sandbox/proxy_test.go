package sandbox_test

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Betree/Orcasm/internal/sandbox"
)

// proxyExec is Provision's root exec starting the proxy.
func proxyExec(t *testing.T) execCall {
	t.Helper()
	f := &fakeCreateMSB{}
	if err := sandbox.Provision(context.Background(), f, demoConfig(), hostValues, io.Discard); err != nil {
		t.Fatal(err)
	}
	i := slices.IndexFunc(f.execs, func(c execCall) bool {
		return slices.Contains(c.argv, sandbox.ProxyImage)
	})
	if i < 0 {
		t.Fatalf("no exec runs %s: %+v", sandbox.ProxyImage, f.execs)
	}
	return f.execs[i]
}

// fakeDockerDaemon is a docker CLI on PATH that logs each call (one line,
// args in brackets) and, like the daemon, refuses to run a second container
// under a name still in use.
func fakeDockerDaemon(t *testing.T) (bin, log string) {
	t.Helper()
	dir := t.TempDir()
	bin, log = filepath.Join(dir, "bin"), filepath.Join(dir, "calls")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(bin, "docker"), `#!/bin/bash
printf '[%s]' "$@" >>"`+log+`"; echo >>"`+log+`"
names="`+dir+`/names"
touch "$names"
case $1 in
  rm) [ "$2" = --force ] && grep -vxF "$3" "$names" >"$names.new"; mv "$names.new" "$names" ;;
  run)
    while [ $# -gt 0 ]; do
      if [ "$1" = --name ]; then
        if grep -qxF "$2" "$names"; then echo "Conflict: name $2 in use" >&2; exit 125; fi
        echo "$2" >>"$names"
      fi
      shift
    done ;;
esac
`, 0o755)
	return bin, log
}

// runWithPath runs argv with bin first on PATH, for argv[0] too: never the
// host's real docker.
func runWithPath(bin string, argv []string) *exec.Cmd {
	cmd := exec.Command(argv[0], argv[1:]...)
	if p := filepath.Join(bin, argv[0]); !strings.Contains(argv[0], "/") {
		if _, err := os.Stat(p); err == nil {
			cmd.Path, cmd.Err = p, nil
		}
	}
	cmd.Env = append(os.Environ(), "PATH="+bin+":"+os.Getenv("PATH"))
	return cmd
}

func TestProvisionStartsTheWorktreeProxyAsRoot(t *testing.T) {
	c := proxyExec(t)
	if c.name != "demo" || c.user != "" {
		t.Errorf("exec in %q as %q, want demo as root", c.name, c.user)
	}
	bin, log := fakeDockerDaemon(t)
	if out, err := runWithPath(bin, c.argv).CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	raw, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	var run string
	for _, l := range strings.Split(string(raw), "\n") {
		if strings.HasPrefix(l, "[run]") {
			run = l
		}
	}
	for _, want := range []string{
		"[run][--detach][--name][orcasm-proxy]",
		// Back after every Sandbox start, with no unit of its own.
		"[--restart][always]",
		// Reaches every Stack's network and serves the Sandbox's port 80.
		"[--network][host]",
		"[/var/run/docker.sock:/var/run/docker.sock:ro]",
		"[" + sandbox.ProxyImage + "]",
		"[--entrypoints.web.address=:80]",
		// Only the web services that orcasm's Stack override labels.
		"[--providers.docker.constraints=Label(`orcasm.proxy`,`true`)]",
	} {
		if !strings.Contains(run, want) {
			t.Errorf("proxy started with %q, lacking %q", run, want)
		}
	}
	if !strings.HasPrefix(sandbox.ProxyImage, "traefik:v") || strings.Count(sandbox.ProxyImage, ".") != 2 {
		t.Errorf("ProxyImage = %q, want an exact traefik version", sandbox.ProxyImage)
	}
}

// Provisioning steps may run again on an existing Sandbox (a retried
// provision, a reconciliation): the proxy is then replaced, not refused.
func TestStartingTheProxyAgainReplacesTheRunningOne(t *testing.T) {
	c := proxyExec(t)
	bin, log := fakeDockerDaemon(t)
	for i := 0; i < 2; i++ {
		if out, err := runWithPath(bin, c.argv).CombinedOutput(); err != nil {
			t.Fatalf("start %d: %v: %s", i+1, err, out)
		}
	}
	raw, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(raw), "[run]"); n != 2 {
		t.Errorf("%d docker runs, want 2:\n%s", n, raw)
	}
}
