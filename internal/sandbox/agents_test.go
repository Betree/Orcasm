package sandbox_test

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Betree/Orcasm/internal/sandbox"
	"github.com/Betree/Orcasm/internal/workbench"
)

// Orca installs an agent's status hooks at start only if the agent's CLI is
// on its PATH, so the wrappers must be in place before provisioning starts
// Orca.
func TestProvisionPutsAWrapperPerChosenHarnessBeforeOrcaStarts(t *testing.T) {
	f := &fakeCreateMSB{}
	if err := sandbox.Provision(context.Background(), f, demoConfig(), sandbox.OrcaVersion, hostValues, io.Discard); err != nil {
		t.Fatal(err)
	}
	wrappers, orca := -1, -1
	for i, c := range f.execs {
		if len(c.argv) < 4 || c.argv[0] != "bash" {
			continue
		}
		switch {
		case c.argv[3] == "install-agents" && wrappers < 0:
			wrappers = i
		case strings.Contains(c.argv[2], "orca-ide serve") && orca < 0:
			orca = i
		}
	}
	if wrappers < 0 || orca < 0 || wrappers > orca {
		t.Fatalf("wrapper install at exec %d, Orca's start at %d: %+v", wrappers, orca, f.execs)
	}
	c := f.execs[wrappers]
	if c.user != "" {
		t.Errorf("wrappers installed as %q, want root", c.user)
	}
	root := t.TempDir()
	cmd := exec.Command(c.argv[0], c.argv[1:]...)
	cmd.Env = append(os.Environ(), "ORCASM_ROOT="+root)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("install: %v: %s", err, out)
	}
	for _, h := range []string{"claude", "codex"} { // demoConfig's harnesses
		p := filepath.Join(root, "usr/local/bin", h)
		raw, err := os.ReadFile(p)
		if err != nil || string(raw) != string(workbench.AgentWrapper) {
			t.Errorf("%s is not the agent wrapper: %v", p, err)
		}
		if fi, err := os.Stat(p); err != nil || fi.Mode().Perm() != 0o755 {
			t.Errorf("%s mode: %v", p, err)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "usr/local/bin/opencode")); !os.IsNotExist(err) {
		t.Errorf("opencode wrapper installed though not chosen: %v", err)
	}
}
