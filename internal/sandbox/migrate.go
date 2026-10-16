package sandbox

// Migrations: numbered, one-shot structural changes that bring a Sandbox made
// by an older orcasm to the shape this one expects, applied in place so
// sign-ins and in-flight work survive (ADR 0006). The last one applied is
// recorded in the Sandbox's LabelSchema label.

import (
	"context"
	"fmt"
	"io"
	"strconv"

	"github.com/Betree/Orcasm/internal/msb"
)

// Migration is one structural change. Each step must be idempotent: it is
// recorded as applied only once both steps succeed, so after a failure (or
// an interrupted upgrade) the whole migration runs again.
type Migration struct {
	// N numbers the migration: the registry holds 1, 2, 3... in order, and
	// LabelSchema holds the last N applied.
	N int
	// Name says what it changes, for progress lines.
	Name string
	// Guest changes the Sandbox itself (nil: nothing to do there). It runs
	// first, with the Sandbox up so systemd and its services are there.
	Guest func(ctx context.Context, env MigrationEnv) error
	// Host changes the host-side state pointing at the Sandbox (its kept
	// Config, SSH entry, git remote...); nil: nothing to do there.
	Host func(ctx context.Context, env MigrationEnv) error
}

// MigrationEnv is what a migration step works with.
type MigrationEnv struct {
	Sandbox Sandbox
	// MSB runs commands in, and copies files into, the Sandbox.
	MSB msb.Sandboxes
	// ConfigDir holds the Configs Sandboxes were created from (LoadConfig).
	ConfigDir string
	// Out takes progress lines.
	Out io.Writer
}

// migrations is the registry, in order. Never renumber or remove one: the
// Sandboxes out there record how far along it they are.
var migrations []Migration

// Latest is the last registered migration: the schema a Sandbox provisioned
// by this orcasm starts at, and the one Migrate brings older ones to.
func Latest() int { return len(migrations) }

// schema reads the last applied migration from a Sandbox's labels. A Sandbox
// without the label predates it and has had none.
func schema(labels map[string]string) (int, error) {
	v, ok := labels[LabelSchema]
	if !ok {
		return 0, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		return 0, fmt.Errorf("unreadable %s label %q", LabelSchema, v)
	}
	return n, nil
}

// LabelSetter changes a sandbox's labels, whether it runs or not. msb.CLI
// implements it.
type LabelSetter interface {
	SetLabels(ctx context.Context, name string, labels ...msb.Label) error
}

// Migrate applies the named Sandbox's pending migrations, in order, recording
// each in its LabelSchema label as soon as it is applied.
func (m Manager) Migrate(ctx context.Context, name string, out io.Writer) error {
	s, err := m.Get(ctx, name)
	if err != nil {
		return err
	}
	labels, err := m.MSB.Labels(ctx, name)
	if err != nil {
		return err
	}
	applied, err := schema(labels)
	if err != nil {
		return fmt.Errorf("Sandbox %s: %w", name, err)
	}
	if applied > Latest() {
		return fmt.Errorf("Sandbox %s is at schema %d but this orcasm only knows %d; run `orcasm self-update` first", name, applied, Latest())
	}
	env := MigrationEnv{Sandbox: s, MSB: m.MSB, ConfigDir: m.ConfigDir, Out: out}
	for _, mg := range migrations[applied:] {
		fmt.Fprintf(out, "==> Migration %d: %s\n", mg.N, mg.Name)
		for _, step := range []func(context.Context, MigrationEnv) error{mg.Guest, mg.Host} {
			if step == nil {
				continue
			}
			if err := step(ctx, env); err != nil {
				return fmt.Errorf("migration %d (%s) of %s: %w", mg.N, mg.Name, name, err)
			}
		}
		if err := m.Labels.SetLabels(ctx, name, msb.Label{Key: LabelSchema, Value: strconv.Itoa(mg.N)}); err != nil {
			return fmt.Errorf("record migration %d of %s: %w", mg.N, name, err)
		}
	}
	return nil
}
