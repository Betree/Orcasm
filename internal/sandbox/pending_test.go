package sandbox_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/Betree/Orcasm/internal/msb"
	"github.com/Betree/Orcasm/internal/sandbox"
)

// pendingWorld has stopped Sandboxes (the check must not boot them) at the
// schemas given, of 2 migrations, with what is recorded as installed in
// each; a zero Installed means no record.
func pendingWorld(t *testing.T, installed map[string]sandbox.Installed, schema map[string]string) (sandbox.Manager, *fakeMSB) {
	t.Helper()
	r := &recorder{}
	t.Cleanup(sandbox.SetMigrations([]sandbox.Migration{r.migration(1, "1"), r.migration(2, "2")}))
	f := newFakeMSB()
	dir := t.TempDir()
	for name, in := range installed {
		f.add(name, msb.Stopped, "orcasm=1", "orcasm.schema="+schema[name])
		if in != (sandbox.Installed{}) {
			if err := sandbox.SaveInstalled(dir, name, in); err != nil {
				t.Fatal(err)
			}
		}
	}
	return sandbox.Manager{MSB: f, ConfigDir: dir}, f
}

var desired = sandbox.Installed{Orca: "1.4.230", Orcasm: "0.3.0"}

func pendingNotice(t *testing.T, m sandbox.Manager) string {
	t.Helper()
	got, err := m.Pending(context.Background(), desired)
	if err != nil {
		t.Fatal(err)
	}
	return sandbox.Notice(got)
}

func TestPendingNamesEachSandboxWithAnOlderOrcaOrcasmOrMigrationsToApply(t *testing.T) {
	m, _ := pendingWorld(t,
		map[string]sandbox.Installed{
			"current":    desired,
			"old-orca":   {Orca: "1.4.227", Orcasm: "0.3.0"},
			"old-orcasm": {Orca: "1.4.230", Orcasm: "0.2.1"},
			"old-schema": desired,
			"ahead":      {Orca: "1.4.240", Orcasm: "0.4.0"},
		},
		map[string]string{"current": "2", "old-orca": "2", "old-orcasm": "2", "old-schema": "1", "ahead": "2"})

	got, err := m.Pending(context.Background(), desired)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, p := range got {
		names = append(names, p.Name)
	}
	if want := []string{"old-orca", "old-orcasm", "old-schema"}; !equal(names, want) {
		t.Errorf("Pending for %q, want %q", names, want)
	}
}

func TestAPendingOrcasmUpgradeAloneShowsInTheNotice(t *testing.T) {
	// After `orcasm self-update`, with no migration and no new Orca: the
	// Sandbox still runs the older orcasm (its hooks, Workbench layer).
	m, _ := pendingWorld(t, map[string]sandbox.Installed{"demo": {Orca: "1.4.230", Orcasm: "0.2.1"}}, map[string]string{"demo": "2"})

	if got, want := pendingNotice(t, m), "an upgrade is pending for demo (orcasm 0.2.1 → 0.3.0); run `orcasm upgrade demo`"; got != want {
		t.Errorf("notice %q, want %q", got, want)
	}
}

func TestTheNoticeIsOneLineForAllPendingSandboxes(t *testing.T) {
	m, _ := pendingWorld(t,
		map[string]sandbox.Installed{"api": {Orca: "1.4.227", Orcasm: "0.3.0"}, "web": desired},
		map[string]string{"api": "2", "web": "0"})

	want := "upgrades are pending for api (Orca 1.4.227 → 1.4.230), web (2 migrations); run `orcasm upgrade <name>` for each"
	if got := pendingNotice(t, m); got != want {
		t.Errorf("notice %q, want %q", got, want)
	}
}

func TestNoPendingUpgradeMeansNoNotice(t *testing.T) {
	m, _ := pendingWorld(t, map[string]sandbox.Installed{"demo": desired}, map[string]string{"demo": "2"})

	if got := pendingNotice(t, m); got != "" {
		t.Errorf("notice %q, want none", got)
	}
}

func TestADevelopmentBuildNeverCountsAsAPendingOrcasmUpgrade(t *testing.T) {
	m, _ := pendingWorld(t, map[string]sandbox.Installed{"demo": {Orca: "1.4.230", Orcasm: "0.2.1"}}, map[string]string{"demo": "2"})

	got, err := m.Pending(context.Background(), sandbox.Installed{Orca: "1.4.230", Orcasm: "dev"})
	if err != nil || len(got) != 0 {
		t.Errorf("Pending = %+v, %v; want none", got, err)
	}
}

func TestASandboxWithNoInstalledRecordIsOnlyCheckedForMigrations(t *testing.T) {
	// Made before the record existed: telling its Orca would mean booting it.
	m, _ := pendingWorld(t, map[string]sandbox.Installed{"legacy": {}}, map[string]string{"legacy": "2"})

	got, err := m.Pending(context.Background(), desired)
	if err != nil || len(got) != 0 {
		t.Errorf("Pending = %+v, %v; want none", got, err)
	}
}

func TestAnUnreadableSandboxIsSkippedNotFatal(t *testing.T) {
	m, f := pendingWorld(t,
		map[string]sandbox.Installed{"broken": {Orca: "1.4.227"}, "garbled": {Orca: "1.4.227"}, "old": {Orca: "1.4.227", Orcasm: "0.3.0"}},
		map[string]string{"broken": "2", "garbled": "2", "old": "2"})
	f.labelErr = map[string]error{"broken": os.ErrPermission}
	if err := os.WriteFile(filepath.Join(m.ConfigDir, "garbled.installed.toml"), []byte("orca = ["), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := m.Pending(context.Background(), desired)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Name != "old" {
		t.Errorf("Pending = %+v, want only old", got)
	}
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
