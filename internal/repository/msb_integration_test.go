//go:build msb

package repository_test

// Runs Copy against a real Sandbox. The sandbox must be running, have git and
// a user named "orca":
//
//	ORCASM_TEST_SANDBOX=<name> go test -tags msb ./internal/repository/

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Betree/Orcasm/internal/msb"
	"github.com/Betree/Orcasm/internal/repository"
)

func TestCopyIntoRealSandbox(t *testing.T) {
	name := os.Getenv("ORCASM_TEST_SANDBOX")
	if name == "" {
		t.Skip("ORCASM_TEST_SANDBOX not set")
	}
	ctx := context.Background()
	sb := msb.CLI{}.Sandbox(name)
	host := newHostRepo(t)
	git(t, host, "remote", "add", "origin", "git@example.com:acme/app.git")
	write(t, filepath.Join(host, "README"), "edited\n")
	write(t, filepath.Join(host, ".gitignore"), ".env\n")
	write(t, filepath.Join(host, ".env"), "SECRET=1\n")
	dest := "/home/orca/copytest-" + filepath.Base(t.TempDir())
	t.Cleanup(func() { sb.Exec(ctx, "", "rm", "-rf", dest) })

	err := repository.Copy(ctx, sb, repository.CopyOptions{
		HostPath: host, GuestPath: dest, Owner: "orca", IgnoredFiles: []string{".env"},
	})
	if err != nil {
		t.Fatal(err)
	}

	out, err := sb.Exec(ctx, "orca", "sh", "-c", `cd "$1" && git branch --show-current && git remote get-url origin && git status --porcelain && cat .env && stat -c %U .git/HEAD`, "sh", dest)
	if err != nil {
		t.Fatal(err)
	}
	want := "feature\ngit@example.com:acme/app.git\n M README\n?? .gitignore\nSECRET=1\norca\n"
	if out != want {
		t.Errorf("got\n%s\nwant\n%s", out, want)
	}
	// Root (msb exec's default user) can fetch from the copy owned by another user.
	if _, err := sb.Exec(ctx, "", "git", "ls-remote", dest); err != nil {
		t.Errorf("root cannot read the copy (host fetch would fail): %v", err)
	}
	if left, _ := sb.Exec(ctx, "", "sh", "-c", "ls -d /tmp/orcasm-copy-* 2>/dev/null || true"); strings.TrimSpace(left) != "" {
		t.Errorf("staging dir left behind: %s", left)
	}
}
