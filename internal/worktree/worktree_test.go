package worktree_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Betree/Orcasm/internal/worktree"
)

// fakeDocker answers `compose ... config --services` with services and
// records every call.
type fakeDocker struct {
	services []string
	calls    [][]string
	// reply, if set, answers the calls other than `config --services`.
	reply func(args []string) (string, error)
	mu    sync.Mutex
}

func (f *fakeDocker) run(_ context.Context, args ...string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, args)
	if f.reply != nil && !slices.Contains(args, "--services") {
		out, err := f.reply(args)
		return []byte(out), err
	}
	return []byte(strings.Join(f.services, "\n") + "\n"), nil
}

// called reports whether a docker call ended with the given args.
func (f *fakeDocker) called(suffix ...string) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.calls {
		if len(c) >= len(suffix) && slices.Equal(c[len(c)-len(suffix):], suffix) {
			return c
		}
	}
	return nil
}

type fixture struct {
	hooks    worktree.Hooks
	docker   *fakeDocker
	detached [][]string
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{docker: &fakeDocker{services: []string{"web"}}}
	home := t.TempDir()
	f.hooks = worktree.Hooks{
		Config: worktree.Config{
			Main:         "/home/dev/shop",
			ComposeFiles: []string{"compose.yml"},
			WebService:   "web",
			WebPort:      80,
			Harnesses:    []string{"claude"},
			Secrets:      []string{"GH_TOKEN"},
			Home:         home,
		},
		Docker: f.docker.run,
		Detach: func(dir string, argv ...string) error {
			f.detached = append(f.detached, append([]string{dir}, argv...))
			return nil
		},
	}
	return f
}

func TestSetupStartsTheWorktreesStackInTheBackground(t *testing.T) {
	f := newFixture(t)
	if err := f.hooks.Setup(context.Background(), "/worktrees/shop/feat-login"); err != nil {
		t.Fatal(err)
	}
	if len(f.detached) != 1 {
		t.Fatalf("detached %v, want one background `up`", f.detached)
	}
	dir := f.detached[0][0]
	if want := filepath.Join(f.hooks.Config.Home, ".local/state/orcasm/stacks/orcasm-feat-login"); dir != want {
		t.Errorf("state dir %s, want %s", dir, want)
	}
	if got := f.detached[0][1:]; !slices.Equal(got, []string{"worktree", "up", dir}) {
		t.Errorf("detached argv %v", got)
	}
	// The Stack reads the Worktree's checkout, not Main's.
	c := f.docker.called("config", "--services")
	if c == nil || !slices.Contains(c, "/worktrees/shop/feat-login/compose.yml") {
		t.Errorf("compose config call %v", c)
	}
	st, err := worktree.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if st.Project != "orcasm-feat-login" || st.Workbench != "orcasm-workbench" || st.WebService != "web" {
		t.Errorf("saved Stack %+v", st)
	}
	override, err := os.ReadFile(st.Override)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"/home/dev/shop/.git:/home/dev/shop/.git", "/worktrees/shop/feat-login:/workspace/shop", "$$MSB_GH_TOKEN"} {
		if !strings.Contains(string(override), want) {
			t.Errorf("override lacks %q:\n%s", want, override)
		}
	}
}

func TestArchiveTearsDownTheStackSetupStarted(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	path := "/worktrees/shop/feat-login"
	if err := f.hooks.Setup(ctx, path); err != nil {
		t.Fatal(err)
	}
	dir := f.detached[0][0]
	if err := f.hooks.Archive(ctx, path); err != nil {
		t.Fatal(err)
	}
	down := f.docker.called("down", "--volumes", "--remove-orphans")
	if down == nil || !slices.Contains(down, "orcasm-feat-login") || !slices.Contains(down, "/worktrees/shop/feat-login/compose.yml") {
		t.Errorf("down call %v", down)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("state dir still there: %v", err)
	}
}

func TestArchiveWithoutSetupStillTearsDownByProjectName(t *testing.T) {
	f := newFixture(t)
	if err := f.hooks.Archive(context.Background(), "/worktrees/shop/no-setup"); err != nil {
		t.Fatal(err)
	}
	want := []string{"compose", "-p", "orcasm-no-setup", "down", "--volumes", "--remove-orphans"}
	if !slices.Equal(f.docker.called("down", "--volumes", "--remove-orphans"), want) {
		t.Errorf("calls %v, want %v", f.docker.calls, want)
	}
}

// running makes `docker compose ps` report the Workbench container id (""
// = not running) and answers every other call with nothing.
func (f *fixture) running(id func() string) {
	f.docker.reply = func(args []string) (string, error) {
		if slices.Contains(args, "ps") {
			return id() + "\n", nil
		}
		return "", nil
	}
}

func TestTheWorkbenchOfARunningStackIsFoundFromAnywhereInTheWorktree(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	if err := f.hooks.Setup(ctx, "/worktrees/shop/feat-login"); err != nil {
		t.Fatal(err)
	}
	f.running(func() string { return "c0ffee" })

	id, err := f.hooks.Workbench(ctx, "/worktrees/shop/feat-login/pkg/api", &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	if id != "c0ffee" {
		t.Errorf("id = %q", id)
	}
	ps := f.docker.called("ps", "--quiet", "--status", "running", "orcasm-workbench")
	if ps == nil || !slices.Contains(ps, "orcasm-feat-login") {
		t.Errorf("calls %v lack a compose ps of the Workbench", f.docker.calls)
	}
	// The agent runs as the Sandbox user with HOME set: the Workbench needs
	// that home to exist and be writable (caches, shell history...).
	owner := strconv.Itoa(os.Getuid()) + ":" + strconv.Itoa(os.Getgid())
	var prepared []string
	for _, c := range f.docker.calls {
		if len(c) > 3 && c[0] == "exec" && slices.Contains(c, "c0ffee") && slices.Contains(c, f.hooks.Config.Home) && slices.Contains(c, owner) {
			prepared = c
		}
	}
	if prepared == nil || !slices.Equal(prepared[1:3], []string{"-u", "0"}) {
		t.Errorf("home not prepared as root; calls %v", f.docker.calls)
	}
}

func TestWhileTheStackStartsTheAgentSeesItsProgress(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	path := "/worktrees/shop/feat-login"
	if err := f.hooks.Setup(ctx, path); err != nil {
		t.Fatal(err)
	}
	dir := f.hooks.StateDir("feat-login")
	// A background `up` still running: its pid file names a live process.
	up := exec.Command("sleep", "30")
	if err := up.Start(); err != nil {
		t.Fatal(err)
	}
	defer up.Process.Kill()
	must(t, os.WriteFile(filepath.Join(dir, "up.pid"), []byte(strconv.Itoa(up.Process.Pid)), 0o644))
	log := filepath.Join(dir, worktree.LogFile)
	must(t, os.WriteFile(log, []byte("Building web\n"), 0o644))
	var mu sync.Mutex
	upDone := false
	f.running(func() string {
		mu.Lock()
		defer mu.Unlock()
		if upDone {
			return "c0ffee"
		}
		return ""
	})
	f.hooks.Poll = 10 * time.Millisecond

	type result struct {
		id  string
		err error
	}
	got := make(chan result, 1)
	var progress bytes.Buffer
	go func() {
		id, err := f.hooks.Workbench(ctx, path, &progress)
		got <- result{id, err}
	}()
	time.Sleep(100 * time.Millisecond)
	select {
	case r := <-got:
		t.Fatalf("returned while the Stack was starting: %+v", r)
	default:
	}
	l, err := os.OpenFile(log, os.O_APPEND|os.O_WRONLY, 0)
	must(t, err)
	_, err = l.WriteString("orcasm: the Stack is up\n")
	must(t, err)
	must(t, l.Close())
	mu.Lock()
	upDone = true
	mu.Unlock()
	must(t, os.Remove(filepath.Join(dir, "up.pid")))

	r := <-got
	if r.err != nil || r.id != "c0ffee" {
		t.Fatalf("got %+v", r)
	}
	for _, want := range []string{"Building web\n", "orcasm: the Stack is up\n"} {
		if !strings.Contains(progress.String(), want) {
			t.Errorf("progress %q lacks %q", progress.String(), want)
		}
	}
	if f.docker.called("up", "--detach", "--build", "--remove-orphans") != nil {
		t.Error("started the Stack a second time")
	}
}

func TestAStoppedStackIsStartedAgain(t *testing.T) {
	// E.g. after the Sandbox restarted: nothing brings the Workbench back.
	f := newFixture(t)
	ctx := context.Background()
	path := "/worktrees/shop/feat-login"
	if err := f.hooks.Setup(ctx, path); err != nil {
		t.Fatal(err)
	}
	started := false
	f.docker.reply = func(args []string) (string, error) {
		switch {
		case slices.Contains(args, "up"):
			started = true
		case slices.Contains(args, "ps") && started:
			return "c0ffee\n", nil
		}
		return "", nil
	}
	var progress bytes.Buffer
	id, err := f.hooks.Workbench(ctx, path, &progress)
	if err != nil || id != "c0ffee" {
		t.Fatalf("id %q, err %v", id, err)
	}
	if !strings.Contains(progress.String(), "Starting") {
		t.Errorf("progress %q does not say the Stack is starting", progress.String())
	}
}

func TestAStackThatWillNotStartIsReported(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	path := "/worktrees/shop/feat-login"
	if err := f.hooks.Setup(ctx, path); err != nil {
		t.Fatal(err)
	}
	f.docker.reply = func(args []string) (string, error) {
		if slices.Contains(args, "up") {
			return "", errors.New("build failed")
		}
		return "", nil
	}
	_, err := f.hooks.Workbench(ctx, path, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "build failed") {
		t.Errorf("err = %v", err)
	}
}

func TestAWorktreeWhoseSetupNeverRanHasNoWorkbench(t *testing.T) {
	f := newFixture(t)
	_, err := f.hooks.Workbench(context.Background(), "/worktrees/shop/no-setup", &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "setup") {
		t.Errorf("err = %v, want one pointing at Orca's setup hook", err)
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
