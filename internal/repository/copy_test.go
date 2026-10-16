package repository_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Betree/Orcasm/internal/repository"
)

// localSandbox stands in for a Sandbox by running everything on this machine:
// guest paths are plain local paths and the guest user is ignored.
type localSandbox struct{}

func (localSandbox) Exec(ctx context.Context, _ string, argv ...string) (string, error) {
	out, err := exec.CommandContext(ctx, argv[0], argv[1:]...).Output()
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return string(out), errors.New(err.Error() + ": " + string(ee.Stderr))
	}
	return string(out), err
}

func (localSandbox) CopyIn(ctx context.Context, hostPath, guestPath string) error {
	return exec.CommandContext(ctx, "cp", "-a", hostPath, guestPath).Run()
}

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

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// newHostRepo makes a repository with two commits on branch "feature" and a
// "main" branch one commit behind.
func newHostRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	git(t, dir, "init", "-q", "-b", "main")
	write(t, filepath.Join(dir, "README"), "v1\n")
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-q", "-m", "first")
	git(t, dir, "checkout", "-q", "-b", "feature")
	write(t, filepath.Join(dir, "README"), "v2\n")
	git(t, dir, "commit", "-q", "-am", "second")
	return dir
}

func currentUser(t *testing.T) string {
	t.Helper()
	u, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	return u.Username
}

func copyRepo(t *testing.T, host string, ignored ...string) string {
	t.Helper()
	dest := filepath.Join(t.TempDir(), "project")
	err := repository.Copy(context.Background(), localSandbox{}, repository.CopyOptions{
		HostPath:     host,
		GuestPath:    dest,
		Owner:        currentUser(t),
		IgnoredFiles: ignored,
	})
	if err != nil {
		t.Fatal(err)
	}
	return dest
}

func TestCopyBringsHistoryAndCurrentBranch(t *testing.T) {
	host := newHostRepo(t)
	dest := copyRepo(t, host)

	if got := git(t, dest, "branch", "--show-current"); got != "feature" {
		t.Errorf("current branch = %q, want feature", got)
	}
	if got := git(t, dest, "log", "--format=%s", "main"); got != "first" {
		t.Errorf("main log = %q, want first", got)
	}
	if got := git(t, dest, "rev-parse", "HEAD"); got != git(t, host, "rev-parse", "HEAD") {
		t.Errorf("HEAD = %s, want host HEAD", got)
	}
	if got := read(t, filepath.Join(dest, "README")); got != "v2\n" {
		t.Errorf("README = %q", got)
	}
	if got := git(t, dest, "status", "--porcelain"); got != "" {
		t.Errorf("status not clean: %q", got)
	}
}

func TestCopyKeepsDetachedHead(t *testing.T) {
	host := newHostRepo(t)
	git(t, host, "checkout", "-q", "--detach", "main")

	dest := copyRepo(t, host)

	if got := git(t, dest, "branch", "--show-current"); got != "" {
		t.Errorf("expected detached HEAD, on branch %q", got)
	}
	if got := git(t, dest, "log", "-1", "--format=%s"); got != "first" {
		t.Errorf("HEAD commit = %q, want first", got)
	}
}

func TestCopyRefusesExistingDestination(t *testing.T) {
	host := newHostRepo(t)
	dest := t.TempDir()

	err := repository.Copy(context.Background(), localSandbox{}, repository.CopyOptions{
		HostPath: host, GuestPath: dest, Owner: currentUser(t),
	})
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("err = %v, want already exists", err)
	}
}

func TestCopyPointsOriginAtHostOrigin(t *testing.T) {
	host := newHostRepo(t)
	origin := t.TempDir()
	git(t, origin, "init", "-q", "--bare")
	git(t, host, "remote", "add", "origin", origin)
	git(t, host, "push", "-q", "-u", "origin", "feature")
	git(t, host, "remote", "set-url", "--push", "origin", "git@example.com:acme/app.git")

	dest := copyRepo(t, host)

	if got := git(t, dest, "remote", "get-url", "origin"); got != origin {
		t.Errorf("origin fetch url = %q, want %q", got, origin)
	}
	if got := git(t, dest, "remote", "get-url", "--push", "origin"); got != "git@example.com:acme/app.git" {
		t.Errorf("origin push url = %q", got)
	}
	if got := git(t, dest, "rev-parse", "--abbrev-ref", "feature@{upstream}"); got != "origin/feature" {
		t.Errorf("feature upstream = %q, want origin/feature", got)
	}
	git(t, dest, "fetch", "-q", "origin")
}

func TestCopyBringsOnlyPickedIgnoredFiles(t *testing.T) {
	host := newHostRepo(t)
	write(t, filepath.Join(host, ".gitignore"), ".env\nconfig/\nnode_modules/\n")
	git(t, host, "add", ".gitignore")
	git(t, host, "commit", "-q", "-m", "ignore")
	write(t, filepath.Join(host, ".env"), "SECRET=1\n")
	write(t, filepath.Join(host, "config", "local.yml"), "a: 1\n")
	write(t, filepath.Join(host, "node_modules", "dep", "index.js"), "x\n")

	dest := copyRepo(t, host, ".env", "config")

	if got := read(t, filepath.Join(dest, ".env")); got != "SECRET=1\n" {
		t.Errorf(".env = %q", got)
	}
	if got := read(t, filepath.Join(dest, "config", "local.yml")); got != "a: 1\n" {
		t.Errorf("config/local.yml = %q", got)
	}
	if _, err := os.Stat(filepath.Join(dest, "node_modules")); !os.IsNotExist(err) {
		t.Errorf("node_modules was not picked but exists (err = %v)", err)
	}
	if got := git(t, dest, "status", "--porcelain"); got != "" {
		t.Errorf("status not clean: %q", got)
	}
}

func TestCopyBringsUncommittedChanges(t *testing.T) {
	host := newHostRepo(t)
	write(t, filepath.Join(host, "tracked"), "committed\n")
	write(t, filepath.Join(host, "doomed"), "committed\n")
	write(t, filepath.Join(host, "removed"), "committed\n")
	git(t, host, "add", ".")
	git(t, host, "commit", "-q", "-m", "third")
	git(t, host, "rm", "-q", "removed")
	write(t, filepath.Join(host, "README"), "edited\n")
	write(t, filepath.Join(host, "tracked"), "staged edit\n")
	git(t, host, "add", "tracked")
	write(t, filepath.Join(host, "sub", "new file"), "untracked\n")
	if err := os.Remove(filepath.Join(host, "doomed")); err != nil {
		t.Fatal(err)
	}

	dest := copyRepo(t, host)

	for name, want := range map[string]string{"README": "edited\n", "tracked": "staged edit\n", "sub/new file": "untracked\n"} {
		if got := read(t, filepath.Join(dest, name)); got != want {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}
	for _, name := range []string{"doomed", "removed"} {
		if _, err := os.Stat(filepath.Join(dest, name)); !os.IsNotExist(err) {
			t.Errorf("%s should be deleted, stat err = %v", name, err)
		}
	}
	want := "M README\n D doomed\n D removed\n M tracked\n?? sub/"
	if got := git(t, dest, "status", "--porcelain"); got != want {
		t.Errorf("status =\n%s\nwant\n%s", got, want)
	}
}
