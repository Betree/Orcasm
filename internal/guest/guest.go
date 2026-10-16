// Package guest is the layout of orcasm inside the Sandbox: its user and the
// paths both provisioning (on the host) and the commands running in the
// Sandbox (orcasm worktree, stack) agree on.
package guest

import (
	"os"
	"path/filepath"
)

const (
	// User owns Main and runs Orca and agents (ADR 0003; SSH logs in as
	// it, see docs/research/microsandbox.md §11).
	User = "dev"
	// Home is User's home directory.
	Home = "/home/" + User
	// Binary is where provisioning installs orcasm.
	Binary = "/usr/local/bin/orcasm"
	// WorktreesDir is where Orca creates Worktrees (owned by User):
	// WorktreesDir/<Repository>/<Worktree>.
	WorktreesDir = "/worktrees"

	// ConfDir holds orcasm's config in the Sandbox; ConfDirEnv relocates
	// it for the commands reading it (tests). stack.sh has the same default.
	ConfDir    = "/etc/orcasm"
	ConfDirEnv = "ORCASM_CONF_DIR"
	// Files in ConfDir.
	WorktreeConfig   = "worktree.json"      // the Worktree hooks' config (worktree.Config)
	MainConfig       = "main"               // Main's path, for stack.sh
	MainComposeFiles = "main-compose-files" // Main's Compose files, one per line, for stack.sh
)

// Conf is where a command in the Sandbox reads the ConfDir file name.
func Conf(name string) string {
	dir := ConfDir
	if d := os.Getenv(ConfDirEnv); d != "" {
		dir = d
	}
	return filepath.Join(dir, name)
}
