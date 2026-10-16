package sandbox_test

import (
	"strings"
	"testing"

	"github.com/Betree/Orcasm/internal/guest"
	"github.com/Betree/Orcasm/internal/sandbox"
	"github.com/Betree/Orcasm/internal/workbench"
)

// The bash commands installed in the Sandbox default to the same layout as
// package guest.
func TestTheSandboxScriptsUseTheGuestLayout(t *testing.T) {
	for _, c := range []struct{ script, want string }{
		{sandbox.StackCommand(), "${" + guest.ConfDirEnv + ":-" + guest.ConfDir + "}"},
		{sandbox.StackCommand(), "$conf/" + guest.MainConfig + `"`},
		{sandbox.StackCommand(), "$conf/" + guest.MainComposeFiles + `"`},
		{string(workbench.AgentWrapper), "${ORCASM_WORKTREES:-" + guest.WorktreesDir + "}"},
	} {
		if !strings.Contains(c.script, c.want) {
			t.Errorf("script lacks %q", c.want)
		}
	}
}
