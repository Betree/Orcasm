package repository_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Betree/Orcasm/internal/repository"
)

func unsaved(t *testing.T, main, host string) []string {
	t.Helper()
	work, err := repository.UnsavedWork(context.Background(), localSandbox{}, main, host)
	if err != nil {
		t.Fatal(err)
	}
	return work
}

func TestFreshCopyHasNoUnsavedWork(t *testing.T) {
	host := newHostRepo(t)
	main := copyRepo(t, host)

	if work := unsaved(t, main, host); len(work) != 0 {
		t.Errorf("unsaved work = %q, want none", work)
	}
}

func TestUncommittedChangesInMainAreUnsaved(t *testing.T) {
	host := newHostRepo(t)
	main := copyRepo(t, host)
	write(t, filepath.Join(main, "README"), "edited in the Sandbox\n")

	work := unsaved(t, main, host)

	if len(work) != 1 || !strings.Contains(work[0], "uncommitted") || !strings.Contains(work[0], main) {
		t.Errorf("unsaved work = %q, want one uncommitted entry for %s", work, main)
	}
}

func TestUncommittedChangesInAWorktreeAreUnsaved(t *testing.T) {
	host := newHostRepo(t)
	main := copyRepo(t, host)
	tree := filepath.Join(t.TempDir(), "fix-login")
	git(t, main, "worktree", "add", "-q", "-b", "fix-login", tree)
	write(t, filepath.Join(tree, "new.txt"), "untracked\n")

	work := unsaved(t, main, host)

	if len(work) != 1 || !strings.Contains(work[0], "uncommitted") || !strings.Contains(work[0], tree) {
		t.Errorf("unsaved work = %q, want one uncommitted entry for %s", work, tree)
	}
}

func TestCommitsOnlyInTheSandboxAreUnsaved(t *testing.T) {
	host := newHostRepo(t)
	main := copyRepo(t, host)
	tree := filepath.Join(t.TempDir(), "fix-login")
	git(t, main, "worktree", "add", "-q", "--detach", tree)
	write(t, filepath.Join(tree, "fix.txt"), "fix\n")
	git(t, tree, "add", ".")
	git(t, tree, "commit", "-q", "-m", "fix on a detached Worktree")

	work := unsaved(t, main, host)

	if len(work) != 1 || !strings.Contains(work[0], "1 commit") {
		t.Errorf("unsaved work = %q, want one entry for 1 commit", work)
	}
}

func TestCommitsTheHostAlreadyFetchedAreSaved(t *testing.T) {
	host := newHostRepo(t)
	main := copyRepo(t, host)
	write(t, filepath.Join(main, "fix.txt"), "fix\n")
	git(t, main, "add", ".")
	git(t, main, "commit", "-q", "-m", "fix")
	git(t, host, "fetch", "-q", main, "feature:refs/remotes/orcasm-dev/feature")

	if work := unsaved(t, main, host); len(work) != 0 {
		t.Errorf("unsaved work = %q, want none", work)
	}
}

func TestCommitsOnARemoteAreSaved(t *testing.T) {
	host := newHostRepo(t)
	origin := t.TempDir()
	git(t, origin, "init", "-q", "--bare")
	git(t, host, "remote", "add", "origin", origin)
	main := copyRepo(t, host)
	write(t, filepath.Join(main, "fix.txt"), "fix\n")
	git(t, main, "add", ".")
	git(t, main, "commit", "-q", "-m", "fix")
	git(t, main, "push", "-q", "origin", "feature")

	if work := unsaved(t, main, host); len(work) != 0 {
		t.Errorf("unsaved work = %q, want none", work)
	}
}
