package cli_test

import (
	"testing"

	"github.com/Betree/Orcasm/internal/cli"
)

func TestTheUpgradeNoticeShowsOnlyToAPersonAtAHostCommand(t *testing.T) {
	for _, c := range []struct {
		args []string
		tty  bool
		env  map[string]string
		want bool
	}{
		{args: []string{"ls"}, tty: true, want: true},
		{args: []string{}, tty: true, want: true},
		{args: []string{"ls"}, tty: false, want: false},
		{args: []string{"ls"}, tty: true, env: map[string]string{"ORCASM_NO_UPGRADE_NOTICE": "1"}, want: false},
		// It does it already.
		{args: []string{"upgrade"}, tty: true, want: false},
		// Runs in the Sandbox, for Orca's hooks and agents.
		{args: []string{"worktree", "workbench"}, tty: true, want: false},
	} {
		if got := cli.WantsUpgradeNotice(c.args, c.tty, c.env); got != c.want {
			t.Errorf("orcasm %q (tty %v, env %v): notice %v, want %v", c.args, c.tty, c.env, got, c.want)
		}
	}
}
