package selfupdate

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"

	"github.com/Betree/Orcasm/internal/msb"
	"github.com/Betree/Orcasm/internal/version"
)

// brewMSB is the Homebrew formula microsandbox ships as
// (docs/research/microsandbox.md §1).
const brewMSB = "superradcompany/tap/microsandbox"

// MSBUpgrader brings the host msb to exactly the version orcasm pins.
// msb's own updaters cannot: `msb self update` and install.microsandbox.dev
// go to the latest release, past the pin. So an msb in its own layout
// (<home>/bin/msb, its libraries in <home>/lib) gets the pinned release's
// bundle laid out over it (msb.InstallScript), and a Homebrew one is
// upgraded through brew only when brew's formula is at the pin. It never
// downgrades on its own: `msb self downgrade` rolls back msb's local
// state, which is the user's call.
type MSBUpgrader struct {
	Want string // the pinned version, msb.PinnedVersion
	// InstallScript installs one msb release (msb.InstallScript).
	// Args: install-msb <version> <home>.
	InstallScript string
	Version       func() (string, error) // `msb --version` output
	Path          func() (string, error) // msb's path, symlinks resolved
	// Run runs a command with its output passed to out; Output runs one
	// and returns its stdout.
	Run    func(ctx context.Context, out io.Writer, name string, args ...string) error
	Output func(ctx context.Context, name string, args ...string) (string, error)
}

// EnsurePinned upgrades msb when it is older than Want and reports whether
// msb changed. An msb it cannot bring to Want is an error carrying the fix.
func (u MSBUpgrader) EnsurePinned(ctx context.Context, out io.Writer) (changed bool, err error) {
	have, err := u.installed()
	if err != nil {
		return false, err
	}
	if version.Compare(have, u.Want) == 0 {
		return false, nil
	}
	newer, err := Newer(have, u.Want)
	if err != nil {
		return false, err
	}
	if !newer {
		return false, fmt.Errorf("msb %s is installed but orcasm needs msb %s; run `msb self downgrade %s` (it rolls back msb's local state) or wait for an orcasm release that pins %s", have, u.Want, u.Want, have)
	}
	path, err := u.Path()
	if err != nil {
		return false, err
	}
	if Homebrew(path) {
		err = u.viaBrew(ctx, out, have)
	} else {
		err = u.overOwnLayout(ctx, out, have, path)
	}
	if err != nil {
		return false, err
	}
	now, err := u.installed()
	if err != nil {
		return true, err
	}
	if version.Compare(now, u.Want) != 0 {
		return true, fmt.Errorf("msb is %s after the upgrade, not %s; %s", now, u.Want, msb.InstallHint)
	}
	return true, nil
}

// overOwnLayout installs Want's bundle into the home msb lives in.
func (u MSBUpgrader) overOwnLayout(ctx context.Context, out io.Writer, have, path string) error {
	bin := filepath.Dir(path)
	if filepath.Base(path) != "msb" || filepath.Base(bin) != "bin" {
		return fmt.Errorf("msb %s at %s is not in msb's own layout (<home>/bin/msb), so orcasm cannot tell where its libraries go; %s", have, path, msb.InstallHint)
	}
	home := filepath.Dir(bin)
	fmt.Fprintf(out, "==> Installing msb %s over %s in %s\n", u.Want, have, home)
	if err := u.Run(ctx, out, "sh", "-c", u.InstallScript, "install-msb", u.Want, home); err != nil {
		return fmt.Errorf("install msb %s: %w", u.Want, err)
	}
	return nil
}

// viaBrew upgrades a Homebrew msb, but only to Want: brew installs its
// formula's current release, which may be past the pin.
func (u MSBUpgrader) viaBrew(ctx context.Context, out io.Writer, have string) error {
	raw, err := u.Output(ctx, "brew", "info", "--json=v2", brewMSB)
	if err != nil {
		return fmt.Errorf("brew info %s: %w", brewMSB, err)
	}
	var info struct {
		Formulae []struct {
			Versions struct {
				Stable string `json:"stable"`
			} `json:"versions"`
		} `json:"formulae"`
	}
	if err := json.Unmarshal([]byte(raw), &info); err != nil || len(info.Formulae) == 0 {
		return fmt.Errorf("brew info %s: unexpected output", brewMSB)
	}
	offered := info.Formulae[0].Versions.Stable
	if version.Compare(offered, u.Want) != 0 {
		return fmt.Errorf("msb %s came from Homebrew, whose formula is at %s, not the %s orcasm pins; `brew uninstall microsandbox`, then %s", have, offered, u.Want, msb.InstallHint)
	}
	fmt.Fprintf(out, "==> Upgrading msb %s to %s with Homebrew\n", have, u.Want)
	if err := u.Run(ctx, out, "brew", "upgrade", brewMSB); err != nil {
		return fmt.Errorf("brew upgrade %s: %w", brewMSB, err)
	}
	return nil
}

func (u MSBUpgrader) installed() (string, error) {
	out, err := u.Version()
	if err != nil {
		return "", fmt.Errorf("msb not found (%v); %s", err, msb.InstallHint)
	}
	return msb.ParseVersion(out), nil
}
