package worktree_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
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
	// unlocked runs reply concurrently, as the real docker would.
	unlocked bool
	mu       sync.Mutex
}

func (f *fakeDocker) run(_ context.Context, args ...string) ([]byte, error) {
	f.mu.Lock()
	f.calls = append(f.calls, args)
	reply, unlocked := f.reply, f.unlocked
	if unlocked {
		f.mu.Unlock()
	} else {
		defer f.mu.Unlock()
	}
	if reply != nil && !slices.Contains(args, "--services") {
		out, err := reply(args)
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
	// upRuns keeps the detached `up` "running" after Setup returns: it
	// holds what Setup handed it until upEnds.
	upRuns bool
	up     *os.File
}

// upEnds makes the detached `up` exit.
func (f *fixture) upEnds() {
	if f.up != nil {
		f.up.Close()
		f.up = nil
	}
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{docker: &fakeDocker{services: []string{"web"}}}
	t.Cleanup(f.upEnds)
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
		// Like a real child process, the background `up` keeps its own
		// copy of the files it inherits, open until it exits.
		Detach: func(dir string, inherit []*os.File, argv ...string) error {
			f.detached = append(f.detached, append([]string{dir}, argv...))
			if f.upRuns && len(inherit) > 0 {
				fd, err := syscall.Dup(int(inherit[0].Fd()))
				if err != nil {
					return err
				}
				f.up = os.NewFile(uintptr(fd), "up")
			}
			return nil
		},
	}
	return f
}

func TestSetupStartsTheWorktreesStackInTheBackground(t *testing.T) {
	f := newFixture(t)
	if err := f.hooks.Setup(context.Background(), "/worktrees/shop/feat-login", io.Discard); err != nil {
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
	if err := f.hooks.Setup(ctx, path, io.Discard); err != nil {
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
	if err := f.hooks.Setup(ctx, "/worktrees/shop/feat-login", io.Discard); err != nil {
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

// An agent launched right after Setup, before the background `up` has done
// anything (written no pid, no log), still waits for it: two `compose up
// --build` of one Stack must never run at once.
func TestWhileTheStackStartsTheAgentWaitsAndSeesItsProgress(t *testing.T) {
	f := newFixture(t)
	f.upRuns = true
	ctx := context.Background()
	path := "/worktrees/shop/feat-login"
	if err := f.hooks.Setup(ctx, path, io.Discard); err != nil {
		t.Fatal(err)
	}
	dir := f.hooks.StateDir("feat-login")
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
	f.upEnds()

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

// After a Sandbox restart, agents launched together in one Worktree start
// its stopped Stack once, not once each.
func TestAgentsLaunchedTogetherStartAStoppedStackOnce(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	path := "/worktrees/shop/feat-login"
	if err := f.hooks.Setup(ctx, path, io.Discard); err != nil {
		t.Fatal(err)
	}
	f.hooks.Poll = 5 * time.Millisecond
	var mu sync.Mutex
	ups, started := 0, false
	f.docker.reply = func(args []string) (string, error) {
		switch {
		case slices.Contains(args, "up"):
			mu.Lock()
			ups++
			mu.Unlock()
			time.Sleep(50 * time.Millisecond) // building
			mu.Lock()
			started = true
			mu.Unlock()
		case slices.Contains(args, "ps"):
			mu.Lock()
			defer mu.Unlock()
			if started {
				return "c0ffee\n", nil
			}
		}
		return "", nil
	}
	// fakeDocker serialises its calls: let `up` sleep outside its lock.
	f.docker.unlocked = true
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if id, err := f.hooks.Workbench(ctx, path, io.Discard); err != nil || id != "c0ffee" {
				t.Errorf("id %q, err %v", id, err)
			}
		}()
	}
	wg.Wait()
	if ups != 1 {
		t.Errorf("the Stack was started %d times, want once", ups)
	}
}

func TestAStoppedStackIsStartedAgain(t *testing.T) {
	// E.g. after the Sandbox restarted: nothing brings the Workbench back.
	f := newFixture(t)
	ctx := context.Background()
	path := "/worktrees/shop/feat-login"
	if err := f.hooks.Setup(ctx, path, io.Discard); err != nil {
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
	if err := f.hooks.Setup(ctx, path, io.Discard); err != nil {
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

// Setup has 2 minutes: if an agent is already starting the Stack, it leaves
// that to the agent instead of waiting for it.
func TestSetupLeavesAStackAnAgentIsStartingToIt(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	path := "/worktrees/shop/early"
	release := make(chan struct{})
	upping := make(chan struct{})
	started := false
	f.docker.unlocked = true
	f.docker.reply = func(args []string) (string, error) {
		switch {
		case slices.Contains(args, "up"):
			close(upping)
			<-release // building
			started = true
		case slices.Contains(args, "ps") && started:
			return "c0ffee\n", nil
		}
		return "", nil
	}
	done := make(chan error, 1)
	go func() {
		_, err := f.hooks.Workbench(ctx, path, io.Discard)
		done <- err
	}()
	<-upping
	setup := make(chan error, 1)
	go func() { setup <- f.hooks.Setup(ctx, path, io.Discard) }()
	select {
	case err := <-setup:
		if err != nil {
			t.Errorf("setup: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Error("setup waited for the agent's Stack start")
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if len(f.detached) != 0 {
		t.Errorf("setup detached %v though the Stack was being started", f.detached)
	}
}

// Orca runs the setup hook in a terminal of its own after creating the
// Worktree, so an agent may launch first (or setup was skipped): the agent
// then generates and starts the Stack itself, from the Worktree's checkout.
func TestAnAgentLaunchedBeforeSetupStartsTheStackItself(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
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
	id, err := f.hooks.Workbench(ctx, "/worktrees/shop/early/src", io.Discard)
	if err != nil || id != "c0ffee" {
		t.Fatalf("id %q, err %v", id, err)
	}
	up := f.docker.called("up", "--detach", "--build", "--remove-orphans")
	if up == nil || !slices.Contains(up, "orcasm-early") || !slices.Contains(up, "/worktrees/shop/early/compose.yml") {
		t.Errorf("up call %v", up)
	}
	// Setup, when it runs, then finds the Stack it would have made.
	st, err := worktree.Load(f.hooks.StateDir("early"))
	if err != nil || st.Project != "orcasm-early" {
		t.Errorf("saved Stack %+v, %v", st, err)
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// #18 asks for Worktrees created with `--setup run`. Orca decides from the
// repo's setupRunPolicy when not told (the UI, or `orca-ide worktree create`
// without --setup): run-by-default makes that a run, never a prompt.
func TestOrcaRunsSetupOnEveryWorktreeItCreates(t *testing.T) {
	s := worktree.OrcaSettings()
	if s.HookSettings == nil || s.HookSettings.SetupRunPolicy != "run-by-default" || s.HookSettings.Mode != "auto" {
		t.Fatalf("hook settings %+v", s.HookSettings)
	}
	if s.HookSettings.Scripts.Setup != "/usr/local/bin/orcasm worktree setup" ||
		s.HookSettings.Scripts.Archive != "/usr/local/bin/orcasm worktree archive" {
		t.Errorf("scripts %+v", s.HookSettings.Scripts)
	}
	if s.WorktreeBasePath != "/worktrees" {
		t.Errorf("worktree base path %q", s.WorktreeBasePath)
	}
}

func TestAPathOutsideTheWorktreesIsNoWorktree(t *testing.T) {
	f := newFixture(t)
	for _, p := range []string{"/home/dev/shop", "/worktrees/shop", "/worktrees"} {
		if err := f.hooks.Setup(context.Background(), p, io.Discard); err == nil {
			t.Errorf("Setup(%s) succeeded", p)
		}
	}
}

// sleeper is a process in its own process group, as the background `up` is;
// it is reaped when the test ends.
func sleeper(t *testing.T) *os.Process {
	t.Helper()
	c := exec.Command("sleep", "30")
	c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	exited := make(chan struct{})
	go func() { c.Wait(); close(exited) }()
	t.Cleanup(func() { c.Process.Kill(); <-exited })
	return c.Process
}

func alive(p *os.Process) bool { return p.Signal(syscall.Signal(0)) == nil }

// An `up` killed with the Sandbox leaves its pid behind; by archive time
// that pid may be another process's, which must be left alone.
func TestArchiveLeavesAStalePidsProcessAlone(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	path := "/worktrees/shop/stale"
	if err := f.hooks.Setup(ctx, path, io.Discard); err != nil {
		t.Fatal(err)
	}
	other := sleeper(t)
	dir := f.detached[0][0]
	if err := os.WriteFile(filepath.Join(dir, "up.pid"), []byte(strconv.Itoa(other.Pid)), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := f.hooks.Archive(ctx, path); err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	if !alive(other) {
		t.Error("archive signalled the process holding a stale up.pid")
	}
}

func TestArchiveStopsTheRunningUpBeforeTearingDown(t *testing.T) {
	f := newFixture(t)
	f.upRuns = true
	f.hooks.Poll = 5 * time.Millisecond
	ctx := context.Background()
	path := "/worktrees/shop/running"
	if err := f.hooks.Setup(ctx, path, io.Discard); err != nil {
		t.Fatal(err)
	}
	up := sleeper(t)
	dir := f.detached[0][0]
	if err := os.WriteFile(filepath.Join(dir, "up.pid"), []byte(strconv.Itoa(up.Pid)), 0o644); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- f.hooks.Archive(ctx, path) }()
	deadline := time.Now().Add(2 * time.Second)
	for alive(up) && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if alive(up) {
		t.Fatal("archive did not stop the running up")
	}
	if f.docker.called("down", "--volumes", "--remove-orphans") != nil {
		t.Error("archive tore down while up still held the Stack")
	}
	f.upEnds() // the real `up` exits on SIGTERM, releasing the lock it inherited
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if f.docker.called("down", "--volumes", "--remove-orphans") == nil {
		t.Error("archive never tore down")
	}
}

// An agent starting a stopped Stack holds its lock but records no pid:
// archive waits for it, so the Stack does not come back after teardown.
func TestArchiveWaitsForAnAgentStartingTheStack(t *testing.T) {
	f := newFixture(t)
	f.hooks.Poll = 5 * time.Millisecond
	ctx := context.Background()
	path := "/worktrees/shop/racing"
	release := make(chan struct{})
	upping := make(chan struct{})
	var order []string
	var mu sync.Mutex
	note := func(s string) { mu.Lock(); order = append(order, s); mu.Unlock() }
	f.docker.unlocked = true
	f.docker.reply = func(args []string) (string, error) {
		switch {
		case slices.Contains(args, "up"):
			close(upping)
			<-release
			note("up")
		case slices.Contains(args, "down"):
			note("down")
		case slices.Contains(args, "ps"):
			mu.Lock()
			defer mu.Unlock()
			if slices.Contains(order, "up") {
				return "c0ffee\n", nil
			}
		}
		return "", nil
	}
	agent := make(chan error, 1)
	go func() {
		_, err := f.hooks.Workbench(ctx, path, io.Discard)
		agent <- err
	}()
	<-upping
	archived := make(chan error, 1)
	go func() { archived <- f.hooks.Archive(ctx, path) }()
	time.Sleep(50 * time.Millisecond)
	close(release)
	if err := <-agent; err != nil {
		t.Fatal(err)
	}
	if err := <-archived; err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if !slices.Equal(order, []string{"up", "down"}) {
		t.Errorf("order %v, want the agent's up before archive's down", order)
	}
}

// Whoever starts the Stack may take longer than Orca lets the archive hook
// run (a long image build): archive never tears down without the lock, it
// leaves the teardown to a background `teardown` that waits for it.
func TestArchiveLeavesASlowStartsTeardownToTheBackground(t *testing.T) {
	f := newFixture(t)
	f.upRuns = true
	f.hooks.Poll = 5 * time.Millisecond
	f.hooks.ArchiveWait = 30 * time.Millisecond
	ctx := context.Background()
	path := "/worktrees/shop/slow"
	if err := f.hooks.Setup(ctx, path, io.Discard); err != nil {
		t.Fatal(err)
	}
	dir := f.detached[0][0]
	if err := f.hooks.Archive(ctx, path); err != nil {
		t.Fatal(err)
	}
	if f.docker.called("down", "--volumes", "--remove-orphans") != nil {
		t.Fatal("archive tore down without the Stack's lock")
	}
	if len(f.detached) != 2 || !slices.Equal(f.detached[1], []string{dir, "worktree", "teardown", path}) {
		t.Fatalf("detached %v, want a background teardown", f.detached)
	}
	torn := make(chan error, 1)
	go func() { torn <- f.hooks.Teardown(ctx, path) }()
	time.Sleep(30 * time.Millisecond)
	if f.docker.called("down", "--volumes", "--remove-orphans") != nil {
		t.Fatal("teardown did not wait for the Stack's lock")
	}
	f.upEnds()
	if err := <-torn; err != nil {
		t.Fatal(err)
	}
	if f.docker.called("down", "--volumes", "--remove-orphans") == nil {
		t.Error("teardown never tore down")
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("state dir still there: %v", err)
	}
}
