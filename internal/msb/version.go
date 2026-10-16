// Package msb holds what orcasm knows about the microsandbox CLI.
package msb

import (
	_ "embed"
	"strings"
)

// PinnedVersion is the msb release orcasm is built and tested against.
// microsandbox is beta software with frequent breaking releases, so orcasm
// requires this exact version (see docs/research/microsandbox.md).
const PinnedVersion = "0.7.8"

// ParseVersion reads the version out of `msb --version` output ("msb 0.7.8").
func ParseVersion(out string) string {
	return strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(out), "msb"))
}

// InstallScript installs exactly one msb release, unlike
// install.microsandbox.dev, which installs the latest. Args:
// install-msb <version> [home]. CI runs the file itself.
//
//go:embed install-msb.sh
var InstallScript string

// InstallHint tells how to install the pinned msb from scratch.
const InstallHint = "install msb " + PinnedVersion + " with: curl -fsSL https://raw.githubusercontent.com/Betree/Orcasm/main/internal/msb/install-msb.sh | sh -s " + PinnedVersion
