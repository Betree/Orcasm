// Package workbench holds what sends a Worktree's agents into its Workbench
// (ADR 0002).
package workbench

import _ "embed"

// AgentWrapper is installed in the Sandbox as each harness's command (claude,
// codex, ...) ahead of the real one on Orca's PATH. Proven in spike #16; see
// docs/research/orca.md, "#16 spike".
//
//go:embed agent-wrapper.sh
var AgentWrapper []byte
