package selfupdate_test

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/Betree/Orcasm/internal/selfupdate"
)

// fakeHostMSB is a host msb install: its version, where it resolves to, and
// what upgrading it through each tool would leave installed.
type fakeHostMSB struct {
	version string // "" means msb is not installed
	path    string
	after   map[string]string // command line → version installed afterwards
	ran     []string
}

func (f *fakeHostMSB) upgrader() selfupdate.MSBUpgrader {
	return selfupdate.MSBUpgrader{
		Want: "0.7.8",
		Version: func() (string, error) {
			if f.version == "" {
				return "", errors.New("executable file not found in $PATH")
			}
			return "msb " + f.version + "\n", nil
		},
		Path: func() (string, error) { return f.path, nil },
		Run: func(_ context.Context, _ io.Writer, name string, args ...string) error {
			line := strings.Join(append([]string{name}, args...), " ")
			f.ran = append(f.ran, line)
			v, ok := f.after[line]
			if !ok {
				return errors.New("unexpected command " + line)
			}
			f.version = v
			return nil
		},
	}
}

func TestEnsurePinnedLeavesThePinnedMSBAlone(t *testing.T) {
	f := &fakeHostMSB{version: "0.7.8", path: "/home/dev/.microsandbox/bin/msb"}

	changed, err := f.upgrader().EnsurePinned(context.Background(), io.Discard)
	if err != nil || changed {
		t.Fatalf("changed=%v err=%v, want neither", changed, err)
	}
	if len(f.ran) != 0 {
		t.Fatalf("ran %v, want nothing", f.ran)
	}
}

func TestEnsurePinnedUpgradesAnOlderMSBWithItsOwnUpdater(t *testing.T) {
	f := &fakeHostMSB{version: "0.7.5", path: "/home/dev/.microsandbox/bin/msb",
		after: map[string]string{"msb self update": "0.7.8"}}

	changed, err := f.upgrader().EnsurePinned(context.Background(), io.Discard)
	if err != nil || !changed {
		t.Fatalf("changed=%v err=%v, want a change and no error", changed, err)
	}
}

func TestEnsurePinnedUpgradesABrewMSBThroughBrew(t *testing.T) {
	f := &fakeHostMSB{version: "0.7.5", path: "/home/linuxbrew/.linuxbrew/Cellar/microsandbox/0.7.5/bin/msb",
		after: map[string]string{"brew upgrade superradcompany/tap/microsandbox": "0.7.8"}}

	changed, err := f.upgrader().EnsurePinned(context.Background(), io.Discard)
	if err != nil || !changed {
		t.Fatalf("changed=%v err=%v, want a change and no error", changed, err)
	}
	if len(f.ran) != 1 {
		t.Fatalf("ran %v, want only brew", f.ran)
	}
}

func TestEnsurePinnedReportsAnUpgradeThatOvershootsThePin(t *testing.T) {
	f := &fakeHostMSB{version: "0.7.5", path: "/home/dev/.microsandbox/bin/msb",
		after: map[string]string{"msb self update": "0.8.0"}}

	changed, err := f.upgrader().EnsurePinned(context.Background(), io.Discard)
	if !changed {
		t.Fatal("msb did change")
	}
	if err == nil || !strings.Contains(err.Error(), "msb self downgrade 0.7.8") {
		t.Fatalf("want an error hinting at the downgrade, got %v", err)
	}
}

func TestEnsurePinnedNeverDowngradesOnItsOwn(t *testing.T) {
	f := &fakeHostMSB{version: "0.8.0", path: "/home/dev/.microsandbox/bin/msb"}

	changed, err := f.upgrader().EnsurePinned(context.Background(), io.Discard)
	if changed || len(f.ran) != 0 {
		t.Fatalf("changed=%v ran=%v, want nothing done", changed, f.ran)
	}
	if err == nil || !strings.Contains(err.Error(), "msb self downgrade 0.7.8") {
		t.Fatalf("want an error hinting at the downgrade, got %v", err)
	}
}

func TestEnsurePinnedExplainsHowToInstallAMissingMSB(t *testing.T) {
	f := &fakeHostMSB{}

	_, err := f.upgrader().EnsurePinned(context.Background(), io.Discard)
	if err == nil || !strings.Contains(err.Error(), "install.microsandbox.dev") {
		t.Fatalf("want an install hint, got %v", err)
	}
}
