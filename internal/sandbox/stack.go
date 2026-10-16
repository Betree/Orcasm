package sandbox

// Main's Stack: the `stack` command Provision installs in the Sandbox runs
// the Project's Compose files for Main (Compose project "main", published
// ports unchanged), on demand only: nothing starts it automatically.

import (
	_ "embed"
	"fmt"
	"strings"
)

//go:embed stack.sh
var stackScript string

// stackInstaller installs the `stack` command and what it reads: Main's path
// in /etc/orcasm/main and the Compose files (relative to Main), one per line,
// in /etc/orcasm/main-compose-files. ORCASM_ROOT relocates it all (tests).
// Args: install-stack <main> <stack script> [compose file...].
const stackInstaller = `set -euo pipefail
root=${ORCASM_ROOT:-}
main=$1 script=$2
shift 2
install -d "$root/etc/orcasm" "$root/usr/local/bin"
printf '%s\n' "$main" >"$root/etc/orcasm/main"
: >"$root/etc/orcasm/main-compose-files"
for f in "$@"; do printf '%s\n' "$f" >>"$root/etc/orcasm/main-compose-files"; done
printf '%s' "$script" >"$root/usr/local/bin/stack"
chmod 0755 "$root/usr/local/bin/stack"
`

// stackInstallArgv is the root command installing `stack` for Main.
func stackInstallArgv(main string, composeFiles []string) ([]string, error) {
	for _, f := range composeFiles {
		if strings.Contains(f, "\n") {
			return nil, fmt.Errorf("compose file %q: newline in path", f)
		}
	}
	return append([]string{"bash", "-c", stackInstaller, "install-stack", main, stackScript}, composeFiles...), nil
}
