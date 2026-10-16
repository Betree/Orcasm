// Package msb holds what orcasm knows about the microsandbox CLI.
package msb

import "strings"

// PinnedVersion is the msb release orcasm is built and tested against.
// microsandbox is beta software with frequent breaking releases, so orcasm
// requires this exact version (see docs/research/microsandbox.md).
const PinnedVersion = "0.7.8"

// ParseVersion reads the version out of `msb --version` output ("msb 0.7.8").
func ParseVersion(out string) string {
	return strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(out), "msb"))
}

// InstallHint tells how to install the pinned msb from scratch.
const InstallHint = "install msb " + PinnedVersion + " with: curl -fsSL https://install.microsandbox.dev | sh"
