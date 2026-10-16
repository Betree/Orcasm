package stack_test

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Betree/Orcasm/internal/stack"
)

// TestRealStackUpGivesTheWorkbenchTheWorktreeCAAndPlaceholders really
// starts a generic Workbench; it pulls an image and needs a Docker daemon,
// so it runs only with ORCASM_TEST_DOCKER_UP=1 (e.g. in a spike Sandbox
// created with `--secret FAKE_TOKEN@example.com`).
func TestRealStackUpGivesTheWorkbenchTheWorktreeCAAndPlaceholders(t *testing.T) {
	if os.Getenv("ORCASM_TEST_DOCKER_UP") != "1" {
		t.Skip("set ORCASM_TEST_DOCKER_UP=1 to start a real Stack")
	}
	ctx := context.Background()
	checkout := t.TempDir()
	if err := os.WriteFile(filepath.Join(checkout, "marker"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	st, err := stack.Generate(ctx, stack.ExecDocker, stack.Spec{
		Worktree:  "spike17",
		Checkouts: []stack.Checkout{{Repository: "shop", Path: checkout}},
		Secrets:   []string{"FAKE_TOKEN"},
		Home:      t.TempDir(),
	}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Up(ctx, stack.ExecDocker); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := st.Down(ctx, stack.ExecDocker); err != nil {
			t.Error(err)
		}
	}()
	out, err := stack.ExecDocker(ctx, st.Command("exec", "-T", st.Workbench, "sh", "-c",
		`cat /workspace/shop/marker; echo; echo "$FAKE_TOKEN"; grep -c 'BEGIN CERT' "$SSL_CERT_FILE"`)...)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("in the Workbench:\n%s", out)
	lines := strings.Fields(string(out))
	if len(lines) != 3 || lines[0] != "hello" || lines[1] != "$MSB_FAKE_TOKEN" || lines[2] == "0" {
		t.Errorf("Workbench sees %q, want [hello $MSB_FAKE_TOKEN <cert count>]", lines)
	}
}

// TestRealComposeAcceptsTheStack merges a generated Stack with the real
// `docker compose config` (no daemon needed). Skipped without Compose.
func TestRealComposeAcceptsTheStack(t *testing.T) {
	if err := exec.Command("docker", "compose", "version").Run(); err != nil {
		t.Skip("docker compose not available")
	}
	checkout := t.TempDir()
	compose := `
services:
  app:
    image: node:22
    ports: ["3000:3000"]
    volumes: [".:/app"]
    environment:
      FOO: bar
  db:
    image: postgres:17
    ports: ["5432:5432"]
  debug:
    image: busybox
    profiles: [debug]
    ports: ["9000:9000"]
`
	if err := os.WriteFile(filepath.Join(checkout, "compose.yml"), []byte(compose), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for _, workbench := range []string{"app", ""} {
		t.Run("workbench="+workbench, func(t *testing.T) {
			s := stack.Spec{
				Worktree:     "Feat/Login",
				Checkouts:    []stack.Checkout{{Repository: "shop", Path: checkout}},
				ComposeFiles: []string{"compose.yml"},
				Workbench:    workbench,
				Harnesses:    []string{"claude"},
				Secrets:      []string{"GH_TOKEN"},
				Home:         t.TempDir(),
			}
			st, err := stack.Generate(ctx, stack.ExecDocker, s, t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			out, err := stack.ExecDocker(ctx, st.Command("config", "--format", "json")...)
			if err != nil {
				t.Fatal(err)
			}
			var merged struct {
				Name     string
				Services map[string]struct {
					Ports       []any
					Environment map[string]string
					Volumes     []struct{ Source, Target string }
				}
			}
			if err := json.Unmarshal(out, &merged); err != nil {
				t.Fatal(err)
			}
			if merged.Name != stack.ProjectName("Feat/Login") {
				t.Errorf("name = %q", merged.Name)
			}
			for name, svc := range merged.Services {
				if len(svc.Ports) != 0 {
					t.Errorf("%s still publishes %v", name, svc.Ports)
				}
			}
			wb, ok := merged.Services[st.Workbench]
			if !ok {
				t.Fatalf("no Workbench service %q in %v", st.Workbench, merged.Services)
			}
			if wb.Environment["GH_TOKEN"] != "$$MSB_GH_TOKEN" && wb.Environment["GH_TOKEN"] != "$MSB_GH_TOKEN" {
				t.Errorf("GH_TOKEN = %q", wb.Environment["GH_TOKEN"])
			}
			if wb.Environment["SSL_CERT_FILE"] != stack.CABundle {
				t.Errorf("SSL_CERT_FILE = %q", wb.Environment["SSL_CERT_FILE"])
			}
			var targets []string
			for _, v := range wb.Volumes {
				targets = append(targets, v.Target)
			}
			for _, want := range []string{"/workspace/shop", checkout, stack.CABundle, stack.HarnessLayer, filepath.Join(s.Home, ".claude")} {
				if !slices.Contains(targets, want) {
					t.Errorf("Workbench mounts %v, missing %s", targets, want)
				}
			}
			if workbench == "app" && (wb.Environment["FOO"] != "bar" || !slices.Contains(targets, "/app")) {
				t.Errorf("the Project's own Workbench settings were lost: %+v", wb)
			}
		})
	}
}
