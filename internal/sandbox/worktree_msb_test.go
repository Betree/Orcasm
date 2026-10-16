//go:build msb

package sandbox_test

// Orca's Worktree hooks end to end in a real Sandbox (several minutes, needs
// network): a Worktree created in Orca with `--setup run` brings its Stack
// up, archiving it tears the Stack down.
//
//	ORCASM_TEST_HOOKS_SANDBOX=<throwaway name> ORCASM_TEST_PORT=<free host port> \
//	  go test -tags msb -timeout 20m -run TestWorktreeHooksInARealSandbox ./internal/sandbox/
//
// ORCASM_TEST_KEEP=1 keeps the Sandbox; run again with it to skip
// provisioning and only reinstall orcasm.

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Betree/Orcasm/internal/msb"
	"github.com/Betree/Orcasm/internal/repository"
	"github.com/Betree/Orcasm/internal/sandbox"
	"github.com/Betree/Orcasm/internal/stack"
	"github.com/Betree/Orcasm/internal/wizard"
	"github.com/Betree/Orcasm/internal/worktree"
)

func TestWorktreeHooksInARealSandbox(t *testing.T) {
	name := os.Getenv("ORCASM_TEST_HOOKS_SANDBOX")
	port, _ := strconv.Atoi(os.Getenv("ORCASM_TEST_PORT"))
	if name == "" || port == 0 {
		t.Skip("ORCASM_TEST_HOOKS_SANDBOX / ORCASM_TEST_PORT not set")
	}
	ctx := context.Background()
	m := msb.CLI{}
	sb := m.Sandbox(name)
	keep := os.Getenv("ORCASM_TEST_KEEP") == "1"
	if !keep {
		t.Cleanup(func() { exec.Command("msb", "rm", "--force", name).Run() })
	}

	project := filepath.Join(t.TempDir(), "demo")
	writeFile(t, filepath.Join(project, "compose.yml"), "services:\n  web:\n    image: nginx:alpine\n    ports: [\"8080:80\"]\n")
	for _, args := range [][]string{{"init", "-q", "-b", "main"}, {"add", "-A"}, {"-c", "user.name=t", "-c", "user.email=t@e", "commit", "-qm", "init"}} {
		runIn(t, project, "git", args...)
	}
	cfg := wizard.Config{
		ProjectPath: project,
		Name:        name,
		Resources:   wizard.Resources{CPUs: 4, MemoryGiB: 8, RootDiskGiB: 32, DockerDiskGiB: 50},
		HostPort:    port,
		Harnesses:   []string{"claude", "codex", "opencode"},
		Project:     wizard.Project{ComposeFiles: []string{"compose.yml"}, WebService: "web", WebPort: 80},
	}
	main := sandbox.MainPath(project)

	existing, err := m.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	reuse := false
	for _, l := range existing {
		reuse = reuse || l.Name == name
	}
	if reuse && !keep {
		t.Fatalf("sandbox %s exists; set ORCASM_TEST_KEEP=1 to reuse it", name)
	}
	if !reuse {
		if err := sandbox.Provision(ctx, m, cfg, nil, testWriter{t}); err != nil {
			t.Fatal(err)
		}
	}
	bin := filepath.Join(t.TempDir(), "orcasm")
	runIn(t, ".", "go", "build", "-o", bin, "../../cmd/orcasm")
	if err := sandbox.InstallOrcasm(ctx, m, cfg, bin, testWriter{t}); err != nil {
		t.Fatal(err)
	}
	if !reuse {
		if err := repository.Copy(ctx, sb, repository.CopyOptions{HostPath: project, GuestPath: main, Owner: sandbox.User}); err != nil {
			t.Fatal(err)
		}
		if err := repository.RegisterInOrca(ctx, sb, sandbox.User, main); err != nil {
			t.Fatal(err)
		}
	}
	if out, err := sb.Exec(ctx, sandbox.User, worktree.Binary, "worktree", "install-hooks", main); err != nil {
		t.Fatalf("install-hooks: %v\n%s", err, out)
	}

	wt := "hooks-" + strconv.FormatInt(time.Now().Unix()%100000, 10)
	path := sandbox.WorktreesDir + "/demo/" + wt
	project = "orcasm-" + wt
	dev := func(script string) string {
		t.Helper()
		out, err := sb.Exec(ctx, sandbox.User, "bash", "-lc", script)
		if err != nil {
			t.Fatalf("%s: %v\n%s", script, err, out)
		}
		return out
	}
	dev("cd " + main + " && orca-ide worktree create --name " + wt + " --repo path:" + main + " --setup run --json >/dev/null")

	// Setup returns at once; the Stack comes up in the background.
	running := ""
	for i := 0; i < 90 && !(strings.Contains(running, "orcasm-workbench") && strings.Contains(running, "web")); i++ {
		time.Sleep(2 * time.Second)
		running = dev("docker compose -p " + project + " ps --status running --services 2>/dev/null || true")
	}
	if !strings.Contains(running, "web") || !strings.Contains(running, "orcasm-workbench") {
		t.Fatalf("Stack %s not up; running: %q; log:\n%s", project, running,
			dev("cat ~/.local/state/orcasm/stacks/"+project+"/up.log 2>&1 || true"))
	}
	if got := dev("docker compose -p " + project + " exec -T orcasm-workbench ls /workspace/demo"); !strings.Contains(got, "compose.yml") {
		t.Errorf("Workbench /workspace/demo: %q", got)
	}
	if got := dev("docker port " + project + "-web-1 || true"); strings.TrimSpace(got) != "" {
		t.Errorf("web publishes ports: %q", got)
	}

	// #20's proxy routes the Worktree's host name to its web service.
	if got := dev("curl -s -o /dev/null -w '%{http_code}' http://" + stack.Hostname(wt) + "/"); got != "200" {
		t.Errorf("http://%s/ answered %q", stack.Hostname(wt), got)
	}

	// #19: each harness's wrapper runs it in the Workbench, whose hook
	// scripts reach Orca through the relay. Orca installed its Claude hooks
	// because the wrapper was on its PATH when it started.
	if got := dev("systemctl is-active " + sandbox.HookRelayUnit); strings.TrimSpace(got) != "active" {
		t.Errorf("%s: %q", sandbox.HookRelayUnit, got)
	}
	if got := dev("ls ~/.orca/agent-hooks/"); !strings.Contains(got, "claude-hook.sh") {
		t.Errorf("Orca installed no Claude hooks: %q", got)
	}
	for _, h := range cfg.Harnesses {
		if got := dev("cd " + path + " && " + h + " --version 2>/dev/null"); strings.TrimSpace(got) == "" {
			t.Errorf("%s --version in the Worktree printed nothing", h)
		}
	}
	if got := dev("ls ~/.cache/orcasm/agent-hooks/"); !strings.Contains(got, "claude-hook.sh") {
		t.Errorf("the wrapper made no Workbench copy of Orca's hooks: %q", got)
	}
	hookPort := strings.TrimSpace(dev("grep -o '[0-9]*$' <<<\"$(grep ^ORCA_AGENT_HOOK_PORT= ~/.config/orca/agent-hooks/endpoint.env)\""))
	if got := dev("docker compose -p " + project + " exec -T orcasm-workbench curl -s -o /dev/null -w '%{http_code}' -X POST http://host.docker.internal:" + hookPort + "/hook/claude || true"); got != "403" {
		// Unauthenticated, so refused: but by Orca, through the relay.
		t.Errorf("hook POST from the Workbench answered %q, want Orca's 403", got)
	}

	dev("orca-ide worktree rm --worktree path:" + path + " --run-hooks --json >/dev/null")
	if got := dev("docker ps -aq --filter label=com.docker.compose.project=" + project); strings.TrimSpace(got) != "" {
		t.Errorf("containers left after archive: %q", got)
	}
	if got := dev("ls ~/.local/state/orcasm/stacks/ 2>/dev/null || true"); strings.Contains(got, project) {
		t.Errorf("state left after archive: %q", got)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func runIn(t *testing.T, dir, name string, args ...string) {
	t.Helper()
	c := exec.Command(name, args...)
	c.Dir = dir
	if out, err := c.CombinedOutput(); err != nil {
		t.Fatalf("%s %v: %v\n%s", name, args, err, out)
	}
}
