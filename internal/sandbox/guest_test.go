package sandbox_test

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Betree/Orcasm/internal/guest"
	"github.com/Betree/Orcasm/internal/sandbox"
	"github.com/Betree/Orcasm/internal/wizard"
	"github.com/Betree/Orcasm/internal/worktree"
)

// guestMSB records copies and runs execs locally, so the install script
// really runs (relocated under ORCASM_ROOT).
type guestMSB struct {
	t      *testing.T
	root   string
	copied map[string]string // guest path -> host path
	users  []string
}

func (g *guestMSB) CopyIn(_ context.Context, _, hostPath, guestPath string) error {
	g.copied[guestPath] = hostPath
	target := filepath.Join(g.root, guestPath)
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	raw, err := os.ReadFile(hostPath)
	if err != nil {
		return err
	}
	return os.WriteFile(target, raw, 0o644)
}

func (g *guestMSB) ExecStreaming(ctx context.Context, _, user string, out io.Writer, argv ...string) error {
	g.users = append(g.users, user)
	c := exec.CommandContext(ctx, argv[0], argv[1:]...)
	c.Env = append(os.Environ(), "ORCASM_ROOT="+g.root, "ORCASM_OWNER="+currentUser(g.t))
	c.Stdout = out
	c.Stderr = out
	return c.Run()
}

func currentUser(t *testing.T) string {
	out, err := exec.Command("id", "-un").Output()
	if err != nil {
		t.Fatal(err)
	}
	return string(out[:len(out)-1])
}

func TestInstallOrcasmPutsTheBinaryAndTheWorktreeConfigInTheSandbox(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "orcasm-linux")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\necho orcasm\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	g := &guestMSB{t: t, root: t.TempDir(), copied: map[string]string{}}
	cfg := demoConfig()
	cfg.Project.ComposeFiles = []string{"compose.yml", "compose.dev.yml"}
	cfg.Project.Workbench = "app"
	cfg.Project.WebService, cfg.Project.WebPort = "web", 3000
	cfg.Secrets = []wizard.Secret{{Name: "GH_TOKEN", Hosts: []string{"github.com"}}}

	if err := sandbox.InstallOrcasm(context.Background(), g, cfg, bin, io.Discard); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(g.users, []string{""}) {
		t.Errorf("exec users %q, want root only", g.users)
	}
	out, err := exec.Command(filepath.Join(g.root, "usr/local/bin/orcasm")).Output()
	if err != nil || string(out) != "orcasm\n" {
		t.Errorf("installed binary: %q, %v", out, err)
	}
	got, err := worktree.ReadConfig(filepath.Join(g.root, guest.ConfDir, guest.WorktreeConfig))
	if err != nil {
		t.Fatal(err)
	}
	want := worktree.Config{
		Main:         "/home/dev/demo",
		ComposeFiles: []string{"compose.yml", "compose.dev.yml"},
		Workbench:    "app",
		WebService:   "web",
		WebPort:      3000,
		Harnesses:    []string{"claude", "codex"},
		Secrets:      []string{"GH_TOKEN"},
		Home:         "/home/dev",
	}
	if !reflect.DeepEqual(got, want) {
		raw, _ := json.Marshal(got)
		t.Errorf("config %s, want %+v", raw, want)
	}
	if fi, err := os.Stat(filepath.Join(g.root, guest.WorktreesDir)); err != nil || !fi.IsDir() {
		t.Errorf("%s: %v", guest.WorktreesDir, err)
	}
}

func installed(t *testing.T, cfg wizard.Config) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "orcasm-linux")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	g := &guestMSB{t: t, root: t.TempDir(), copied: map[string]string{}}
	if err := sandbox.InstallOrcasm(context.Background(), g, cfg, bin, io.Discard); err != nil {
		t.Fatal(err)
	}
	return g.root
}

func TestTheHookRelayRunsAsASystemdUnitOfTheSandboxUser(t *testing.T) {
	root := installed(t, demoConfig())
	raw, err := os.ReadFile(filepath.Join(root, "etc/systemd/system", sandbox.HookRelayUnit+".service"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"User=dev", "ExecStart=/usr/local/bin/orcasm worktree hook-relay", "Restart=always", "After=docker.service", "WantedBy=multi-user.target"} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("unit lacks %q:\n%s", want, raw)
		}
	}
}
