package sandbox_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Betree/Orcasm/internal/repository"
	"github.com/Betree/Orcasm/internal/sandbox"
	"github.com/Betree/Orcasm/internal/wizard"
)

// world records, in order, what creating a Sandbox did to the host and the
// Sandbox.
type world struct {
	steps []string
	// orcaUp is how many registration attempts fail before Orca serves.
	orcaUp     int
	copied     []repository.CopyOptions
	remoteErr  error
	configDir  string
	linkedUser bool
}

func (w *world) creator(t *testing.T) sandbox.Creator {
	w.configDir = t.TempDir()
	return sandbox.Creator{
		Provision: func(_ context.Context, cfg wizard.Config, _ io.Writer) error {
			w.steps = append(w.steps, "provision "+cfg.Name)
			return nil
		},
		InstallOrcasm: func(_ context.Context, cfg wizard.Config, _ io.Writer) error {
			w.steps = append(w.steps, "install orcasm in "+cfg.Name)
			return nil
		},
		Copy: func(_ context.Context, name string, opts repository.CopyOptions) error {
			w.steps = append(w.steps, "copy into "+name)
			w.copied = append(w.copied, opts)
			return nil
		},
		SSH: fakeAccess{w},
		AddRemote: func(_ context.Context, hostRepo, name, guestPath, sshCommand string) error {
			w.steps = append(w.steps, fmt.Sprintf("remote %s %s:%s via %s", hostRepo, name, guestPath, sshCommand))
			return w.remoteErr
		},
		RegisterInOrca: func(_ context.Context, name, guestPath string) error {
			if w.orcaUp > 0 {
				w.orcaUp--
				return errors.New("orca runtime not ready")
			}
			w.steps = append(w.steps, "register "+guestPath+" in Orca of "+name)
			return nil
		},
		SetHooks: func(_ context.Context, name, guestPath string) error {
			w.steps = append(w.steps, "set Orca hooks of "+guestPath+" in "+name)
			return nil
		},
		ConfigDir:   w.configDir,
		OrcaVersion: "1.4.230",
		OrcaWait:    time.Second,
		Poll:        time.Millisecond,
	}
}

type fakeAccess struct{ w *world }

func (f fakeAccess) Add(_ context.Context, name string) error {
	f.w.steps = append(f.w.steps, "ssh add "+name)
	return nil
}

func (f fakeAccess) LinkUserConfig() error {
	f.w.steps = append(f.w.steps, "link ~/.ssh/config")
	f.w.linkedUser = true
	return nil
}

func (f fakeAccess) GitSSHCommand() string { return "ssh -F orcasm" }

func createConfig() wizard.Config {
	cfg := demoConfig()
	cfg.Project.BringFiles = []string{".env"}
	return cfg
}

func TestCreateSetsUpTheWholeSandboxInOrder(t *testing.T) {
	w := &world{orcaUp: 2}
	if err := w.creator(t).Create(context.Background(), createConfig(), io.Discard); err != nil {
		t.Fatal(err)
	}

	want := []string{
		"provision demo",
		"install orcasm in demo",
		"copy into demo",
		"ssh add demo",
		"remote /home/u/code/demo demo:/home/dev/demo via ssh -F orcasm",
		"register /home/dev/demo in Orca of demo",
		"set Orca hooks of /home/dev/demo in demo",
	}
	if !reflect.DeepEqual(w.steps, want) {
		t.Errorf("steps\n%s\nwant\n%s", strings.Join(w.steps, "\n"), strings.Join(want, "\n"))
	}
	wantCopy := repository.CopyOptions{HostPath: "/home/u/code/demo", GuestPath: "/home/dev/demo", Owner: "dev", IgnoredFiles: []string{".env"}}
	if len(w.copied) != 1 || !reflect.DeepEqual(w.copied[0], wantCopy) {
		t.Errorf("copied %+v, want %+v", w.copied, wantCopy)
	}
	if w.linkedUser {
		t.Error("~/.ssh/config linked without consent")
	}
	if kept, err := sandbox.LoadConfig(w.configDir, "demo"); err != nil || kept.HostPort != 6769 {
		t.Errorf("want the Config kept for later starts, got %+v, %v", kept, err)
	}
}

// The daily upgrade notice compares it without booting the Sandbox.
func TestCreateRecordsTheOrcaReleaseItInstalledOnTheHost(t *testing.T) {
	w := &world{}
	if err := w.creator(t).Create(context.Background(), createConfig(), io.Discard); err != nil {
		t.Fatal(err)
	}
	got, err := sandbox.LoadInstalled(w.configDir, "demo")
	if err != nil || got.Orca != "1.4.230" {
		t.Errorf("installed = %+v, %v; want Orca 1.4.230", got, err)
	}
}

func TestCreateLinksTheUsersSSHConfigOnlyWithConsent(t *testing.T) {
	w := &world{}
	cfg := createConfig()
	cfg.LinkSSHConfig = true
	if err := w.creator(t).Create(context.Background(), cfg, io.Discard); err != nil {
		t.Fatal(err)
	}
	if !w.linkedUser {
		t.Errorf("consented, but ~/.ssh/config not linked; steps %q", w.steps)
	}
}

func TestCreateGivesUpWhenOrcaNeverServes(t *testing.T) {
	w := &world{orcaUp: 1 << 30}
	err := w.creator(t).Create(context.Background(), createConfig(), io.Discard)
	if err == nil || !strings.Contains(err.Error(), "orca runtime not ready") {
		t.Fatalf("err = %v", err)
	}
}

// A user's own core.sshCommand only costs the plain `git fetch`; the Sandbox
// is still usable.
func TestCreateWarnsWhenTheRemoteNeedsTheUsersHelp(t *testing.T) {
	w := &world{remoteErr: fmt.Errorf("%w: use git -c core.sshCommand=...", repository.ErrOwnSSHCommand)}
	var out strings.Builder
	if err := w.creator(t).Create(context.Background(), createConfig(), &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "core.sshCommand") {
		t.Errorf("want a warning, got:\n%s", out.String())
	}
	if last := w.steps[len(w.steps)-1]; !strings.HasPrefix(last, "set Orca hooks") {
		t.Errorf("steps %q", w.steps)
	}
}
