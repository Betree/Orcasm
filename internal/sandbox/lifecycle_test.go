package sandbox_test

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"slices"
	"strings"
	"testing"

	"github.com/Betree/Orcasm/internal/msb"
	"github.com/Betree/Orcasm/internal/sandbox"
)

// fakeSandbox is one sandbox in fakeMSB.
type fakeSandbox struct {
	status string
	labels map[string]string
	// orcaLog is what the current run of the Orca service has logged, as
	// successive reads of the log see it (the last entry repeats).
	orcaLog []string
	// startEnv is the extra environment msb start last ran with.
	startEnv []string
}

// fakeMSB is an in-memory msb. Exec only knows how to read Orca's service log.
type fakeMSB struct {
	sandboxes map[string]*fakeSandbox
	removed   []string
}

func newFakeMSB() *fakeMSB { return &fakeMSB{sandboxes: map[string]*fakeSandbox{}} }

func (f *fakeMSB) add(name, status string, labels ...string) *fakeSandbox {
	sb := &fakeSandbox{status: status, labels: map[string]string{}}
	for _, l := range labels {
		k, v, _ := strings.Cut(l, "=")
		sb.labels[k] = v
	}
	f.sandboxes[name] = sb
	return sb
}

func (f *fakeMSB) get(name string) (*fakeSandbox, error) {
	sb, ok := f.sandboxes[name]
	if !ok {
		return nil, fmt.Errorf("sandbox not found: %s", name)
	}
	return sb, nil
}

func (f *fakeMSB) List(_ context.Context, labels ...string) ([]msb.Listing, error) {
	var out []msb.Listing
	for name, sb := range f.sandboxes {
		match := true
		for _, l := range labels {
			k, v, _ := strings.Cut(l, "=")
			if got, ok := sb.labels[k]; !ok || got != v {
				match = false
			}
		}
		if match {
			out = append(out, msb.Listing{Name: name, Status: sb.status})
		}
	}
	slices.SortFunc(out, func(a, b msb.Listing) int { return strings.Compare(a.Name, b.Name) })
	return out, nil
}

func (f *fakeMSB) Labels(_ context.Context, name string) (map[string]string, error) {
	sb, err := f.get(name)
	if err != nil {
		return nil, err
	}
	return sb.labels, nil
}

func (f *fakeMSB) Start(_ context.Context, name string, env []string) error {
	sb, err := f.get(name)
	if err != nil {
		return err
	}
	if sb.status == msb.Running {
		return fmt.Errorf("cannot start sandbox '%s': already running", name)
	}
	sb.status = msb.Running
	sb.startEnv = env
	return nil
}

func (f *fakeMSB) Stop(_ context.Context, name string, _ bool) error {
	sb, err := f.get(name)
	if err != nil {
		return err
	}
	sb.status = msb.Stopped
	return nil
}

func (f *fakeMSB) Remove(_ context.Context, name string) error {
	if _, err := f.get(name); err != nil {
		return err
	}
	delete(f.sandboxes, name)
	f.removed = append(f.removed, name)
	return nil
}

func (f *fakeMSB) Exec(_ context.Context, name, _ string, argv ...string) (string, error) {
	sb, err := f.get(name)
	if err != nil {
		return "", err
	}
	if argv[0] == "git" {
		// The guest's paths are local paths: Main is a local clone.
		out, err := exec.Command(argv[0], argv[1:]...).Output()
		return string(out), err
	}
	if !strings.Contains(strings.Join(argv, " "), "journalctl") {
		return "", fmt.Errorf("fake msb cannot run %q", argv)
	}
	if sb.status != msb.Running || len(sb.orcaLog) == 0 {
		return "", nil
	}
	log := sb.orcaLog[0]
	if len(sb.orcaLog) > 1 {
		sb.orcaLog = sb.orcaLog[1:]
	}
	return log, nil
}

func names(sbs []sandbox.Sandbox) []string {
	var out []string
	for _, s := range sbs {
		out = append(out, s.Name)
	}
	return out
}

func TestListShowsOnlyOrcasmSandboxes(t *testing.T) {
	f := newFakeMSB()
	f.add("app", msb.Running, "orcasm=1", "orcasm.folder=/home/me/app")
	f.add("app-2", msb.Stopped, "orcasm=1", "orcasm.folder=/home/me/app")
	f.add("oc-dev", msb.Running, "team=platform")

	got, err := sandbox.Manager{MSB: f}.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	want := []sandbox.Sandbox{
		{Name: "app", Status: msb.Running, Folder: "/home/me/app"},
		{Name: "app-2", Status: msb.Stopped, Folder: "/home/me/app"},
	}
	if !slices.Equal(got, want) {
		t.Errorf("List = %+v, want %+v", got, want)
	}
}

func TestInFolderFindsTheFoldersSandboxes(t *testing.T) {
	f := newFakeMSB()
	f.add("app", msb.Running, "orcasm=1", "orcasm.folder=/home/me/app")
	f.add("other", msb.Running, "orcasm=1", "orcasm.folder=/home/me/other")

	got, err := sandbox.Manager{MSB: f}.InFolder(context.Background(), "/home/me/app")
	if err != nil {
		t.Fatal(err)
	}

	if !slices.Equal(names(got), []string{"app"}) {
		t.Errorf("InFolder = %v, want [app]", names(got))
	}
}

func TestStartBootsAStoppedSandbox(t *testing.T) {
	f := newFakeMSB()
	f.add("app", msb.Stopped, "orcasm=1")

	if err := (sandbox.Manager{MSB: f}).Start(context.Background(), "app"); err != nil {
		t.Fatal(err)
	}

	if f.sandboxes["app"].status != msb.Running {
		t.Errorf("status = %s, want Running", f.sandboxes["app"].status)
	}
}

func TestStartHandsMSBTheSandboxSecretEnv(t *testing.T) {
	f := newFakeMSB()
	f.add("app", msb.Stopped, "orcasm=1")
	m := sandbox.Manager{MSB: f, StartEnv: func(_ context.Context, name string) ([]string, error) {
		return []string{"ORCASM_SECRET_" + strings.ToUpper(name) + "=token"}, nil
	}}

	if err := m.Start(context.Background(), "app"); err != nil {
		t.Fatal(err)
	}

	if got := f.sandboxes["app"].startEnv; !slices.Equal(got, []string{"ORCASM_SECRET_APP=token"}) {
		t.Errorf("start env = %q", got)
	}
}

func TestStartWithoutSecretValuesDoesNotStart(t *testing.T) {
	f := newFakeMSB()
	f.add("app", msb.Stopped, "orcasm=1")
	m := sandbox.Manager{MSB: f, StartEnv: func(context.Context, string) ([]string, error) {
		return nil, errors.New("no token for ANTHROPIC_API_KEY on this host")
	}}

	err := m.Start(context.Background(), "app")

	if err == nil || !strings.Contains(err.Error(), "ANTHROPIC_API_KEY") {
		t.Errorf("err = %v", err)
	}
	if f.sandboxes["app"].status != msb.Stopped {
		t.Error("started without its secrets")
	}
}

func TestStartARunningSandboxIsNoop(t *testing.T) {
	f := newFakeMSB()
	f.add("app", msb.Running, "orcasm=1")

	if err := (sandbox.Manager{MSB: f}).Start(context.Background(), "app"); err != nil {
		t.Fatal(err)
	}
}

func TestStopShutsDownASandbox(t *testing.T) {
	f := newFakeMSB()
	f.add("app", msb.Running, "orcasm=1")

	if err := (sandbox.Manager{MSB: f}).Stop(context.Background(), "app", false); err != nil {
		t.Fatal(err)
	}

	if f.sandboxes["app"].status != msb.Stopped {
		t.Errorf("status = %s, want Stopped", f.sandboxes["app"].status)
	}
}

func TestLifecycleRefusesSandboxesOrcasmDidNotMake(t *testing.T) {
	f := newFakeMSB()
	f.add("oc-dev", msb.Running, "team=platform")
	m := sandbox.Manager{MSB: f}
	ctx := context.Background()

	for op, err := range map[string]error{
		"start": m.Start(ctx, "oc-dev"),
		"stop":  m.Stop(ctx, "oc-dev", true),
		"rm":    m.Remove(ctx, "oc-dev", sandbox.RemoveOptions{Force: true}),
	} {
		if err == nil || !strings.Contains(err.Error(), `no Sandbox named "oc-dev"`) {
			t.Errorf("%s: err = %v, want no Sandbox named", op, err)
		}
	}
	if f.sandboxes["oc-dev"].status != msb.Running || len(f.removed) != 0 {
		t.Error("touched a sandbox orcasm did not make")
	}
}
