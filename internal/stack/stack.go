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
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
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
	// Workbench, running GenericImage.
	GenericWorkbench = "orcasm-workbench"
	GenericImage     = "ubuntu:24.04"
	// overrideName is the generated file, written in Generate's dir.
	overrideName = "compose.orcasm.yaml"
)

// harnessDirs are the config dirs of each harness, relative to the home
// directory, shared read-write between Main and every Workbench.
var harnessDirs = map[string][]string{
	"claude":   {".claude"},
	"codex":    {".codex"},
	"opencode": {".config/opencode", ".local/share/opencode"},
}

// caEnv are the variables pointing tools with their own CA list at
// CABundle.
var caEnv = []string{"SSL_CERT_FILE", "NODE_EXTRA_CA_CERTS", "CURL_CA_BUNDLE", "REQUESTS_CA_BUNDLE", "GIT_SSL_CAINFO"}

// Docker runs `docker args...` and returns its stdout.
type Docker func(ctx context.Context, args ...string) ([]byte, error)

// ExecDocker runs the docker CLI on PATH; stderr goes in the error.
func ExecDocker(ctx context.Context, args ...string) ([]byte, error) {
	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, "docker", args...)
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return out, fmt.Errorf("docker %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return out, nil
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

// Spec is everything a Stack is generated from.
type Spec struct {
	// Worktree is the Worktree's name.
	Worktree string
	// Checkouts are the Worktree's git worktrees, one per Repository.
	Checkouts []Checkout
	// ComposeFiles are relative to the Project, as in wizard.Project.
	ComposeFiles []string
	// Workbench is the Compose service used as Workbench; "" adds
	// GenericWorkbench.
	Workbench string
	// WebService and WebPort are the Project's web app, if any.
	WebService string
	WebPort    int
	// Harnesses are the agent harnesses installed in the Sandbox.
	Harnesses []string
	// Secrets are the names of the Sandbox's msb secrets.
	Secrets []string
	// Home is the Sandbox user's home directory.
	Home string
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

	for _, h := range spec.Harnesses {
		for _, d := range harnessDirs[h] {
			if err := os.MkdirAll(filepath.Join(spec.Home, d), 0o755); err != nil {
				return Stack{}, err
			}
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

var unsafeName = regexp.MustCompile(`[^a-z0-9_-]+`)

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
		svcs.Content = append(svcs.Content, scalar(name), svc)
	}
	if workbench == GenericWorkbench {
		svc := mapping(
			scalar("image"), scalar(GenericImage),
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
	for _, h := range spec.Harnesses {
		for _, d := range harnessDirs[h] {
			p := filepath.Join(spec.Home, d)
			vols = append(vols, p+":"+p)
		}
	}
	env := mapping()
	for _, v := range caEnv {
		env.Content = append(env.Content, scalar(v), scalar(CABundle))
	}
	for _, name := range spec.Secrets {
		// msb's placeholder; $$ keeps Compose from interpolating it.
		env.Content = append(env.Content, scalar(name), scalar("$$MSB_"+name))
	}
	return []*yaml.Node{scalar("volumes"), sequence(vols...), scalar("environment"), env}
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
