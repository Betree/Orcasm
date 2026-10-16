package sandbox_test

import (
	"context"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/Betree/Orcasm/internal/msb"
	"github.com/Betree/Orcasm/internal/sandbox"
)

// upgrading is a running Sandbox demo, made from demoConfig, with Orca
// installedOrca and agents agent processes running.
type upgrading struct {
	f      *fakeMSB
	m      sandbox.Manager
	asked  []string
	answer bool
	out    strings.Builder
}

func newUpgrading(t *testing.T, installedOrca string, agents int) *upgrading {
	t.Helper()
	u := &upgrading{f: newFakeMSB()}
	sb := u.f.add("demo", msb.Running, "orcasm=1", "orcasm.main=/home/dev/demo", "orcasm.port=6769", "orcasm.schema=0")
	sb.orca, sb.agents = installedOrca, agents
	dir := t.TempDir()
	if err := sandbox.SaveConfig(dir, demoConfig()); err != nil {
		t.Fatal(err)
	}
	if err := sandbox.SaveInstalled(dir, "demo", sandbox.Installed{Orca: installedOrca}); err != nil {
		t.Fatal(err)
	}
	u.m = sandbox.Manager{MSB: u.f, Labels: u.f, Installer: u.f, ConfigDir: dir}
	return u
}

func (u *upgrading) upgrade(t *testing.T, desired string) sandbox.UpgradeResult {
	t.Helper()
	res, err := u.m.Upgrade(context.Background(), "demo", sandbox.UpgradeOptions{
		Orca:        desired,
		GuestBinary: "/host/orcasm",
		Confirm: func(q string) bool {
			u.asked = append(u.asked, q)
			return u.answer
		},
	}, &u.out)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func (u *upgrading) recordedOrca(t *testing.T) string {
	t.Helper()
	in, err := sandbox.LoadInstalled(u.m.ConfigDir, "demo")
	if err != nil {
		t.Fatal(err)
	}
	return in.Orca
}

func TestUpgradeInstallsTheDesiredOrcaAndRestartsIt(t *testing.T) {
	u := newUpgrading(t, "1.4.227", 0)

	res := u.upgrade(t, "1.4.230")

	c, ok := u.f.script("upgrade-orca")
	if !ok {
		t.Fatalf("Orca not upgraded; scripts %q", u.f.scripts())
	}
	if got, want := c.argv[4:], []string{"dev", "6769", "1.4.230"}; !reflect.DeepEqual(got, want) {
		t.Errorf("upgrade-orca args %q, want %q", got, want)
	}
	if !strings.Contains(c.argv[2], "install_orca") || !strings.Contains(c.argv[2], "start_orca") {
		t.Error("upgrade-orca does not install and restart Orca")
	}
	if !res.OrcaRestarted {
		t.Error("result does not say Orca restarted (its URL changed)")
	}
	if len(u.asked) != 0 {
		t.Errorf("asked %q with no agent running", u.asked)
	}
	if got := u.recordedOrca(t); got != "1.4.230" {
		t.Errorf("recorded Orca %q, want 1.4.230", got)
	}
	if !strings.Contains(u.out.String(), "1.4.227") || !strings.Contains(u.out.String(), "1.4.230") {
		t.Errorf("output %q does not show installed and desired Orca", u.out.String())
	}
}

func TestAnUpToDateOrcaIsLeftRunning(t *testing.T) {
	u := newUpgrading(t, "1.4.230", 1)

	res := u.upgrade(t, "1.4.230")

	if _, ok := u.f.script("upgrade-orca"); ok || res.OrcaRestarted {
		t.Error("restarted an up to date Orca")
	}
	if len(u.asked) != 0 {
		t.Errorf("asked %q", u.asked)
	}
}

func TestUpgradeNeverDowngradesOrca(t *testing.T) {
	// E.g. installed by `orcasm upgrade --latest` before the compatibility
	// file caught up.
	u := newUpgrading(t, "1.4.240", 0)

	u.upgrade(t, "1.4.230")

	if _, ok := u.f.script("upgrade-orca"); ok {
		t.Error("downgraded Orca")
	}
	if got := u.recordedOrca(t); got != "1.4.240" {
		t.Errorf("recorded Orca %q, want 1.4.240", got)
	}
}

func TestUpgradeAsksBeforeRestartingOrcaUnderRunningAgents(t *testing.T) {
	u := newUpgrading(t, "1.4.227", 2)

	res := u.upgrade(t, "1.4.230")

	if len(u.asked) != 1 || !strings.Contains(u.asked[0], "agent") {
		t.Fatalf("asked %q, want one question about the running agents", u.asked)
	}
	if _, ok := u.f.script("upgrade-orca"); ok || res.OrcaRestarted {
		t.Error("restarted Orca without consent")
	}
	if got := u.recordedOrca(t); got != "1.4.227" {
		t.Errorf("recorded Orca %q, want 1.4.227 still", got)
	}
	if !strings.Contains(u.out.String(), "orcasm upgrade demo") {
		t.Errorf("output %q does not say how to finish later", u.out.String())
	}

	u.answer = true
	if res := u.upgrade(t, "1.4.230"); !res.OrcaRestarted {
		t.Error("did not restart Orca with consent")
	}
}

func TestUpgradeRefreshesTheHarnessesAndOrcasmInTheSandbox(t *testing.T) {
	u := newUpgrading(t, "1.4.230", 0)

	u.upgrade(t, "1.4.230")

	c, ok := u.f.script("upgrade-harnesses")
	if !ok {
		t.Fatalf("harnesses not upgraded; scripts %q", u.f.scripts())
	}
	if got, want := c.argv[4:], []string{"claude", "codex"}; !reflect.DeepEqual(got, want) {
		t.Errorf("harnesses %q, want %q", got, want)
	}
	for _, s := range []string{"install-agents", "install-orcasm", "install-stack", "start-proxy"} {
		if !slices.Contains(u.f.scripts(), s) {
			t.Errorf("did not rerun %s; scripts %q", s, u.f.scripts())
		}
	}
	if want := "demo /host/orcasm -> /tmp/orcasm.new"; !slices.Contains(u.f.copied, want) {
		t.Errorf("copied %q, want %q", u.f.copied, want)
	}
}

func TestHarnessesAreLeftAloneWhileAgentsRun(t *testing.T) {
	// Replacing a harness's files under a running agent can crash it.
	u := newUpgrading(t, "1.4.230", 1)

	u.upgrade(t, "1.4.230")

	if _, ok := u.f.script("upgrade-harnesses"); ok {
		t.Error("upgraded the harnesses under running agents")
	}
	if !strings.Contains(u.out.String(), "agent") {
		t.Errorf("output %q does not say why the harnesses were skipped", u.out.String())
	}
}
