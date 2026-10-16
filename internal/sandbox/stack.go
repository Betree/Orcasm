package sandbox

// Main's Stack: the `stack` command Provision installs in the Sandbox runs
// the Project's Compose files for Main (Compose project "main", published
// ports unchanged), on demand only: nothing starts it automatically.

import (
	_ "embed"
	"fmt"
	"path"
	"strings"

	"github.com/Betree/Orcasm/internal/guest"
)

//go:embed stack.sh
var stackScript string

// stackInstallArgv is the root command installing the `stack` command and
// what it reads in guest.ConfDir: Main's path (guest.MainConfig) and the
// Compose files, relative to Main, one per line (guest.MainComposeFiles).
func stackInstallArgv(main string, composeFiles []string) ([]string, error) {
	lines := ""
	for _, f := range composeFiles {
		if strings.Contains(f, "\n") {
			return nil, fmt.Errorf("compose file %q: newline in path", f)
		}
		lines += f + "\n"
	}
	return installArgv("install-stack", []guestFile{
		{path: path.Join(guest.ConfDir, guest.MainConfig), content: main + "\n", mode: "0644"},
		{path: path.Join(guest.ConfDir, guest.MainComposeFiles), content: lines, mode: "0644"},
		{path: "/usr/local/bin/stack", content: stackScript, mode: "0755"},
	}, ""), nil
}
