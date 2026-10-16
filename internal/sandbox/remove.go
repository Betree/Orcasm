package sandbox

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/Betree/Orcasm/internal/repository"
)

// RemoveOptions tune Remove.
type RemoveOptions struct {
	// Force removes without looking for unsaved work.
	Force bool
	// Confirm is shown the unsaved work (or why it could not be checked) and
	// returns true to remove anyway. Nil means no.
	Confirm func(work []string) bool
}

// Remove deletes the named Sandbox and the host state pointing at it: its SSH
// entry and the `orcasm-<name>` git remote in the Project. Unless forced, it
// first looks for unsaved work in Main and the Worktrees and refuses if any
// is found (or cannot be ruled out) and Confirm does not approve.
func (m Manager) Remove(ctx context.Context, name string, opts RemoveOptions) error {
	s, err := m.Get(ctx, name)
	if err != nil {
		return err
	}
	if !opts.Force {
		work := m.unsavedWork(ctx, s)
		if len(work) > 0 && opts.Confirm != nil && !opts.Confirm(work) {
			return fmt.Errorf("not removing %q", name)
		}
		if len(work) > 0 && opts.Confirm == nil {
			return fmt.Errorf("not removing %q, it may hold unsaved work:\n  - %s\nfetch or push it first, or rerun with --force",
				name, strings.Join(work, "\n  - "))
		}
	}
	if err := m.MSB.Remove(ctx, name); err != nil {
		return err
	}
	var errs []error
	if m.SSH != nil {
		if err := m.SSH.Remove(name); err != nil {
			errs = append(errs, fmt.Errorf("removing SSH entry: %w", err))
		}
	}
	if s.Folder != "" {
		if err := repository.RemoveSandboxRemote(ctx, s.Folder, name); err != nil {
			errs = append(errs, fmt.Errorf("removing git remote: %w", err))
		}
	}
	return errors.Join(errs...)
}

// unsavedWork lists the work Remove would lose; a check that cannot run
// counts as possible unsaved work. On a stopped Sandbox msb exec still runs
// (in a transient boot), so stopped Sandboxes are checked too.
func (m Manager) unsavedWork(ctx context.Context, s Sandbox) []string {
	if s.Main == "" {
		return []string{"Main's location is unknown, so unsaved work cannot be checked"}
	}
	work, err := repository.UnsavedWork(ctx, guest{m.MSB, s.Name}, s.Main, s.Folder)
	if err != nil {
		return []string{"could not check for unsaved work: " + err.Error()}
	}
	return work
}

// guest runs commands in one Sandbox.
type guest struct {
	msb  MSB
	name string
}

func (g guest) Exec(ctx context.Context, user string, argv ...string) (string, error) {
	return g.msb.Exec(ctx, g.name, user, argv...)
}
