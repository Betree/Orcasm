package sandbox

import (
	"context"
	"fmt"
	"strings"

	"github.com/Betree/Orcasm/internal/version"
)

// Pending is an upgrade waiting for a Sandbox: what `orcasm upgrade` would
// do to it, as far as the host can tell.
type Pending struct {
	Name string
	// Migrations is how many migrations it lacks.
	Migrations int
	// Orca and DesiredOrca are its recorded and desired Orca releases;
	// both empty when Orca is up to date (or unknown).
	Orca, DesiredOrca string
}

// String is the one-line notice.
func (p Pending) String() string {
	var what []string
	if p.DesiredOrca != "" {
		what = append(what, fmt.Sprintf("Orca %s → %s", p.Orca, p.DesiredOrca))
	}
	switch {
	case p.Migrations == 1:
		what = append(what, "1 migration")
	case p.Migrations > 1:
		what = append(what, fmt.Sprintf("%d migrations", p.Migrations))
	}
	return fmt.Sprintf("an upgrade is pending for %s (%s); run `orcasm upgrade %s`", p.Name, strings.Join(what, ", "), p.Name)
}

// Pending lists the Sandboxes an upgrade is pending for, with desiredOrca
// the Orca they should run (OrcaSource.Desired). It reads only labels and
// the host's Installed records: it never execs into, so never boots, a
// Sandbox. One with no Installed record is only checked for migrations.
func (m Manager) Pending(ctx context.Context, desiredOrca string) ([]Pending, error) {
	all, err := m.List(ctx)
	if err != nil {
		return nil, err
	}
	var out []Pending
	for _, s := range all {
		labels, err := m.MSB.Labels(ctx, s.Name)
		if err != nil {
			return nil, err
		}
		p := Pending{Name: s.Name}
		if applied, err := schema(labels); err == nil && applied < Latest() {
			p.Migrations = Latest() - applied
		}
		in, err := LoadInstalled(m.ConfigDir, s.Name)
		if err != nil {
			return nil, err
		}
		if in.Orca != "" && desiredOrca != "" && version.Compare(in.Orca, desiredOrca) < 0 {
			p.Orca, p.DesiredOrca = in.Orca, desiredOrca
		}
		if p.Migrations > 0 || p.DesiredOrca != "" {
			out = append(out, p)
		}
	}
	return out, nil
}
