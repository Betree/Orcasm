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
// which release Homebrew's formula would install.
type fakeHostMSB struct {
	version string // "" means msb is not installed
	path    string
	brew    string // Homebrew's microsandbox formula version
	ran     []string
}

const installScript = "#!/bin/sh\n# install-msb\n"

func (f *fakeHostMSB) upgrader() selfupdate.MSBUpgrader {
	return selfupdate.MSBUpgrader{
		Want:          "0.7.8",
		InstallScript: installScript,
		Version: func() (string, error) {
			if f.version == "" {
				return "", errors.New("executable file not found in $PATH")
			}
			return "msb " + f.version + "\n", nil
		},
		Path: func() (string, error) { return f.path, nil },
		Run: func(_ context.Context, _ io.Writer, name string, args ...string) error {
			switch {
			case name == "sh" && len(args) == 5 && args[0] == "-c" && args[1] == installScript:
				// sh -c SCRIPT install-msb VERSION HOME
				f.ran = append(f.ran, "install-msb "+args[3]+" "+args[4])
				f.version = args[3]
			case name == "brew" && strings.Join(args, " ") == "upgrade superradcompany/tap/microsandbox":
				f.ran = append(f.ran, "brew upgrade")
				f.version = f.brew
			default:
				f.ran = append(f.ran, name+" "+strings.Join(args, " "))
				return errors.New("unexpected command")
			}
			return nil
		},
		Output: func(_ context.Context, name string, args ...string) (string, error) {
			if name != "brew" || strings.Join(args, " ") != "info --json=v2 superradcompany/tap/microsandbox" {
				return "", errors.New("unexpected command")
			}
			return `{"formulae": [{"name": "microsandbox", "versions": {"stable": "` + f.brew + `"}}], "casks": []}`, nil
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

func TestEnsurePinnedInstallsExactlyThePinnedReleaseOverAnOlderMSB(t *testing.T) {
	// `msb self update` would go to the latest release, past the pin.
	f := &fakeHostMSB{version: "0.7.5", path: "/home/dev/.microsandbox/bin/msb"}

	changed, err := f.upgrader().EnsurePinned(context.Background(), io.Discard)
	if err != nil || !changed {
		t.Fatalf("changed=%v err=%v, want a change and no error", changed, err)
	}
	if want := []string{"install-msb 0.7.8 /home/dev/.microsandbox"}; strings.Join(f.ran, "\n") != strings.Join(want, "\n") {
		t.Fatalf("ran %q, want %q", f.ran, want)
	}
}

func TestEnsurePinnedUpgradesABrewMSBWhenBrewOffersThePinnedRelease(t *testing.T) {
	f := &fakeHostMSB{version: "0.7.5", brew: "0.7.8", path: "/home/linuxbrew/.linuxbrew/Cellar/microsandbox/0.7.5/bin/msb"}

	changed, err := f.upgrader().EnsurePinned(context.Background(), io.Discard)
	if err != nil || !changed {
		t.Fatalf("changed=%v err=%v, want a change and no error", changed, err)
	}
	if strings.Join(f.ran, ",") != "brew upgrade" {
		t.Fatalf("ran %v, want only brew", f.ran)
	}
}

func TestEnsurePinnedNeverLetsBrewOvershootThePin(t *testing.T) {
	// Brew only installs its formula's current release.
	f := &fakeHostMSB{version: "0.7.5", brew: "0.8.0", path: "/home/linuxbrew/.linuxbrew/Cellar/microsandbox/0.7.5/bin/msb"}

	changed, err := f.upgrader().EnsurePinned(context.Background(), io.Discard)
	if changed || len(f.ran) != 0 {
		t.Fatalf("changed=%v ran=%v, want nothing done", changed, f.ran)
	}
	if err == nil || !strings.Contains(err.Error(), "0.8.0") || !strings.Contains(err.Error(), "install-msb.sh") {
		t.Fatalf("want an error naming brew's release and the pinned installer, got %v", err)
	}
}

func TestEnsurePinnedLeavesAnMSBItDoesNotRecogniseAlone(t *testing.T) {
	// Not msb's own layout (<home>/bin/msb with <home>/lib): its libraries
	// could be anywhere.
	f := &fakeHostMSB{version: "0.7.5", path: "/opt/tools/msb"}

	changed, err := f.upgrader().EnsurePinned(context.Background(), io.Discard)
	if changed || len(f.ran) != 0 {
		t.Fatalf("changed=%v ran=%v, want nothing done", changed, f.ran)
	}
	if err == nil || !strings.Contains(err.Error(), "/opt/tools/msb") {
		t.Fatalf("want an error naming the msb, got %v", err)
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
	if err == nil || !strings.Contains(err.Error(), "install-msb.sh") {
		t.Fatalf("want an install hint, got %v", err)
	}
}
