package cli

// The daily upgrade notice: once a day, after a host command, one line on
// stderr naming the Sandboxes an upgrade is pending for (ADR 0006).

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/Betree/Orcasm/internal/notice"
	"github.com/Betree/Orcasm/internal/sandbox"
)

// noticeBudget bounds the whole check, network included: it must never
// slow a command noticeably, so a slow network means no notice.
const noticeBudget = time.Second

// noNoticeCommands already upgrade, or run in the Sandbox (worktree).
var noNoticeCommands = map[string]bool{"upgrade": true, "self-update": true, "worktree": true}

// noticeWanted reports whether the notice may follow cmd: only for a
// person at a terminal (not scripts or CI, which also set
// ORCASM_NO_UPGRADE_NOTICE=1), and only after host commands that do not
// upgrade already.
func noticeWanted(cmd *cobra.Command, tty bool, getenv func(string) string) bool {
	if !tty || getenv("ORCASM_NO_UPGRADE_NOTICE") != "" {
		return false
	}
	for c := cmd; c != nil && c.HasParent(); c = c.Parent() {
		if noNoticeCommands[c.Name()] {
			return false
		}
	}
	return true
}

// upgradeNotice is the root's PersistentPostRun, for an orcasm at info. It
// fails silently.
func upgradeNotice(info BuildInfo) func(*cobra.Command, []string) {
	return func(cmd *cobra.Command, _ []string) { printUpgradeNotice(cmd, info) }
}

func printUpgradeNotice(cmd *cobra.Command, info BuildInfo) {
	if !noticeWanted(cmd, isTerminal(os.Stdout), os.Getenv) {
		return
	}
	cache, err := os.UserCacheDir()
	if err != nil {
		return
	}
	daily := notice.Daily{Stamp: filepath.Join(cache, "orcasm", "upgrade-notice")}
	if !daily.Due() || daily.Mark() != nil {
		return
	}
	ctx, cancel := context.WithTimeout(cmd.Context(), noticeBudget)
	defer cancel()
	src := sandbox.NewOrcaSource(info.Version)
	src.Timeout = noticeBudget
	orca, err := src.Desired(ctx, false)
	if err != nil {
		return
	}
	m, err := newManager()
	if err != nil {
		return
	}
	pending, err := m.Pending(ctx, sandbox.Installed{Orca: orca.Version, Orcasm: info.Version})
	if err != nil {
		return
	}
	if line := sandbox.Notice(pending); line != "" {
		fmt.Fprintf(cmd.ErrOrStderr(), "orcasm: %s\n", line)
	}
}

func isTerminal(f *os.File) bool {
	fi, err := f.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}
