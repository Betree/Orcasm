// Package cli wires orcasm's command tree. Each subcommand lives in its own
// file and registers itself with register() from an init function, so adding
// a command never touches root.go.
package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

// BuildInfo is stamped into the binary at release time by GoReleaser ldflags.
type BuildInfo struct {
	Version string
	Commit  string
	Date    string
}

var subcommands []func(BuildInfo) *cobra.Command

// build is the BuildInfo of the last root command built, for code running
// outside a command's constructor (create).
var build BuildInfo

// bareRun is what `orcasm` without a subcommand does (set in bare.go).
var bareRun func(*cobra.Command, []string) error

// register adds a subcommand constructor. Call it from an init function in
// the subcommand's own file.
func register(newCmd func(BuildInfo) *cobra.Command) {
	subcommands = append(subcommands, newCmd)
}

// NewRootCommand builds a fresh `orcasm` command tree.
func NewRootCommand(info BuildInfo) *cobra.Command {
	build = info
	root := &cobra.Command{
		Use:           "orcasm",
		Short:         "Give a Project a Sandbox where many coding agents work in parallel",
		Version:       info.Version,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.SetVersionTemplate(fmt.Sprintf("orcasm %s (commit %s, built %s)\n", info.Version, info.Commit, info.Date))
	root.RunE = bareRun
	root.PersistentPostRun = upgradeNotice
	for _, newCmd := range subcommands {
		root.AddCommand(newCmd(info))
	}
	return root
}
