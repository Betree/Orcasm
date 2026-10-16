// Command orcasm is the orcasm CLI entrypoint.
package main

import (
	"fmt"
	"os"

	"github.com/Betree/Orcasm/internal/cli"
)

// Set by GoReleaser via -ldflags "-X main.version=...".
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	root := cli.NewRootCommand(cli.BuildInfo{Version: version, Commit: commit, Date: date})
	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "orcasm:", err)
		os.Exit(1)
	}
}
