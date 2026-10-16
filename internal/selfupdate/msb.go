package selfupdate

import (
	"context"
	"fmt"
	"io"

	"github.com/Betree/Orcasm/internal/msb"
)

// brewMSB is the Homebrew formula microsandbox ships as
// (docs/research/microsandbox.md §1).
const brewMSB = "superradcompany/tap/microsandbox"

// MSBUpgrader brings the host msb to the version orcasm pins. Neither
// upgrade path takes a target version (`msb self update` and brew both go to
// the latest release, and install.microsandbox.dev has no version knob), so
// it upgrades and then checks it landed on the pin. It never downgrades on
// its own: `msb self downgrade` rolls back msb's local state, which is the
// user's call.
type MSBUpgrader struct {
	Want    string                 // the pinned version, msb.PinnedVersion
	Version func() (string, error) // `msb --version` output
	Path    func() (string, error) // msb's path, symlinks resolved
	Run     func(ctx context.Context, out io.Writer, name string, args ...string) error
}

// EnsurePinned upgrades msb when it is older than Want and reports whether
// msb changed. An msb it cannot bring to Want is an error carrying the fix.
func (u MSBUpgrader) EnsurePinned(ctx context.Context, out io.Writer) (changed bool, err error) {
	have, err := u.installed()
	if err != nil {
		return false, err
	}
	if have == u.Want {
		return false, nil
	}
	newer, err := Newer(have, u.Want)
	if err != nil {
		return false, err
	}
	if !newer {
		return false, u.mismatch(have)
	}
	path, err := u.Path()
	if err != nil {
		return false, err
	}
	fmt.Fprintf(out, "==> Upgrading msb %s to %s\n", have, u.Want)
	if Homebrew(path) {
		err = u.Run(ctx, out, "brew", "upgrade", brewMSB)
	} else {
		err = u.Run(ctx, out, "msb", "self", "update")
	}
	if err != nil {
		return false, fmt.Errorf("upgrade msb %s to %s: %w", have, u.Want, err)
	}
	now, err := u.installed()
	if err != nil {
		return true, err
	}
	if now != u.Want {
		return true, u.mismatch(now)
	}
	return true, nil
}

func (u MSBUpgrader) installed() (string, error) {
	out, err := u.Version()
	if err != nil {
		return "", fmt.Errorf("msb not found (%v); %s", err, msb.InstallHint)
	}
	return msb.ParseVersion(out), nil
}

func (u MSBUpgrader) mismatch(have string) error {
	return fmt.Errorf("msb %s is installed but orcasm needs msb %s; run `msb self downgrade %s` (it rolls back msb's local state) or wait for an orcasm release that pins %s", have, u.Want, u.Want, have)
}
