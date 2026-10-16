// Package repository copies a Repository from the host into a Sandbox and
// wires the two together (host git remote, Orca registration).
//
// There is no host mount (ADR 0001): the Repository travels as a git bundle
// (full history) plus a tar of the working tree, so the Sandbox copy has the
// same commits, branch and uncommitted changes as the host checkout.
package repository

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
)

// Sandbox is the narrow slice of a Sandbox this package needs.
type Sandbox interface {
	// Exec runs argv in the Sandbox as user ("" means the default user) and
	// returns its stdout.
	Exec(ctx context.Context, user string, argv ...string) (string, error)
	// CopyIn copies a host file to an absolute path in the Sandbox.
	CopyIn(ctx context.Context, hostPath, guestPath string) error
}

// CopyOptions describes one Repository copy.
type CopyOptions struct {
	// HostPath is the Repository's working tree on the host.
	HostPath string
	// GuestPath is where the Repository lands in the Sandbox. It must not exist.
	GuestPath string
	// Owner is the guest user that will own the copy (the user Orca runs as).
	Owner string
	// IgnoredFiles are gitignored paths, relative to HostPath, picked in the
	// Wizard to travel with the copy (e.g. ".env"). Directories are copied whole.
	IgnoredFiles []string
}

// Copy copies the Repository at opts.HostPath into the Sandbox at
// opts.GuestPath: every ref, the checked-out branch (or detached HEAD), the
// working tree's uncommitted and untracked changes, and opts.IgnoredFiles.
// origin points at the host's origin. Staged-vs-unstaged state is not kept:
// all uncommitted changes arrive unstaged.
func Copy(ctx context.Context, sb Sandbox, opts CopyOptions) error {
	local, err := os.MkdirTemp("", "orcasm-copy-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(local)

	bundle := filepath.Join(local, "repo.bundle")
	if _, err := hostGit(ctx, opts.HostPath, "bundle", "create", "-q", bundle, "--all"); err != nil {
		return fmt.Errorf("bundle %s: %w", opts.HostPath, err)
	}
	head, err := hostHead(ctx, opts.HostPath)
	if err != nil {
		return err
	}
	tree := filepath.Join(local, "worktree.tar")
	deleted := filepath.Join(local, "deleted")
	if err := packWorkingTree(ctx, opts.HostPath, opts.IgnoredFiles, tree, deleted); err != nil {
		return err
	}
	config := filepath.Join(local, "config")
	if err := writeConfig(ctx, opts.HostPath, config); err != nil {
		return err
	}

	stage, err := sb.Exec(ctx, "", "mktemp", "-d", "/tmp/orcasm-copy-XXXXXX")
	if err != nil {
		return fmt.Errorf("create staging dir in sandbox: %w", err)
	}
	stage = strings.TrimSpace(stage)
	defer sb.Exec(context.WithoutCancel(ctx), "", "rm", "-rf", stage)

	for _, f := range []string{bundle, tree, deleted, config} {
		if err := sb.CopyIn(ctx, f, path.Join(stage, filepath.Base(f))); err != nil {
			return fmt.Errorf("copy %s into sandbox: %w", filepath.Base(f), err)
		}
	}
	if _, err := sb.Exec(ctx, "", "sh", "-euc", guestScript, "orcasm-copy",
		stage, opts.GuestPath, opts.Owner, head); err != nil {
		return fmt.Errorf("unpack repository in sandbox: %w", err)
	}
	return nil
}

// guestScript unpacks a staged copy. Positional args: stage dir, destination,
// owner, HEAD (a "refs/heads/..." name or a commit id).
const guestScript = `
stage=$1 dest=$2 owner=$3 head=$4
if [ -e "$dest" ]; then echo "$dest already exists" >&2; exit 1; fi
mkdir -p "$dest"
cd "$dest"
git init -q
cat "$stage/config" >> .git/config
git fetch -q --update-head-ok "$stage/repo.bundle" 'refs/*:refs/*'
case "$head" in
  refs/*) git symbolic-ref HEAD "$head" ;;
  *) git update-ref --no-deref HEAD "$head" ;;
esac
git reset -q --hard
xargs -0 -r rm -rf -- < "$stage/deleted"
tar -xf "$stage/worktree.tar"
chown -R "$owner:" "$dest"
# Let other users (root via msb exec or ssh -o User=root) run git on the copy
# without "dubious ownership" errors; upload-pack (host fetch) checks this too.
if [ "$(id -un)" != "$owner" ]; then
  for safe in "$dest" "$dest/.git"; do # upload-pack checks the gitdir
    git config --system --get-all safe.directory | grep -qxF "$safe" ||
      git config --system --add safe.directory "$safe"
  done
fi
`

// packWorkingTree writes a tar of every non-ignored file in the host working
// tree plus the ignored picks, and a NUL-separated list of HEAD paths that are
// gone from the working tree.
func packWorkingTree(ctx context.Context, repo string, ignored []string, tarPath, deletedPath string) error {
	listed, err := hostGit(ctx, repo, "ls-files", "-z", "--cached", "--others", "--exclude-standard")
	if err != nil {
		return err
	}
	var files []string
	for _, f := range append(splitNUL(listed), ignored...) {
		if _, err := os.Lstat(filepath.Join(repo, f)); err == nil {
			files = append(files, f)
		} else if !os.IsNotExist(err) {
			return err
		}
	}
	inHead, err := hostGit(ctx, repo, "ls-tree", "-r", "-z", "--name-only", "HEAD")
	if err != nil {
		return err
	}
	var gone []byte
	for _, f := range splitNUL(inHead) {
		if _, err := os.Lstat(filepath.Join(repo, f)); os.IsNotExist(err) {
			gone = append(append(gone, f...), 0)
		}
	}
	if err := os.WriteFile(deletedPath, gone, 0o644); err != nil {
		return err
	}

	cmd := exec.CommandContext(ctx, "tar", "-C", repo, "--null", "-T", "-", "-cf", tarPath)
	cmd.Stdin = strings.NewReader(strings.Join(files, "\x00"))
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("tar working tree: %w: %s", err, out)
	}
	return nil
}

// writeConfig writes a git config fragment with the host's origin remote and
// branch upstreams, so the Sandbox copy pushes and pulls where the host does.
func writeConfig(ctx context.Context, repo, out string) error {
	if err := os.WriteFile(out, nil, 0o644); err != nil {
		return err
	}
	entries, err := hostGit(ctx, repo, "config", "--local", "-z", "--get-regexp", `^(remote\.origin|branch)\.`)
	if err != nil {
		// Exit status 1 means no matching entries.
		var ee *exec.ExitError
		if errors.As(err, &ee) && ee.ExitCode() == 1 {
			return nil
		}
		return err
	}
	for _, e := range splitNUL(entries) {
		key, value, _ := strings.Cut(e, "\n")
		if _, err := hostGit(ctx, repo, "config", "-f", out, "--add", key, value); err != nil {
			return err
		}
	}
	return nil
}

func splitNUL(s string) []string {
	var out []string
	for _, f := range strings.Split(s, "\x00") {
		if f != "" {
			out = append(out, f)
		}
	}
	return out
}

// hostHead returns the host checkout's HEAD as a branch ref, or a commit id
// when detached.
func hostHead(ctx context.Context, repo string) (string, error) {
	if ref, err := hostGit(ctx, repo, "symbolic-ref", "-q", "HEAD"); err == nil {
		return ref, nil
	}
	return hostGit(ctx, repo, "rev-parse", "HEAD")
}

func hostGit(ctx context.Context, repo string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", repo}, args...)...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(string(out)), nil
}
