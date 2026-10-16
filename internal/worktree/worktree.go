// Package worktree runs inside the Sandbox, from Orca's repo hooks: setup
// starts a new Worktree's Stack in the background, archive tears it down.
//
// Orca runs the hooks as the Sandbox user with ORCA_WORKTREE_PATH set, and
// gives each at most 2 minutes, so setup only generates the Stack and
// detaches `orcasm worktree up`, which builds and starts it.
//
// A Worktree has one Repository's checkout here: Main's. ADR 0005's sibling
// checkouts across every Repository of the Project (created by setup, torn
// down by the last archive under a file lock) are Milestone 4,
// https://github.com/Betree/Orcasm/issues/28.
package worktree

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/Betree/Orcasm/internal/guest"
	"github.com/Betree/Orcasm/internal/orca"
	"github.com/Betree/Orcasm/internal/stack"
)

// OrcaSettings are the repo settings making Orca run these hooks, set on
// Main through the runtime (no committed orca.yaml).
//
// setupRunPolicy run-by-default makes Orca run setup on every Worktree it
// creates unless told to skip it: the UI's default, and `orca-ide worktree
// create` without --setup (inherit) or with --setup run. Only an explicit
// --setup skip goes without; the first agent launched then starts the Stack
// (Workbench).
func OrcaSettings() orca.RepoUpdates {
	return orca.RepoUpdates{
		HookSettings: &orca.HookSettings{
			Mode:           "auto",
			SetupRunPolicy: "run-by-default",
			Scripts: orca.Scripts{
				Setup:   guest.Binary + " worktree setup",
				Archive: guest.Binary + " worktree archive",
			},
		},
		WorktreeBasePath: guest.WorktreesDir,
	}
}

// Config is what the hooks know about the Project, written in the Sandbox
// by provisioning.
type Config struct {
	// Main is Main's path; its .git is every Worktree's common git dir.
	Main string `json:"main"`
	stack.Settings
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
	// the hook's process group, logging into dir. The process inherits
	// the inherit files and keeps them open until it exits.
	Detach func(dir string, inherit []*os.File, argv ...string) error
	// Poll is how often Workbench checks on a starting Stack (0 = 500ms).
	Poll time.Duration
	// ArchiveWait bounds how long Archive waits for whoever starts the
	// Stack before leaving the teardown to the background (0 = 90s, inside
	// Orca's 2-minute hook timeout).
	ArchiveWait time.Duration
}

// Guest are the hooks of the Sandbox's Project: the Config provisioning
// wrote (guest.Conf relocates it), the docker CLI, and DetachSelf.
func Guest() (Hooks, error) {
	cfg, err := ReadConfig(guest.Conf(guest.WorktreeConfig))
	if err != nil {
		return Hooks{}, err
	}
	return Hooks{Config: cfg, Docker: stack.ExecDocker, Detach: DetachSelf}, nil
}

// NameOf is the Worktree holding dir, Orca's worktree directory name:
// dir is guest.WorktreesDir/<Repository>/<Worktree>[/...].
func NameOf(dir string) (string, error) {
	_, name, err := checkout(dir)
	return name, err
}

// checkout is the root of the Worktree checkout holding dir, and the
// Worktree's name.
func checkout(dir string) (root, name string, err error) {
	rel, err := filepath.Rel(guest.WorktreesDir, filepath.Clean(dir))
	parts := strings.Split(rel, string(filepath.Separator))
	if err != nil || len(parts) < 2 || parts[0] == ".." || parts[0] == "." {
		return "", "", fmt.Errorf("%s is in no Worktree (under %s)", dir, guest.WorktreesDir)
	}
	return filepath.Join(guest.WorktreesDir, parts[0], parts[1]), parts[1], nil
}

// StateDir holds a Worktree's generated Stack, its `up` log and pid.
func (h Hooks) StateDir(worktree string) string {
	return filepath.Join(h.Config.Home, ".local/state/orcasm/stacks", stack.ProjectName(worktree))
}

const (
	stackFile = "stack.json"
	pidFile   = "up.pid"
	// lockFile is held by whoever starts the Stack: the background `up`
	// from Setup to its end, Workbench while it checks and starts it.
	lockFile = "up.lock"
	// LogFile is the background `up`'s output, in the StateDir.
	LogFile = "up.log"
)

// Setup generates the Stack of the Worktree checked out at path and starts
// it in the background, telling out where to follow it.
func (h Hooks) Setup(ctx context.Context, path string, out io.Writer) error {
	root, name, err := checkout(path)
	if err != nil {
		return err
	}
	dir := h.StateDir(name)
	// Taken before generating and handed over to the background `up`, so
	// an agent launched meanwhile waits for it instead of starting the
	// Stack a second time. Already held, an agent got there first (Orca
	// launched it before this hook) and is starting the Stack: that is
	// left to it, rather than waiting past the hook's timeout.
	lock, err := h.lock(ctx, dir, func() error { return errBusy })
	if errors.Is(err, errBusy) {
		fmt.Fprintf(out, "The Stack of %s is already being started (by an agent in it)\n", name)
	} else if err != nil {
		return err
	} else {
		defer lock.Close()
		if _, err := h.generate(ctx, name, root); err != nil {
			return err
		}
		if err := h.Detach(dir, []*os.File{lock}, "worktree", "up", dir); err != nil {
			return err
		}
		fmt.Fprintf(out, "Starting the Stack of %s in the background; log: %s\n", name, filepath.Join(dir, LogFile))
	}
	if h.Config.WebService != "" {
		fmt.Fprintf(out, "Its web app will be at http://%s/\n", stack.Hostname(name))
	}
	return nil
}

// generate writes the Stack of Worktree name, checked out at path, into
// its StateDir, for Load. It has a single checkout, Main's Repository: see
// the package doc for #28.
func (h Hooks) generate(ctx context.Context, name, path string) (stack.Stack, error) {
	dir := h.StateDir(name)
	st, err := stack.Generate(ctx, h.Docker, h.spec(name, path), dir)
	if err != nil {
		return st, err
	}
	raw, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return st, err
	}
	return st, os.WriteFile(filepath.Join(dir, stackFile), raw, 0o644)
}

// spec is what the Stack of Worktree name, checked out at path, is
// generated from.
func (h Hooks) spec(name, path string) stack.Spec {
	main := filepath.Clean(h.Config.Main)
	return stack.Spec{
		Worktree: name,
		Checkouts: []stack.Checkout{{
			Repository: filepath.Base(main),
			Path:       filepath.Clean(path),
			GitDir:     filepath.Join(main, ".git"),
		}},
		Settings: h.Config.Settings,
	}
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
// Setup, holding the Stack's lock Setup handed it. It records its pid so
// Archive can stop it.
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

// DetachSelf starts this binary with argv in its own session, so it
// outlives the hook (and Orca's process-group kill on timeout), appending
// to dir's log and inheriting the inherit files.
func DetachSelf(dir string, inherit []*os.File, argv ...string) error {
	self, err := os.Executable()
	if err != nil {
		return err
	}
	log, err := os.OpenFile(filepath.Join(dir, LogFile), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer log.Close()
	c := exec.Command(self, argv...)
	c.Stdout, c.Stderr = log, log
	c.ExtraFiles = inherit
	c.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := c.Start(); err != nil {
		return err
	}
	return c.Process.Release()
}

// Archive tears down the Stack of the Worktree checked out at path (see
// Teardown). Whoever starts the Stack may outlast Orca's hook timeout (a
// long image build): past ArchiveWait, the teardown is left to a
// background `orcasm worktree teardown`, still waiting for the lock. With
// one checkout per Worktree there are no siblings to wait for (ADR 0005,
// #28).
func (h Hooks) Archive(ctx context.Context, path string) error {
	name, err := NameOf(path)
	if err != nil {
		return err
	}
	wait := h.ArchiveWait
	if wait == 0 {
		wait = 90 * time.Second
	}
	waitCtx, cancel := context.WithTimeout(ctx, wait)
	defer cancel()
	err = h.teardown(waitCtx, name)
	if errors.Is(err, context.DeadlineExceeded) && ctx.Err() == nil {
		return h.Detach(h.StateDir(name), nil, "worktree", "teardown", path)
	}
	return err
}

// Teardown stops a still running `up` of the Worktree checked out at path,
// removes its Stack's containers, networks and volumes, and forgets the
// Stack. It holds the Stack's lock while doing so, so whoever is starting
// the Stack finishes first rather than bringing it back afterwards.
func (h Hooks) Teardown(ctx context.Context, path string) error {
	name, err := NameOf(path)
	if err != nil {
		return err
	}
	return h.teardown(ctx, name)
}

func (h Hooks) teardown(ctx context.Context, name string) error {
	dir := h.StateDir(name)
	stopped := false
	lock, err := h.lock(ctx, dir, func() error {
		// Only a held lock means a live `up`: a pid file alone may be
		// stale (Sandbox restart) and its pid another process's.
		if !stopped {
			stopUp(dir)
			stopped = true
		}
		return nil
	})
	if err != nil {
		return err
	}
	defer lock.Close()
	st, err := Load(dir)
	if errors.Is(err, os.ErrNotExist) {
		// Setup never ran (e.g. created with --setup skip): Compose finds
		// the project's containers by name alone.
		st = stack.Stack{Project: stack.ProjectName(name)}
	} else if err != nil {
		return err
	}
	if err := st.Down(ctx, h.Docker); err != nil {
		return err
	}
	return os.RemoveAll(dir)
}

// stopUp ends the background `up` recorded in dir with the docker compose
// it runs (same process group). Only call it while the Stack's lock is
// held by someone else: that `up` is then still running.
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
// started again, by one caller at a time. Orca runs the setup hook in its
// own terminal once the Worktree exists, so an agent may come first (or
// setup was skipped): the Stack is then generated here, as Setup would.
func (h Hooks) Workbench(ctx context.Context, dir string, progress io.Writer) (string, error) {
	root, name, err := checkout(dir)
	if err != nil {
		return "", err
	}
	state := h.StateDir(name)
	lock, err := h.follow(ctx, name, state, progress)
	if err != nil {
		return "", err
	}
	defer lock.Close()
	st, err := Load(state)
	if errors.Is(err, os.ErrNotExist) {
		fmt.Fprintf(progress, "orcasm: Orca's setup hook has not run for %s yet; generating its Stack\n", name)
		st, err = h.generate(ctx, name, root)
	}
	if err != nil {
		return "", err
	}
	id, err := h.runningWorkbench(ctx, st)
	if err != nil {
		return "", err
	}
	if st.Outdated(h.spec(name, root)) {
		// Generated by an older orcasm (before `orcasm upgrade`), or with
		// other Sandbox settings: recreate the Workbench with this one's
		// layer, unless that would kill an agent running in it; it then
		// waits for a later launch.
		busy := false
		if id != "" {
			if busy, err = h.agentsIn(ctx, id); err != nil {
				return "", err
			}
		}
		if !busy {
			fmt.Fprintf(progress, "orcasm: Updating the Workbench of %s to this orcasm\n", name)
			if st, err = h.generate(ctx, name, root); err != nil {
				return "", err
			}
			id = ""
		}
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

// agentsIn reports whether an agent runs in Workbench container id, as
// `orcasm upgrade` tells agents in the Sandbox (stack.AgentProcesses).
func (h Hooks) agentsIn(ctx context.Context, id string) (bool, error) {
	// docker top runs ps on the Sandbox side: no ps needed in the image.
	out, err := h.Docker(ctx, "top", id, "-o", "pid,args")
	if err != nil {
		return false, fmt.Errorf("listing the processes of the Workbench: %w", err)
	}
	agent, err := regexp.Compile(stack.AgentProcesses(h.Config.Harnesses))
	if err != nil {
		return false, err
	}
	lines := strings.Split(string(out), "\n")
	for _, line := range lines[min(1, len(lines)):] { // after the header
		_, args, _ := strings.Cut(strings.TrimSpace(line), " ")
		if agent.MatchString(strings.TrimSpace(args)) {
			return true, nil
		}
	}
	return false, nil
}

func (h Hooks) runningWorkbench(ctx context.Context, st stack.Stack) (string, error) {
	out, err := h.Docker(ctx, st.Command("ps", "--quiet", "--status", "running", st.Workbench)...)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// follow takes the Stack's lock in state; while a background `up` holds
// it, it copies the `up` log to progress.
func (h Hooks) follow(ctx context.Context, name, state string, progress io.Writer) (*os.File, error) {
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
	waited := false
	lock, err := h.lock(ctx, state, func() error {
		if !waited {
			fmt.Fprintf(progress, "orcasm: waiting for the Stack of %s to start...\n", name)
			waited = true
		}
		copyNew()
		return nil
	})
	if err == nil && waited {
		copyNew()
	}
	return lock, err
}

// errBusy gives up waiting for the Stack's lock.
var errBusy = errors.New("the Stack is being started")

// lock takes the Stack's lock in dir, calling wait every Poll while
// someone else holds it; an error from wait gives up. Closing the file
// releases the lock.
func (h Hooks) lock(ctx context.Context, dir string, wait func() error) (*os.File, error) {
	poll := h.Poll
	if poll == 0 {
		poll = 500 * time.Millisecond
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(dir, lockFile), os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}
	for {
		err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			// No `up` holds the lock, so a pid file is a dead one's.
			if err := os.Remove(filepath.Join(dir, pidFile)); err != nil && !errors.Is(err, os.ErrNotExist) {
				f.Close()
				return nil, err
			}
			return f, nil
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) {
			f.Close()
			return nil, fmt.Errorf("locking the Stack in %s: %w", dir, err)
		}
		if err := wait(); err != nil {
			f.Close()
			return nil, err
		}
		select {
		case <-ctx.Done():
			f.Close()
			return nil, ctx.Err()
		case <-time.After(poll):
		}
	}
}
