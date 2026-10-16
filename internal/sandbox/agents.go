package sandbox

// The agent wrappers (internal/workbench): /usr/local/bin/<harness> runs the
// real harness from the harness layer (/opt/orcasm/harness, see
// provision.sh) on Main, or in the Workbench inside a Worktree.

import "github.com/Betree/Orcasm/internal/workbench"

// agentsInstaller writes the wrapper as each harness's command.
// ORCASM_ROOT relocates it (tests).
// Args: install-agents <wrapper> [harness...].
const agentsInstaller = `set -euo pipefail
root=${ORCASM_ROOT:-}
wrapper=$1
shift
install -d "$root/usr/local/bin"
for h in "$@"; do
  printf '%s' "$wrapper" >"$root/usr/local/bin/$h.new"
  chmod 0755 "$root/usr/local/bin/$h.new"
  mv -f "$root/usr/local/bin/$h.new" "$root/usr/local/bin/$h"
done
`

// agentsInstallArgv is the root command installing the wrappers. It runs
// before Orca first starts: Orca installs an agent's status hooks only if
// the agent's command is on its PATH at start.
func agentsInstallArgv(harnesses []string) []string {
	return append([]string{"bash", "-c", agentsInstaller, "install-agents", string(workbench.AgentWrapper)}, harnesses...)
}
