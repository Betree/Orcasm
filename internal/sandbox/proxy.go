package sandbox

// The Worktree proxy: one Traefik container in the Sandbox serving
// http://<worktree>.localhost (port 80, any interface of the guest) for
// Orca's browser and anything else running in the Sandbox. It discovers the
// Stacks itself through Docker: the Stack override (internal/stack) labels
// each Worktree's web service with its hostname and port, so starting or
// stopping a Stack is all it takes to add or drop a route. Its host network
// reaches every Stack's network without joining them, so web ports stay
// unpublished.

import (
	"github.com/Betree/Orcasm/internal/stack"
)

const (
	// ProxyImage is the pinned Traefik release run as the proxy.
	ProxyImage = "traefik:v3.7.12"
	// ProxyContainer is the proxy's Docker container name.
	ProxyContainer = "orcasm-proxy"
)

// proxyArgv is the root command starting the proxy. Docker's restart policy
// brings it back on every Sandbox start.
func proxyArgv() []string {
	return []string{"docker", "run", "--detach", "--name", ProxyContainer,
		"--restart", "always",
		"--network", "host",
		"--volume", "/var/run/docker.sock:/var/run/docker.sock:ro",
		ProxyImage,
		"--entrypoints.web.address=:80",
		"--providers.docker=true",
		"--providers.docker.exposedbydefault=false",
		"--providers.docker.constraints=Label(`" + stack.ProxyLabel + "`,`true`)",
		"--global.checknewversion=false",
		"--global.sendanonymoususage=false",
	}
}
