package sandbox

// The agent wrappers (internal/workbench): /usr/local/bin/<harness> runs the
// real harness from the harness layer (/opt/orcasm/harness, see
// provision.sh) on Main, or in the Workbench inside a Worktree.

import "github.com/Betree/Orcasm/internal/workbench"

// agentsInstallArgv is the root command writing the wrapper as each
// harness's command. It runs before Orca first starts: Orca installs an
// agent's status hooks only if the agent's command is on its PATH at start.
func agentsInstallArgv(harnesses []string) []string {
	var files []guestFile
	for _, h := range harnesses {
		files = append(files, guestFile{path: "/usr/local/bin/" + h, content: string(workbench.AgentWrapper), mode: "0755"})
	}
	return installArgv("install-agents", files, "")
}
