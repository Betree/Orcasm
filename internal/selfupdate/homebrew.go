package selfupdate

import (
	"path/filepath"
	"strings"
)

// Homebrew reports whether resolved (a path with symlinks already evaluated)
// is a binary Homebrew owns. Brew links `bin/<name>` into a versioned
// directory under Caskroom (casks, how orcasm ships: .goreleaser.yaml
// homebrew_casks) or Cellar (formulae, how msb ships from
// superradcompany/tap). Overwriting it would leave brew's records lying, so
// such installs are upgraded through brew instead.
func Homebrew(resolved string) bool {
	for _, dir := range strings.Split(filepath.ToSlash(resolved), "/") {
		if dir == "Caskroom" || dir == "Cellar" {
			return true
		}
	}
	return false
}
