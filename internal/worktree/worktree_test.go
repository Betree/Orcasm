package worktree_test

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Betree/Orcasm/internal/worktree"
)

// fakeDocker answers `compose ... config --services` with services and
// records every call.
type fakeDocker struct {
	services []string
	calls    [][]string
}

func (f *fakeDocker) run(_ context.Context, args ...string) ([]byte, error) {
	f.calls = append(f.calls, args)
	return []byte(strings.Join(f.services, "\n") + "\n"), nil
}

// called reports whether a docker call ended with the given args.
func (f *fakeDocker) called(suffix ...string) []string {
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
