package sandbox_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Betree/Orcasm/internal/msb"
	"github.com/Betree/Orcasm/internal/repository"
	"github.com/Betree/Orcasm/internal/sandbox"
)

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// fakeSSH records which Sandboxes' SSH entries were removed.
type fakeSSH struct{ removed []string }

func (f *fakeSSH) Remove(name string) error {
	f.removed = append(f.removed, name)
	return nil
}

// project sets up a host Project with the Sandbox "app" whose Main (a local
// clone standing in for the guest copy) is returned along with the fakes.
func project(t *testing.T) (host, main string, f *fakeMSB, ssh *fakeSSH) {
	t.Helper()
	host = t.TempDir()
	git(t, host, "init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(host, "README"), []byte("v1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, host, "add", ".")
	git(t, host, "commit", "-q", "-m", "first")
	main = filepath.Join(t.TempDir(), "app")
	git(t, host, "clone", "-q", host, main)
	if err := repository.AddSandboxRemote(context.Background(), host, "app", main); err != nil {
		t.Fatal(err)
	}
	f = newFakeMSB()
	f.add("app", msb.Running, sandbox.CreateLabels(host, main)...)
	return host, main, f, &fakeSSH{}
}

func TestRemoveCleanSandboxCleansHostState(t *testing.T) {
	host, _, f, ssh := project(t)
	m := sandbox.Manager{MSB: f, SSH: ssh}

	err := m.Remove(context.Background(), "app", sandbox.RemoveOptions{
		Confirm: func([]string) bool { t.Error("asked to confirm with no unsaved work"); return false },
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, ok := f.sandboxes["app"]; ok {
		t.Error("sandbox not removed")
	}
	if len(ssh.removed) != 1 || ssh.removed[0] != "app" {
		t.Errorf("ssh entries removed = %v, want [app]", ssh.removed)
	}
	if got := git(t, host, "remote"); got != "" {
		t.Errorf("host remotes left: %q", got)
	}
}

func TestRemoveRefusesUnsavedWorkWithoutConfirmation(t *testing.T) {
	_, main, f, ssh := project(t)
	if err := os.WriteFile(filepath.Join(main, "README"), []byte("agent edit\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var asked []string
	m := sandbox.Manager{MSB: f, SSH: ssh}

	err := m.Remove(context.Background(), "app", sandbox.RemoveOptions{
		Confirm: func(work []string) bool { asked = work; return false },
	})

	if err == nil || !strings.Contains(err.Error(), "uncommitted") {
		t.Fatalf("err = %v, want refusal listing the uncommitted changes", err)
	}
	if len(asked) != 1 || !strings.Contains(asked[0], main) {
		t.Errorf("confirm asked about %q, want Main's changes", asked)
	}
	if _, ok := f.sandboxes["app"]; !ok || len(ssh.removed) != 0 {
		t.Error("removed despite the refusal")
	}
}

func TestRemoveWithUnsavedWorkProceedsWhenConfirmed(t *testing.T) {
	_, main, f, ssh := project(t)
	if err := os.WriteFile(filepath.Join(main, "README"), []byte("agent edit\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := sandbox.Manager{MSB: f, SSH: ssh}

	if err := m.Remove(context.Background(), "app", sandbox.RemoveOptions{
		Confirm: func([]string) bool { return true },
	}); err != nil {
		t.Fatal(err)
	}

	if _, ok := f.sandboxes["app"]; ok {
		t.Error("sandbox not removed")
	}
}

func TestRemoveForcedSkipsTheCheck(t *testing.T) {
	_, main, f, ssh := project(t)
	if err := os.WriteFile(filepath.Join(main, "README"), []byte("agent edit\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := sandbox.Manager{MSB: f, SSH: ssh}

	if err := m.Remove(context.Background(), "app", sandbox.RemoveOptions{Force: true}); err != nil {
		t.Fatal(err)
	}

	if _, ok := f.sandboxes["app"]; ok {
		t.Error("sandbox not removed")
	}
}

func TestRemoveRefusesWhenWorkCannotBeChecked(t *testing.T) {
	f := newFakeMSB()
	f.add("app", msb.Running, "orcasm=1") // no Main label
	m := sandbox.Manager{MSB: f, SSH: &fakeSSH{}}

	err := m.Remove(context.Background(), "app", sandbox.RemoveOptions{})

	if err == nil || !strings.Contains(err.Error(), "--force") {
		t.Fatalf("err = %v, want refusal pointing at --force", err)
	}
	if _, ok := f.sandboxes["app"]; !ok {
		t.Error("removed despite the refusal")
	}
}
