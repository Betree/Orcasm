package sandbox_test

import (
	"context"
	"io"
	"slices"
	"strings"
	"testing"

	"github.com/Betree/Orcasm/internal/sandbox"
)

func TestProvisionStartsTheWorktreeProxyAsRoot(t *testing.T) {
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
	c := f.execs[i]
	if c.name != "demo" || c.user != "" {
		t.Errorf("exec in %q as %q, want demo as root", c.name, c.user)
	}
	argv := strings.Join(c.argv, " ")
	for _, want := range []string{
		"docker run --detach --name orcasm-proxy",
		// Back after every Sandbox start, with no unit of its own.
		"--restart always",
		// Reaches every Stack's network and serves the Sandbox's port 80.
		"--network host",
		"/var/run/docker.sock:/var/run/docker.sock:ro",
		"--entrypoints.web.address=:80",
		// Only the web services that orcasm's Stack override labels.
		"--providers.docker.constraints=Label(`orcasm.proxy`,`true`)",
	} {
		if !strings.Contains(argv, want) {
			t.Errorf("proxy started with %q, lacking %q", argv, want)
		}
	}
	if !strings.HasPrefix(sandbox.ProxyImage, "traefik:v") || strings.Count(sandbox.ProxyImage, ".") != 2 {
		t.Errorf("ProxyImage = %q, want an exact traefik version", sandbox.ProxyImage)
	}
}
