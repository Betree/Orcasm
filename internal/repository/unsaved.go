package repository

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// Runner runs commands in a Sandbox (msb.Sandbox implements it).
type Runner interface {
	Exec(ctx context.Context, user string, argv ...string) (string, error)
}

// UnsavedWork lists what would be lost if the Sandbox were removed: Main
// (at mainPath in the guest) or any of its Worktrees having uncommitted
// changes, and commits that are neither on a remote nor already in the
// host Repository at hostRepo. Each entry is a human-readable line.
func UnsavedWork(ctx context.Context, sb Runner, mainPath, hostRepo string) ([]string, error) {
	trees, err := worktrees(ctx, sb, mainPath)
	if err != nil {
		return nil, err
	}
	var work []string
	var heads []string
	for _, tree := range trees {
		heads = append(heads, tree.head)
		status, err := guestGit(ctx, sb, tree.path, "status", "--porcelain")
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(status) != "" {
			work = append(work, tree.path+": uncommitted changes")
		}
	}

	// Commits reachable from a branch or a Worktree's (maybe detached) HEAD
	// that no remote-tracking ref has. The copy brought the host's
	// remote-tracking refs along, so only Sandbox-made commits remain, minus
	// those the host has since fetched.
	args := append([]string{"rev-list"}, heads...)
	out, err := guestGit(ctx, sb, mainPath, append(args, "--branches", "--not", "--remotes")...)
	if err != nil {
		return nil, err
	}
	var lost int
	for _, c := range strings.Fields(out) {
		if !hostHasCommit(ctx, hostRepo, c) {
			lost++
		}
	}
	if lost > 0 {
		work = append(work, fmt.Sprintf("%s: %d %s not pushed and not fetched by the host", mainPath, lost, plural(lost, "commit")))
	}
	return work, nil
}

type worktree struct{ path, head string }

// worktrees returns Main and every git worktree attached to it.
func worktrees(ctx context.Context, sb Runner, mainPath string) ([]worktree, error) {
	out, err := guestGit(ctx, sb, mainPath, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, err
	}
	var trees []worktree
	for _, line := range strings.Split(out, "\n") {
		if p, ok := strings.CutPrefix(line, "worktree "); ok {
			trees = append(trees, worktree{path: p})
		} else if h, ok := strings.CutPrefix(line, "HEAD "); ok && len(trees) > 0 {
			trees[len(trees)-1].head = h
		}
	}
	return trees, nil
}

// hostHasCommit reports whether the host Repository holds commit c. A missing
// or broken host Repository holds nothing.
func hostHasCommit(ctx context.Context, hostRepo, c string) bool {
	return exec.CommandContext(ctx, "git", "-C", hostRepo, "cat-file", "-e", c+"^{commit}").Run() == nil
}

func plural(n int, word string) string {
	if n == 1 {
		return word
	}
	return word + "s"
}

// guestGit runs git in dir inside the Sandbox. msb exec runs as root while
// Main and the Worktrees belong to the guest users, so ownership checks are
// waived for this read-only inspection.
func guestGit(ctx context.Context, sb Runner, dir string, args ...string) (string, error) {
	argv := append([]string{"git", "-c", "safe.directory=*", "-C", dir}, args...)
	return sb.Exec(ctx, "", argv...)
}
