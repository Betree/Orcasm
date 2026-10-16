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
	"github.com/Betree/Orcasm/internal/wizard"
)

type execCall struct {
	name, user string
	argv       []string
}

// fakeMSB records what Provision asks of microsandbox.
type fakeMSB struct {
	created []msb.CreateOptions
	execs   []execCall
	execErr error
}

func (f *fakeMSB) Create(_ context.Context, opts msb.CreateOptions) error {
	f.created = append(f.created, opts)
	return nil
}

func (f *fakeMSB) ExecTo(_ context.Context, name, user string, out io.Writer, argv ...string) error {
	f.execs = append(f.execs, execCall{name, user, argv})
	io.WriteString(out, "==> progress\n")
	return f.execErr
}

func demoConfig() wizard.Config {
	return wizard.Config{
		Folder:    "/home/u/code/demo",
		Name:      "demo",
		Resources: wizard.Resources{CPUs: 4, MemoryGiB: 8, RootDiskGiB: 32, DockerDiskGiB: 100},
		HostPort:  6769,
		Harnesses: []string{"claude", "codex"},
	}
}

func TestProvisionCreatesTheSandboxFromTheWizardConfig(t *testing.T) {
	f := &fakeMSB{}
	if err := sandbox.Provision(context.Background(), f, demoConfig(), io.Discard); err != nil {
		t.Fatal(err)
	}
	want := msb.CreateOptions{
		Image: "ghcr.io/superradcompany/ubuntu-systemd:24.04",
		Name:  "demo",
		Labels: map[string]string{
			"orcasm":         "1",
			"orcasm.project": "/home/u/code/demo",
			"orcasm.port":    "6769",
			"orcasm.schema":  "0",
		},
		CPUs:        4,
		MemoryGiB:   8,
		RootDiskGiB: 32,
		OwnedDisks:  []msb.OwnedDisk{{Path: "/var/lib/docker", SizeGiB: 100}},
		Ports:       []msb.Port{{HostAddr: "127.0.0.1", Host: 6769, Guest: 6768}},
		Shell:       "/bin/bash",
		Workdir:     "/home/dev/demo",
		Mkdirs:      []string{"/home/dev/demo"},
	}
	if len(f.created) != 1 || !reflect.DeepEqual(f.created[0], want) {
		t.Errorf("created %+v\nwant %+v", f.created, want)
	}
}

func TestProvisionRunsTheProvisioningScriptAsRoot(t *testing.T) {
	f := &fakeMSB{}
	var out strings.Builder
	if err := sandbox.Provision(context.Background(), f, demoConfig(), &out); err != nil {
		t.Fatal(err)
	}
	if len(f.execs) != 1 {
		t.Fatalf("want one exec, got %+v", f.execs)
	}
	c := f.execs[0]
	if c.name != "demo" || c.user != "" {
		t.Errorf("exec in %q as %q, want demo as root", c.name, c.user)
	}
	if len(c.argv) < 3 || c.argv[0] != "bash" || c.argv[1] != "-c" || !strings.Contains(c.argv[2], "orca-ide serve") {
		t.Fatalf("want bash -c <provisioning script>, got %q", c.argv)
	}
	wantArgs := []string{"provision", "dev", "6769", "1.4.227", "/home/dev/demo", "claude", "codex"}
	if got := c.argv[3:]; !reflect.DeepEqual(got, wantArgs) {
		t.Errorf("script args %q, want %q", got, wantArgs)
	}
	if !strings.Contains(out.String(), "==> progress") {
		t.Errorf("want script output streamed, got %q", out.String())
	}
}

func TestProvisionReportsAFailedProvisioning(t *testing.T) {
	f := &fakeMSB{execErr: errors.New("apt-get exploded")}
	err := sandbox.Provision(context.Background(), f, demoConfig(), io.Discard)
	if err == nil || !strings.Contains(err.Error(), "apt-get exploded") || !strings.Contains(err.Error(), "demo") {
		t.Fatalf("want error naming the Sandbox and the cause, got %v", err)
	}
}
