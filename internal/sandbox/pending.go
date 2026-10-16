package sandbox

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/Betree/Orcasm/internal/msb"
	"github.com/Betree/Orcasm/internal/version"
)

// Pending is an upgrade waiting for a Sandbox: what `orcasm upgrade` would
// do to it, as far as the host can tell.
type Pending struct {
	Name string
	// Migrations is how many migrations it lacks.
	Migrations int
	// Installed is what is recorded as installed in it, and Desired what
	// it should run; only the parts that are behind are set in Desired.
	Installed, Desired Installed
}

// what lists the pending parts, e.g. "Orca 1.4.227 → 1.4.230".
func (p Pending) what() string {
	var what []string
	if p.Desired.Orca != "" {
		what = append(what, fmt.Sprintf("Orca %s → %s", p.Installed.Orca, p.Desired.Orca))
	}
	if p.Desired.Orcasm != "" {
		what = append(what, fmt.Sprintf("orcasm %s → %s", p.Installed.Orcasm, p.Desired.Orcasm))
	}
	switch {
	case p.Migrations == 1:
		what = append(what, "1 migration")
	case p.Migrations > 1:
		what = append(what, fmt.Sprintf("%d migrations", p.Migrations))
	}
	return strings.Join(what, ", ")
}

// Notice is the one-line upgrade notice for all of pending ("" for none).
func Notice(pending []Pending) string {
	switch len(pending) {
	case 0:
		return ""
	case 1:
		p := pending[0]
		return fmt.Sprintf("an upgrade is pending for %s (%s); run `orcasm upgrade %s`", p.Name, p.what(), p.Name)
	}
	var each []string
	for _, p := range pending {
		each = append(each, fmt.Sprintf("%s (%s)", p.Name, p.what()))
	}
	return fmt.Sprintf("upgrades are pending for %s; run `orcasm upgrade <name>` for each", strings.Join(each, ", "))
}

// Pending lists the Sandboxes an upgrade is pending for, with desired what
// they should run: the Orca from OrcaSource.Desired and this orcasm's
// version. It reads only labels and the host's Installed records: it never
// execs into, so never boots, a Sandbox. A part with nothing recorded (a
// Sandbox made before the record held it), or a desired orcasm that is a
// development build, is not compared. A Sandbox whose labels or record
// cannot be read is skipped: the notice is a hint, and `orcasm upgrade`
// reports the error.
func (m Manager) Pending(ctx context.Context, desired Installed) ([]Pending, error) {
	// Listed by msb, not List: List fails on any one unreadable Sandbox.
	all, err := m.MSB.List(ctx, orcasmLabel)
	if err != nil {
		return nil, err
	}
	slices.SortFunc(all, func(a, b msb.Listing) int { return strings.Compare(a.Name, b.Name) })
	var out []Pending
	for _, s := range all {
		labels, err := m.MSB.Labels(ctx, s.Name)
		if err != nil {
			continue
		}
		in, err := LoadInstalled(m.ConfigDir, s.Name)
		if err != nil {
			continue
		}
		p := Pending{Name: s.Name, Installed: in}
		if applied, err := schema(labels); err == nil && applied < Latest() {
			p.Migrations = Latest() - applied
		}
		if behind(in.Orca, desired.Orca) {
			p.Desired.Orca = desired.Orca
		}
		if behind(in.Orcasm, desired.Orcasm) {
			p.Desired.Orcasm = desired.Orcasm
		}
		if p.Migrations > 0 || p.Desired != (Installed{}) {
			out = append(out, p)
		}
	}
	return out, nil
}

// behind reports whether installed is a release older than release desired.
func behind(installed, desired string) bool {
	return version.IsRelease(installed) && version.IsRelease(desired) && version.Compare(installed, desired) < 0
}
