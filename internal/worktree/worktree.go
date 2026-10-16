// Package worktree runs inside the Sandbox, from Orca's repo hooks: setup
// starts a new Worktree's Stack in the background, archive tears it down.
//
// Orca runs the hooks as the Sandbox user with ORCA_WORKTREE_PATH set, and
// gives each at most 2 minutes, so setup only generates the Stack and
// detaches `orcasm worktree up`, which builds and starts it.
package worktree

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/Betree/Orcasm/internal/orca"
	"github.com/Betree/Orcasm/internal/stack"
)

const (
	// ConfigFile is where provisioning writes the Config in the Sandbox.
	ConfigFile = "/etc/orcasm/worktree.json"
	// Binary is where provisioning installs orcasm in the Sandbox.
	Binary = "/usr/local/bin/orcasm"
	// BasePath is where Orca puts Worktrees: BasePath/<repo>/<worktree>.
	BasePath = "/worktrees"
)

// OrcaSettings are the repo settings making Orca run these hooks, set on
// Main through the runtime (no committed orca.yaml).
func OrcaSettings() orca.RepoUpdates {
	return orca.RepoUpdates{
		HookSettings: &orca.HookSettings{
			Mode:           "auto",
			SetupRunPolicy: "run-by-default",
			Scripts: orca.Scripts{
				Setup:   Binary + " worktree setup",
				Archive: Binary + " worktree archive",
			},
		},
		WorktreeBasePath: BasePath,
	}
}

// Config is what the hooks know about the Project, written in the Sandbox
// by provisioning.
type Config struct {
	// Main is Main's path; its .git is every Worktree's common git dir.
	Main         string   `json:"main"`
	ComposeFiles []string `json:"compose_files,omitempty"`
	Workbench    string   `json:"workbench,omitempty"`
	WebService   string   `json:"web_service,omitempty"`
	WebPort      int      `json:"web_port,omitempty"`
	Harnesses    []string `json:"harnesses,omitempty"`
	// Secrets are the names of the Sandbox's msb secrets.
	Secrets []string `json:"secrets,omitempty"`
	// Home is the Sandbox user's home directory.
	Home string `json:"home"`
}

// ReadConfig reads the Config at path.
func ReadConfig(path string) (Config, error) {
	var c Config
	raw, err := os.ReadFile(path)
	if err != nil {
		return c, err
	}
	if err := json.Unmarshal(raw, &c); err != nil {
		return c, fmt.Errorf("%s: %w", path, err)
	}
	return c, nil
}

// Hooks are Orca's setup and archive hooks for the Project.
type Hooks struct {
	Config Config
	Docker stack.Docker
	// Detach starts argv (an orcasm subcommand) in the background, out of
	// the hook's process group, logging into dir.
	Detach func(dir string, argv ...string) error
	// Poll is how often Workbench checks on a starting Stack (0 = 500ms).
	Poll time.Duration
}

// Name is the Worktree checked out at path: Orca's worktree directory name.
func Name(path string) string { return filepath.Base(filepath.Clean(path)) }

// StateDir holds a Worktree's generated Stack, its `up` log and pid.
func (h Hooks) StateDir(worktree string) string {
	return filepath.Join(h.Config.Home, ".local/state/orcasm/stacks", stack.ProjectName(worktree))
}

const (
	stackFile = "stack.json"
	pidFile   = "up.pid"
	// LogFile is the background `up`'s output, in the StateDir.
	LogFile = "up.log"
)

// Setup generates the Stack of the Worktree checked out at path and starts
// it in the background.
func (h Hooks) Setup(ctx context.Context, path string) error {
	st, dir, err := h.generate(ctx, path)
	if err != nil {
		return err
	}
	raw, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, stackFile), raw, 0o644); err != nil {
		return err
	}
	return h.Detach(dir, "worktree", "up", dir)
}

func (h Hooks) generate(ctx context.Context, path string) (stack.Stack, string, error) {
	name := Name(path)
	dir := h.StateDir(name)
	main := filepath.Clean(h.Config.Main)
	spec := stack.Spec{
		Worktree: name,
		Checkouts: []stack.Checkout{{
			Repository: filepath.Base(main),
			Path:       filepath.Clean(path),
			GitDir:     filepath.Join(main, ".git"),
		}},
		ComposeFiles: h.Config.ComposeFiles,
		Workbench:    h.Config.Workbench,
		WebService:   h.Config.WebService,
		WebPort:      h.Config.WebPort,
		Harnesses:    h.Config.Harnesses,
		Secrets:      h.Config.Secrets,
		Home:         h.Config.Home,
	}
	st, err := stack.Generate(ctx, h.Docker, spec, dir)
	return st, dir, err
}

// Load reads the Stack Setup saved in dir.
func Load(dir string) (stack.Stack, error) {
	var st stack.Stack
	raw, err := os.ReadFile(filepath.Join(dir, stackFile))
	if err != nil {
		return st, err
	}
	return st, json.Unmarshal(raw, &st)
}

// Up builds and starts the Stack saved in dir: the background half of
// Setup. It records its pid so Archive can stop it.
func Up(ctx context.Context, docker stack.Docker, dir string) error {
	st, err := Load(dir)
	if err != nil {
		return err
	}
	pid := filepath.Join(dir, pidFile)
	if err := os.WriteFile(pid, []byte(strconv.Itoa(os.Getpid())), 0o644); err != nil {
		return err
	}
	defer os.Remove(pid)
	return st.Up(ctx, docker)
}

// Archive tears down the Stack of the Worktree checked out at path: it stops
// a still running `up`, removes the Stack's containers, networks and
// volumes, and forgets the Stack.
func (h Hooks) Archive(ctx context.Context, path string) error {
	dir := h.StateDir(Name(path))
	stopUp(dir)
	st, err := Load(dir)
	if errors.Is(err, os.ErrNotExist) {
		// Setup never ran (e.g. created with --setup skip): Compose finds
		// the project's containers by name alone.
		st = stack.Stack{Project: stack.ProjectName(Name(path))}
	} else if err != nil {
		return err
	}
	if err := st.Down(ctx, h.Docker); err != nil {
		return err
	}
	return os.RemoveAll(dir)
}

// stopUp ends the background `up` recorded in dir, if still running, with
// the docker compose it runs (same process group).
func stopUp(dir string) {
	raw, err := os.ReadFile(filepath.Join(dir, pidFile))
	if err != nil {
		return
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil || pid <= 1 {
		return
	}
	_ = syscall.Kill(-pid, syscall.SIGTERM)
}

// Workbench waits until the Stack of the Worktree holding dir is up and
// returns its Workbench's container id, ready for the agent wrapper to run
// an agent in as the calling user. While a background `up` runs, its log
// goes to progress; a Stack found stopped (e.g. after a Sandbox restart) is
// started again.
func (h Hooks) Workbench(ctx context.Context, dir string, progress io.Writer) (string, error) {
	name, ok := worktreeOf(dir)
	if !ok {
		return "", fmt.Errorf("%s is in no Worktree (under %s)", dir, BasePath)
	}
	state := h.StateDir(name)
	st, err := Load(state)
	if errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("Worktree %s has no Stack: Orca's setup hook did not run for it", name)
	} else if err != nil {
		return "", err
	}
	if err := h.follow(ctx, name, state, progress); err != nil {
		return "", err
	}
	id, err := h.runningWorkbench(ctx, st)
	if err != nil {
		return "", err
	}
	if id == "" {
		fmt.Fprintf(progress, "orcasm: Starting the Stack of %s...\n", name)
		if err := st.Up(ctx, h.Docker); err != nil {
			return "", fmt.Errorf("starting the Stack of %s: %w", name, err)
		}
		if id, err = h.runningWorkbench(ctx, st); err != nil {
			return "", err
		}
		if id == "" {
			return "", fmt.Errorf("the Workbench (%s) of %s is not running: see docker compose -p %s logs %s",
				st.Workbench, name, st.Project, st.Workbench)
		}
	}
	// docker exec -u <uid> -e HOME=<home>: the home must exist in the
	// Workbench and be the user's. Only its shared subdirs are mounted, so
	// Docker created the home and the dirs leading to them (~/.cache,
	// ~/.local...) as root; hand those to the user, not the mounts (-xdev).
	owner := strconv.Itoa(os.Getuid()) + ":" + strconv.Itoa(os.Getgid())
	if _, err := h.Docker(ctx, "exec", "-u", "0", id, "sh", "-c",
		`mkdir -p "$1" && find "$1" -xdev -type d -user 0 -exec chown "$2" {} +`,
		"sh", h.Config.Home, owner); err != nil {
		return "", fmt.Errorf("preparing %s in the Workbench: %w", h.Config.Home, err)
	}
	return id, nil
}

// worktreeOf is the Worktree holding dir: BasePath/<repo>/<worktree>/...
func worktreeOf(dir string) (string, bool) {
	rel, err := filepath.Rel(BasePath, filepath.Clean(dir))
	if err != nil {
		return "", false
	}
	parts := strings.Split(rel, string(filepath.Separator))
	if len(parts) < 2 || parts[0] == ".." || parts[0] == "." {
		return "", false
	}
	return parts[1], true
}

func (h Hooks) runningWorkbench(ctx context.Context, st stack.Stack) (string, error) {
	out, err := h.Docker(ctx, st.Command("ps", "--quiet", "--status", "running", st.Workbench)...)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// follow copies the background `up`'s log to progress until it ends; it
// returns at once if none runs.
func (h Hooks) follow(ctx context.Context, name, state string, progress io.Writer) error {
	if !upRunning(state) {
		return nil
	}
	poll := h.Poll
	if poll == 0 {
		poll = 500 * time.Millisecond
	}
	fmt.Fprintf(progress, "orcasm: waiting for the Stack of %s to start...\n", name)
	var off int64
	copyNew := func() {
		f, err := os.Open(filepath.Join(state, LogFile))
		if err != nil {
			return
		}
		defer f.Close()
		if _, err := f.Seek(off, io.SeekStart); err == nil {
			n, _ := io.Copy(progress, f)
			off += n
		}
	}
	for upRunning(state) {
		copyNew()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(poll):
		}
	}
	copyNew()
	return nil
}

// upRunning reports whether the background `up` recorded in dir is alive.
func upRunning(dir string) bool {
	raw, err := os.ReadFile(filepath.Join(dir, pidFile))
	if err != nil {
		return false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil || pid <= 1 {
		return false
	}
	err = syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}
