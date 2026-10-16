// Package stack generates and drives the Stack of one Worktree: the
// Project's Compose files, read from the Worktree's checkouts, plus an
// orcasm override that unpublishes every port and sets up the Workbench.
//
// It runs inside the Sandbox, as the Sandbox user: the paths in a Spec are
// Sandbox paths, and Generate creates the shared harness config dirs there.
package stack

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	// CABundle is where the Workbench finds the Sandbox's CA bundle, which
	// holds msb's TLS-interception CA along with the public ones.
	CABundle = "/etc/orcasm/ca-certificates.crt"
	// sandboxCABundle is the Sandbox's system bundle (see provision.sh).
	sandboxCABundle = "/etc/ssl/certs/ca-certificates.crt"
	// HarnessLayer is the Sandbox directory holding the agent harnesses
	// for Workbenches, mounted read-only at the same path.
	HarnessLayer = "/opt/orcasm/harness"
	// GenericWorkbench is the service added when the Project names no
	// Workbench, running GenericImage (built from genericDockerfile).
	GenericWorkbench = "orcasm-workbench"
	GenericImage     = "orcasm-workbench:24.04"
	// overrideName is the generated file, written in Generate's dir.
	overrideName = "compose.orcasm.yaml"
)

// genericDockerfile builds GenericImage: what agents and Orca's hook
// scripts (sh, curl, base64, tr, awk, sed) need, on glibc for Claude Code.
const genericDockerfile = `FROM ubuntu:24.04
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates curl git less openssh-client \
 && rm -rf /var/lib/apt/lists/*
`

// share is a directory of the Sandbox user's home given to the Workbench;
// paths are relative to the home directory.
type share struct {
	src, dst string // dst "" = src
	ro       bool
}

// The Workbench gets only what its agents need from the Sandbox user's home,
// not the whole of it: the harnesses' config, Orca's per-agent plumbing and
// the git identity. Orca's own state (~/.config/orca: its runtime socket
// and token, which drive Orca and so the whole Sandbox) stays out.
var (
	// harnessShares are each harness's config, shared read-write with Main
	// and every Workbench, plus what Orca prepares per launch.
	harnessShares = map[string][]share{
		"claude": {{src: ".claude"}},
		// Orca's managed Codex home, should it set CODEX_HOME to it.
		"codex":    {{src: ".codex"}, {src: ".config/orca/codex-runtime-home"}},
		"opencode": {{src: ".config/opencode"}, {src: ".local/share/opencode"}, {src: OpenCodeConfigCopy, ro: true}},
	}
	// hookShares let any harness's hooks reach Orca: its endpoint file and
	// spool, and the wrapper's rewritten hook scripts where Orca's settings
	// call them.
	hookShares = []share{
		{src: ".config/orca/agent-hooks"},
		{src: HookScriptsCopy, dst: ".orca/agent-hooks", ro: true},
	}
	// homeShares are given to every Workbench.
	homeShares = []share{{src: ".config/git"}}
)

const (
	// HookScriptsCopy is where the agent wrapper writes its Workbench copy
	// of Orca's hook scripts (~/.orca/agent-hooks), relative to the home.
	HookScriptsCopy = ".cache/orcasm/agent-hooks"
	// OpenCodeConfigCopy is where the agent wrapper copies OpenCode's
	// per-launch config overlay, relative to the home.
	OpenCodeConfigCopy = ".cache/orcasm/opencode-config"
)

// shares are the home directories spec's Workbench gets.
func shares(spec Spec) []share {
	out := append([]share(nil), homeShares...)
	if len(spec.Harnesses) > 0 {
		out = append(out, hookShares...)
	}
	for _, h := range spec.Harnesses {
		out = append(out, harnessShares[h]...)
	}
	return out
}

// hostGateway names the Sandbox from the Workbench: Docker's bridge
// gateway, where the hook relay listens (internal/hookrelay).
const hostGateway = "host.docker.internal:host-gateway"

// caEnv are the variables pointing tools with their own CA list at
// CABundle.
var caEnv = []string{"SSL_CERT_FILE", "NODE_EXTRA_CA_CERTS", "CURL_CA_BUNDLE", "REQUESTS_CA_BUNDLE", "GIT_SSL_CAINFO"}

// Docker runs `docker args...` and returns its stdout.
type Docker func(ctx context.Context, args ...string) ([]byte, error)

// ExecDocker runs the docker CLI on PATH; stderr goes in the error.
func ExecDocker(ctx context.Context, args ...string) ([]byte, error) {
	var stdout, stderr bytes.Buffer
	err := runDocker(ctx, &stdout, &stderr, args)
	if err != nil {
		err = fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), err
}

// StreamDocker runs the docker CLI on PATH with its output (stdout and
// stderr) going to w, e.g. Compose's build progress to a log; it returns no
// output.
func StreamDocker(w io.Writer) Docker {
	return func(ctx context.Context, args ...string) ([]byte, error) {
		return nil, runDocker(ctx, w, w, args)
	}
}

func runDocker(ctx context.Context, stdout, stderr io.Writer, args []string) error {
	cmd := exec.CommandContext(ctx, "docker", args...)
	cmd.Stdout, cmd.Stderr = stdout, stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker %s: %w", strings.Join(args, " "), err)
	}
	return nil
}

// Checkout is one Repository's git worktree within a Worktree.
type Checkout struct {
	// Repository is the Repository's name; the Workbench shows the
	// checkout at /workspace/<Repository>.
	Repository string
	// ProjectDir is the Repository's path relative to the Project ("" when
	// the Project is the Repository). Compose files under it are read from
	// this checkout.
	ProjectDir string
	// Path is the checkout's absolute path in the Sandbox.
	Path string
	// GitDir is the Repository's common git directory in Main, which the
	// checkout's .git file points to; "" mounts none.
	GitDir string
}

// Settings are what every Worktree's Stack in a Sandbox is generated with:
// the Project's Compose answers (from wizard.Project) and the Sandbox's
// harnesses, secrets and user. Provisioning writes them in the Sandbox
// (worktree.Config).
type Settings struct {
	// ComposeFiles are relative to the Project, as in wizard.Project.
	ComposeFiles []string `json:"compose_files,omitempty"`
	// Workbench is the Compose service used as Workbench; "" adds
	// GenericWorkbench.
	Workbench string `json:"workbench,omitempty"`
	// WebService and WebPort are the Project's web app, if any.
	WebService string `json:"web_service,omitempty"`
	WebPort    int    `json:"web_port,omitempty"`
	// Harnesses are the agent harnesses installed in the Sandbox.
	Harnesses []string `json:"harnesses,omitempty"`
	// Secrets are the names of the Sandbox's msb secrets.
	Secrets []string `json:"secrets,omitempty"`
	// Home is the Sandbox user's home directory.
	Home string `json:"home"`
}

// Spec is everything a Stack is generated from.
type Spec struct {
	// Worktree is the Worktree's name.
	Worktree string
	// Checkouts are the Worktree's git worktrees, one per Repository.
	Checkouts []Checkout
	Settings
}

// Stack is a generated Stack, ready for `docker compose`.
type Stack struct {
	// Project is the Compose project name.
	Project string
	// Files are the Compose files, in order, ending with Override.
	Files    []string
	Override string
	// Workbench is the Workbench's service name.
	Workbench string
	// WebService and WebPort are the Project's web app ("" if none); its
	// port is no longer published.
	WebService string
	WebPort    int
}

// Generate resolves spec's Compose files in its checkouts, asks Compose
// for their services, and writes the override into dir. Calling it again
// regenerates the override.
func Generate(ctx context.Context, docker Docker, spec Spec, dir string) (Stack, error) {
	st := Stack{
		Project:    ProjectName(spec.Worktree),
		Workbench:  spec.Workbench,
		WebService: spec.WebService,
		WebPort:    spec.WebPort,
	}
	if len(spec.Checkouts) == 0 {
		return Stack{}, fmt.Errorf("worktree %s has no checkouts", spec.Worktree)
	}
	for _, f := range spec.ComposeFiles {
		p, err := resolve(spec.Checkouts, f)
		if err != nil {
			return Stack{}, err
		}
		st.Files = append(st.Files, p)
	}
	var services []string
	if len(st.Files) > 0 {
		// Every profile: the override may only name declared services.
		out, err := docker(ctx, st.Command("--profile", "*", "config", "--services")...)
		if err != nil {
			return Stack{}, fmt.Errorf("listing the Stack's services: %w", err)
		}
		services = strings.Fields(string(out))
	}
	if st.Workbench == "" {
		st.Workbench = GenericWorkbench
	} else if !slices.Contains(services, st.Workbench) {
		return Stack{}, fmt.Errorf("workbench service %q is not in the Compose files %v", st.Workbench, spec.ComposeFiles)
	}
	if st.WebService != "" && !slices.Contains(services, st.WebService) {
		return Stack{}, fmt.Errorf("web service %q is not in the Compose files %v", st.WebService, spec.ComposeFiles)
	}
	if st.WebService != "" && (st.WebPort < 1 || st.WebPort > 65535) {
		// The proxy routes to this port: 0 would give a dead route.
		return Stack{}, fmt.Errorf("web service %q needs its web port (1-65535), got %d", st.WebService, st.WebPort)
	}

	// Created up front, by the Sandbox user: Docker would create them as
	// root.
	for _, sh := range shares(spec) {
		if err := os.MkdirAll(filepath.Join(spec.Home, sh.src), 0o755); err != nil {
			return Stack{}, err
		}
	}
	raw, err := yaml.Marshal(override(spec, st.Workbench, services))
	if err != nil {
		return Stack{}, err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return Stack{}, err
	}
	st.Override = filepath.Join(dir, overrideName)
	if err := os.WriteFile(st.Override, raw, 0o644); err != nil {
		return Stack{}, err
	}
	st.Files = append(st.Files, st.Override)
	return st, nil
}

var (
	unsafeName = regexp.MustCompile(`[^a-z0-9_-]+`)
	unsafeHost = regexp.MustCompile(`[^a-z0-9-]+`)
)

// Hostname is where the Sandbox's proxy serves a Worktree's web service:
// http://<Hostname>/ from inside the Sandbox, e.g. in Orca's browser
// (*.localhost resolves to the loopback).
func Hostname(worktree string) string {
	return strings.Trim(unsafeHost.ReplaceAllString(strings.ToLower(worktree), "-"), "-") + ".localhost"
}

// ProjectName is the Compose project name of a Worktree's Stack: sibling
// checkouts of one Worktree share it.
func ProjectName(worktree string) string {
	return "orcasm-" + strings.Trim(unsafeName.ReplaceAllString(strings.ToLower(worktree), "-"), "-")
}

// Command is the `docker` argv running `docker compose args...` on the
// whole Stack.
func (s Stack) Command(args ...string) []string {
	out := []string{"compose", "-p", s.Project}
	for _, f := range s.Files {
		out = append(out, "-f", f)
	}
	return append(out, args...)
}

// Up builds and starts the Stack in the background.
func (s Stack) Up(ctx context.Context, docker Docker) error {
	_, err := docker(ctx, s.Command("up", "--detach", "--build", "--remove-orphans")...)
	return err
}

// Down stops the Stack and deletes its containers, networks and volumes.
func (s Stack) Down(ctx context.Context, docker Docker) error {
	_, err := docker(ctx, s.Command("down", "--volumes", "--remove-orphans")...)
	return err
}

// resolve finds file (relative to the Project) in the checkout of the
// innermost Repository containing it.
func resolve(checkouts []Checkout, file string) (string, error) {
	file = filepath.Clean(file)
	best, rest := -1, ""
	for i, c := range checkouts {
		rel, err := filepath.Rel(filepath.Clean("./"+c.ProjectDir), file)
		if err != nil || rel == ".." || strings.HasPrefix(rel, "../") {
			continue
		}
		if best < 0 || len(c.ProjectDir) > len(checkouts[best].ProjectDir) {
			best, rest = i, rel
		}
	}
	if best < 0 {
		return "", fmt.Errorf("compose file %s is in none of the Worktree's Repositories", file)
	}
	return filepath.Join(checkouts[best].Path, rest), nil
}

func override(spec Spec, workbench string, services []string) *yaml.Node {
	svcs := mapping()
	for _, name := range services {
		svc := mapping(scalar("ports"), &yaml.Node{Kind: yaml.SequenceNode, Tag: "!reset", Style: yaml.FlowStyle})
		if name == workbench {
			svc.Content = append(svc.Content, workbenchSetup(spec)...)
		}
		if name == spec.WebService {
			svc.Content = append(svc.Content, scalar("labels"), proxyLabels(spec))
		}
		svcs.Content = append(svcs.Content, scalar(name), svc)
	}
	if workbench == GenericWorkbench {
		svc := mapping(
			scalar("image"), scalar(GenericImage),
			scalar("build"), mapping(scalar("dockerfile_inline"), scalar(genericDockerfile)),
			scalar("command"), sequence("sleep", "infinity"),
			scalar("init"), &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: "true"},
			scalar("working_dir"), scalar(spec.Checkouts[0].Path),
		)
		svc.Content = append(svc.Content, workbenchSetup(spec)...)
		svcs.Content = append(svcs.Content, scalar(workbench), svc)
	}
	return &yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{mapping(scalar("services"), svcs)}}
}

// workbenchSetup is what every Workbench gets: the Worktree, the harnesses
// and their config, the Sandbox's CAs and the secret placeholders.
func workbenchSetup(spec Spec) []*yaml.Node {
	var vols []string
	for _, c := range spec.Checkouts {
		// The same path as in the Sandbox, so the agent wrapper keeps its
		// working directory and git finds GitDir; /workspace groups them.
		vols = append(vols, c.Path+":"+c.Path, c.Path+":/workspace/"+c.Repository)
		if c.GitDir != "" {
			vols = append(vols, c.GitDir+":"+c.GitDir)
		}
	}
	vols = append(vols, sandboxCABundle+":"+CABundle+":ro", HarnessLayer+":"+HarnessLayer+":ro")
	for _, sh := range shares(spec) {
		dst := sh.dst
		if dst == "" {
			dst = sh.src
		}
		v := filepath.Join(spec.Home, sh.src) + ":" + filepath.Join(spec.Home, dst)
		if sh.ro {
			v += ":ro"
		}
		vols = append(vols, v)
	}
	env := mapping()
	for _, v := range caEnv {
		env.Content = append(env.Content, scalar(v), scalar(CABundle))
	}
	if slices.Contains(spec.Harnesses, "claude") {
		// Claude keeps ~/.claude.json, which it and Orca replace by rename,
		// in this mounted directory instead (provisioning sets it Sandbox
		// wide): a single-file mount would keep the stale inode.
		env.Content = append(env.Content, scalar("CLAUDE_CONFIG_DIR"), scalar(filepath.Join(spec.Home, ".claude")))
	}
	for _, name := range spec.Secrets {
		// msb's placeholder; $$ keeps Compose from interpolating it.
		env.Content = append(env.Content, scalar(name), scalar("$$MSB_"+name))
	}
	out := []*yaml.Node{scalar("volumes"), sequence(vols...), scalar("environment"), env}
	if len(spec.Harnesses) > 0 {
		out = append(out, scalar("extra_hosts"), sequence(hostGateway))
	}
	return out
}

// ProxyLabel marks the containers the Sandbox's proxy routes to: it watches
// only containers labelled ProxyLabel=true (see internal/sandbox/proxy.go).
const ProxyLabel = "orcasm.proxy"

// proxyLabels route Hostname(Worktree) to the web service's port through
// the proxy's Docker provider (Traefik labels).
func proxyLabels(spec Spec) *yaml.Node {
	name := ProjectName(spec.Worktree) // unique per Stack
	return mapping(
		scalar(ProxyLabel), scalar("true"),
		scalar("traefik.enable"), scalar("true"),
		scalar("traefik.http.routers."+name+".rule"), scalar("Host(`"+Hostname(spec.Worktree)+"`)"),
		scalar("traefik.http.services."+name+".loadbalancer.server.port"), scalar(strconv.Itoa(spec.WebPort)),
	)
}

func mapping(kv ...*yaml.Node) *yaml.Node {
	return &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map", Content: kv}
}

func sequence(items ...string) *yaml.Node {
	n := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
	for _, it := range items {
		n.Content = append(n.Content, scalar(it))
	}
	return n
}

func scalar(v string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: v}
}
