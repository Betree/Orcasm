package sandbox_test

import (
	"context"
	"testing"

	"github.com/Betree/Orcasm/internal/msb"
	"github.com/Betree/Orcasm/internal/sandbox"
)

// pendingWorld has Sandboxes at schema 1 of 2 migrations, with Orca
// recorded as installed per name.
func pendingWorld(t *testing.T, orca map[string]string, schema map[string]string) sandbox.Manager {
	t.Helper()
	r := &recorder{}
	t.Cleanup(sandbox.SetMigrations([]sandbox.Migration{r.migration(1, "1"), r.migration(2, "2")}))
	f := newFakeMSB()
	dir := t.TempDir()
	for name, v := range orca {
		// Stopped: the check must not boot them.
		f.add(name, msb.Stopped, "orcasm=1", "orcasm.schema="+schema[name])
		if v != "" {
			if err := sandbox.SaveInstalled(dir, name, sandbox.Installed{Orca: v}); err != nil {
				t.Fatal(err)
			}
		}
	}
	return sandbox.Manager{MSB: f, ConfigDir: dir}
}

func TestPendingNamesEachSandboxWithAnOlderOrcaOrMigrationsToApply(t *testing.T) {
	m := pendingWorld(t,
		map[string]string{"current": "1.4.230", "old-orca": "1.4.227", "old-schema": "1.4.230", "ahead": "1.4.240"},
		map[string]string{"current": "2", "old-orca": "2", "old-schema": "1", "ahead": "2"})

	got, err := m.Pending(context.Background(), "1.4.230")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"an upgrade is pending for old-orca (Orca 1.4.227 → 1.4.230); run `orcasm upgrade old-orca`",
		"an upgrade is pending for old-schema (1 migration); run `orcasm upgrade old-schema`",
	}
	if len(got) != len(want) {
		t.Fatalf("Pending = %q, want %q", got, want)
	}
	for i := range want {
		if got[i].String() != want[i] {
			t.Errorf("Pending[%d] = %q, want %q", i, got[i].String(), want[i])
		}
	}
}

func TestASandboxWithNoInstalledRecordIsOnlyCheckedForMigrations(t *testing.T) {
	// Made before the record existed: telling its Orca would mean booting it.
	m := pendingWorld(t, map[string]string{"legacy": ""}, map[string]string{"legacy": "2"})

	got, err := m.Pending(context.Background(), "1.4.230")
	if err != nil || len(got) != 0 {
		t.Errorf("Pending = %q, %v; want none", got, err)
	}
}
