package sandbox_test

import (
	"context"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/Betree/Orcasm/internal/msb"
	"github.com/Betree/Orcasm/internal/sandbox"
)

// recorder builds test migrations that log their steps as "N guest"/"N host".
type recorder struct{ ran []string }

func (r *recorder) migration(n int, label string) sandbox.Migration {
	step := func(part string) func(context.Context, sandbox.MigrationEnv) error {
		return func(_ context.Context, env sandbox.MigrationEnv) error {
			r.ran = append(r.ran, label+" "+part+" on "+env.Sandbox.Name)
			return nil
		}
	}
	return sandbox.Migration{N: n, Name: "test " + label, Guest: step("guest"), Host: step("host")}
}

func TestMigrateRunsEachPendingMigrationInOrderAndRecordsItAsApplied(t *testing.T) {
	r := &recorder{}
	defer sandbox.SetMigrations([]sandbox.Migration{r.migration(1, "1"), r.migration(2, "2"), r.migration(3, "3")})()
	f := newFakeMSB()
	f.add("demo", msb.Running, "orcasm=1", "orcasm.schema=1")
	m := sandbox.Manager{MSB: f, Labels: f}

	if err := m.Migrate(context.Background(), "demo", io.Discard); err != nil {
		t.Fatal(err)
	}
	wantRan := []string{"2 guest on demo", "2 host on demo", "3 guest on demo", "3 host on demo"}
	if !reflect.DeepEqual(r.ran, wantRan) {
		t.Errorf("ran %q, want %q", r.ran, wantRan)
	}
	wantWrites := []string{"demo orcasm.schema=2", "demo orcasm.schema=3"}
	if !reflect.DeepEqual(f.labelWrites, wantWrites) {
		t.Errorf("label writes %q, want %q", f.labelWrites, wantWrites)
	}
}

func TestAFailedMigrationIsRunAgainNextTimeAndTheOnesBeforeItAreNot(t *testing.T) {
	r := &recorder{}
	broken := r.migration(2, "2")
	fail := true
	host := broken.Host
	broken.Host = func(ctx context.Context, env sandbox.MigrationEnv) error {
		if fail {
			return errors.New("network down")
		}
		return host(ctx, env)
	}
	defer sandbox.SetMigrations([]sandbox.Migration{r.migration(1, "1"), broken, r.migration(3, "3")})()
	f := newFakeMSB()
	f.add("demo", msb.Running, "orcasm=1", "orcasm.schema=0")
	m := sandbox.Manager{MSB: f, Labels: f}

	err := m.Migrate(context.Background(), "demo", io.Discard)
	if err == nil || !strings.Contains(err.Error(), "network down") || !strings.Contains(err.Error(), "demo") {
		t.Fatalf("err = %v, want the failure, naming the Sandbox", err)
	}
	if got := f.sandboxes["demo"].labels["orcasm.schema"]; got != "1" {
		t.Errorf("after the failure orcasm.schema = %q, want 1", got)
	}

	fail, r.ran = false, nil
	if err := m.Migrate(context.Background(), "demo", io.Discard); err != nil {
		t.Fatal(err)
	}
	want := []string{"2 guest on demo", "2 host on demo", "3 guest on demo", "3 host on demo"}
	if !reflect.DeepEqual(r.ran, want) {
		t.Errorf("rerun ran %q, want %q", r.ran, want)
	}
	if got := f.sandboxes["demo"].labels["orcasm.schema"]; got != "3" {
		t.Errorf("orcasm.schema = %q, want 3", got)
	}
}

func TestASandboxWithoutASchemaLabelHasHadNoMigration(t *testing.T) {
	r := &recorder{}
	defer sandbox.SetMigrations([]sandbox.Migration{r.migration(1, "1")})()
	f := newFakeMSB()
	f.add("demo", msb.Running, "orcasm=1")
	m := sandbox.Manager{MSB: f, Labels: f}

	if err := m.Migrate(context.Background(), "demo", io.Discard); err != nil {
		t.Fatal(err)
	}
	if want := []string{"1 guest on demo", "1 host on demo"}; !reflect.DeepEqual(r.ran, want) {
		t.Errorf("ran %q, want %q", r.ran, want)
	}
}

func TestMigrateRefusesASchemaLabelItCannotRead(t *testing.T) {
	r := &recorder{}
	defer sandbox.SetMigrations([]sandbox.Migration{r.migration(1, "1")})()
	f := newFakeMSB()
	f.add("demo", msb.Running, "orcasm=1", "orcasm.schema=one")
	m := sandbox.Manager{MSB: f, Labels: f}

	err := m.Migrate(context.Background(), "demo", io.Discard)
	if err == nil || !strings.Contains(err.Error(), `"one"`) {
		t.Fatalf("err = %v, want it to quote the bad label", err)
	}
	if len(r.ran) != 0 || len(f.labelWrites) != 0 {
		t.Errorf("ran %q and wrote %q, want nothing", r.ran, f.labelWrites)
	}
}

func TestMigrateRefusesASandboxAheadOfThisOrcasm(t *testing.T) {
	r := &recorder{}
	defer sandbox.SetMigrations([]sandbox.Migration{r.migration(1, "1")})()
	f := newFakeMSB()
	f.add("demo", msb.Running, "orcasm=1", "orcasm.schema=4")
	m := sandbox.Manager{MSB: f, Labels: f}

	err := m.Migrate(context.Background(), "demo", io.Discard)
	if err == nil || !strings.Contains(err.Error(), "orcasm self-update") {
		t.Fatalf("err = %v, want a hint to update orcasm", err)
	}
	if len(f.labelWrites) != 0 {
		t.Errorf("wrote %q, want nothing", f.labelWrites)
	}
}

func TestMigrateLeavesAnUpToDateSandboxAlone(t *testing.T) {
	r := &recorder{}
	defer sandbox.SetMigrations([]sandbox.Migration{r.migration(1, "1"), r.migration(2, "2")})()
	f := newFakeMSB()
	f.add("demo", msb.Running, "orcasm=1", "orcasm.schema=2")
	m := sandbox.Manager{MSB: f, Labels: f}

	if err := m.Migrate(context.Background(), "demo", io.Discard); err != nil {
		t.Fatal(err)
	}
	if len(r.ran) != 0 || len(f.labelWrites) != 0 {
		t.Errorf("ran %q and wrote %q, want nothing", r.ran, f.labelWrites)
	}
}

func TestMigrateNeverTouchesASandboxOrcasmDidNotMake(t *testing.T) {
	r := &recorder{}
	defer sandbox.SetMigrations([]sandbox.Migration{r.migration(1, "1")})()
	f := newFakeMSB()
	f.add("theirs", msb.Running, "orcasm.schema=0")
	m := sandbox.Manager{MSB: f, Labels: f}

	if err := m.Migrate(context.Background(), "theirs", io.Discard); err == nil {
		t.Fatal("migrated a sandbox orcasm did not make")
	}
	if len(r.ran) != 0 || len(f.labelWrites) != 0 {
		t.Errorf("ran %q and wrote %q, want nothing", r.ran, f.labelWrites)
	}
}

func TestUpgradeStartsAStoppedSandboxSoMigrationsRunWithItsServices(t *testing.T) {
	f := newFakeMSB()
	f.add("demo", msb.Stopped, "orcasm=1", "orcasm.schema=0")
	var seen []msb.Status
	defer sandbox.SetMigrations([]sandbox.Migration{{N: 1, Name: "look", Guest: func(context.Context, sandbox.MigrationEnv) error {
		seen = append(seen, f.sandboxes["demo"].status)
		return nil
	}}})()
	secrets := []msb.Secret{{Name: "TOKEN", Value: "s3cret"}}
	m := sandbox.Manager{MSB: f, Labels: f, StartSecrets: func(context.Context, string) ([]msb.Secret, error) { return secrets, nil }}

	var out strings.Builder
	if err := m.Upgrade(context.Background(), "demo", &out); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(seen, []msb.Status{msb.Running}) {
		t.Errorf("migration saw the Sandbox %q, want it running", seen)
	}
	if !reflect.DeepEqual(f.sandboxes["demo"].startSecrets, secrets) {
		t.Errorf("started with secrets %v, want %v", f.sandboxes["demo"].startSecrets, secrets)
	}
	if !strings.Contains(out.String(), "Starting demo") {
		t.Errorf("output %q does not say it starts the Sandbox", out.String())
	}
}

func TestRegisteredMigrationsAreNumberedFromOneWithoutGaps(t *testing.T) {
	for i, mg := range sandbox.Migrations() {
		if mg.N != i+1 {
			t.Errorf("migration #%d is numbered %d, want %d", i, mg.N, i+1)
		}
		if mg.Name == "" || (mg.Guest == nil && mg.Host == nil) {
			t.Errorf("migration %d needs a name and at least one step", mg.N)
		}
	}
}
