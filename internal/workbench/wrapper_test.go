package workbench_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/Betree/Orcasm/internal/workbench"
)

// rig runs the agent wrapper as `claude` against fake docker and orcasm
// and a fake real agent, all recording their argv under rec.
type rig struct {
	t         *testing.T
	home      string
	worktrees string
	bin       string // fakes, first on PATH
	agentBin  string // where the real agents live
	rec       string
	wrapper   string
}

func newRig(t *testing.T) *rig {
	t.Helper()
	root := t.TempDir()
	r := &rig{
		t:         t,
		home:      filepath.Join(root, "home"),
		worktrees: filepath.Join(root, "worktrees"),
		bin:       filepath.Join(root, "fakebin"),
		agentBin:  filepath.Join(root, "harness", "bin"),
		rec:       filepath.Join(root, "rec"),
		wrapper:   filepath.Join(root, "wrappers", "claude"),
	}
	for _, d := range []string{r.home, r.worktrees, r.bin, r.agentBin, r.rec, filepath.Dir(r.wrapper)} {
		must(t, os.MkdirAll(d, 0o755))
	}
	must(t, os.WriteFile(r.wrapper, workbench.AgentWrapper, 0o755))
	r.script(filepath.Join(r.agentBin, "claude"), `echo "real claude $*"`)
	r.script(filepath.Join(r.bin, "docker"), `echo "$*" >>"$REC/docker"`)
	// `orcasm worktree workbench DIR`: progress on stderr, the id on stdout.
	r.script(filepath.Join(r.bin, "orcasm"), `echo "$*" >>"$REC/orcasm"
echo "orcasm: the Stack is up" >&2
cat "$REC/workbench" 2>/dev/null || { echo "orcasm: Worktree has no Stack" >&2; exit 1; }`)
	return r
}

func (r *rig) script(path, body string) {
	r.t.Helper()
	must(r.t, os.MkdirAll(filepath.Dir(path), 0o755))
	must(r.t, os.WriteFile(path, []byte("#!/bin/bash\n"+body+"\n"), 0o755))
}

// workbench makes `orcasm worktree workbench` name a running Workbench.
func (r *rig) workbench(id string) {
	must(r.t, os.WriteFile(filepath.Join(r.rec, "workbench"), []byte(id+"\n"), 0o644))
}

// worktree creates Orca's Worktree <worktrees>/demo/wt1 and returns it.
func (r *rig) worktree() string {
	wt := filepath.Join(r.worktrees, "demo", "wt1")
	must(r.t, os.MkdirAll(wt, 0o755))
	return wt
}

func (r *rig) run(dir string, env []string, args ...string) (string, error) {
	cmd := exec.Command(r.wrapper, args...)
	cmd.Dir = dir
	cmd.Env = append([]string{
		"HOME=" + r.home,
		"PATH=" + r.bin + ":/usr/bin:/bin",
		"REC=" + r.rec,
		"ORCASM_AGENT_BIN_DIR=" + r.agentBin,
		"ORCASM_WORKTREES=" + r.worktrees,
	}, env...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func (r *rig) recorded(name string) string {
	b, _ := os.ReadFile(filepath.Join(r.rec, name))
	return string(b)
}

func (r *rig) execLine() string {
	for _, l := range strings.Split(r.recorded("docker"), "\n") {
		if strings.HasPrefix(l, "exec ") {
			return l
		}
	}
	return ""
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func TestOnMainTheAgentRunsDirectlyInTheSandbox(t *testing.T) {
	r := newRig(t)
	main := filepath.Join(r.home, "demo")
	must(t, os.MkdirAll(main, 0o755))

	out, err := r.run(main, nil, "--dangerously-skip-permissions")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if out != "real claude --dangerously-skip-permissions\n" {
		t.Errorf("got %q", out)
	}
	if r.recorded("docker") != "" || r.recorded("orcasm") != "" {
		t.Errorf("looked for a Workbench: docker %q, orcasm %q", r.recorded("docker"), r.recorded("orcasm"))
	}
}

func TestInAWorktreeTheAgentRunsInItsWorkbenchOnceTheStackIsUp(t *testing.T) {
	r := newRig(t)
	sub := filepath.Join(r.worktree(), "pkg")
	must(t, os.MkdirAll(sub, 0o755))
	r.workbench("c0ffee")

	out, err := r.run(sub, []string{"ORCA_PANE_KEY=tab:pane", "ORCA_AGENT_HOOK_TOKEN=secret", "UNRELATED=x"}, "--resume", "abc")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if strings.Contains(out, "real claude") {
		t.Errorf("ran in the Sandbox: %s", out)
	}
	// The wait (and its progress) is orcasm's, shown to the user.
	if r.recorded("orcasm") != "worktree workbench "+sub+"\n" {
		t.Errorf("orcasm called with %q", r.recorded("orcasm"))
	}
	if !strings.Contains(out, "the Stack is up") {
		t.Errorf("progress not shown: %q", out)
	}
	line := r.execLine()
	for _, want := range []string{
		"exec -i ", // no tty here: chat-view sessions talk over pipes
		"-u " + strconv.Itoa(os.Getuid()) + ":" + strconv.Itoa(os.Getgid()),
		"-w " + sub,
		"-e HOME=" + r.home,
		"-e ORCASM_HOOK_HOST=host.docker.internal",
		"-e ORCA_PANE_KEY ", // forwarded by name: values never land in argv
		"-e ORCA_AGENT_HOOK_TOKEN ",
		" c0ffee " + filepath.Join(r.agentBin, "claude") + " --resume abc",
	} {
		if !strings.Contains(line, want) {
			t.Errorf("docker exec line lacks %q:\n%s", want, line)
		}
	}
	for _, unwanted := range []string{"secret", "UNRELATED", " -t "} {
		if strings.Contains(line, unwanted) {
			t.Errorf("docker exec line has %q:\n%s", unwanted, line)
		}
	}
}

func TestWithoutAWorkbenchTheAgentDoesNotFallBackToTheSandbox(t *testing.T) {
	r := newRig(t)
	wt := r.worktree()

	out, err := r.run(wt, nil)
	if err == nil {
		t.Fatalf("succeeded: %s", out)
	}
	if !strings.Contains(out, "no Stack") || strings.Contains(out, "real claude") || r.execLine() != "" {
		t.Errorf("got %q, docker %q", out, r.recorded("docker"))
	}
}

// A stand-in for Orca's ~/.orca/agent-hooks/claude-hook.sh: the two things
// the Workbench copy must change are the PID capture and the loopback POST.
const orcaHookScript = `#!/bin/sh
echo "agent-pid=${ORCA_HOOK_AGENT_PID:-}"
printf '%s' "$payload" | curl -sS -X POST "http://127.0.0.1:${ORCA_AGENT_HOOK_PORT}/hook/claude" \
  --noproxy "127.0.0.1" \
  --data-binary @-
`

func TestTheWorkbenchCopyOfOrcasHookScriptsReachesOrca(t *testing.T) {
	r := newRig(t)
	hooks := filepath.Join(r.home, ".orca", "agent-hooks")
	must(t, os.MkdirAll(hooks, 0o755))
	must(t, os.WriteFile(filepath.Join(hooks, "claude-hook.sh"), []byte(orcaHookScript), 0o755))
	r.workbench("c0ffee")
	if out, err := r.run(r.worktree(), nil); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}

	// Run the copy the way the Workbench does (it is mounted over
	// ~/.orca/agent-hooks there), with a fake curl.
	r.script(filepath.Join(r.bin, "curl"), `echo "curl $*"`)
	cmd := exec.Command(filepath.Join(r.home, ".cache", "orcasm", "agent-hooks", "claude-hook.sh"))
	cmd.Env = []string{"PATH=" + r.bin + ":/usr/bin:/bin", "ORCASM_HOOK_HOST=host.docker.internal",
		"ORCA_AGENT_HOOK_PORT=32881", "ORCA_HOOK_AGENT_PID=42"}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	// The PID is from the Workbench's PID namespace; Orca would probe it in
	// the Sandbox's and mark the agent ended.
	want := "agent-pid=\ncurl -sS -X POST http://host.docker.internal:32881/hook/claude --noproxy * --data-binary @-\n"
	if string(out) != want {
		t.Errorf("got\n%s\nwant\n%s", out, want)
	}
	// Orca's own scripts still serve Main.
	if orig, _ := os.ReadFile(filepath.Join(hooks, "claude-hook.sh")); string(orig) != orcaHookScript {
		t.Errorf("Orca's script was changed:\n%s", orig)
	}
}

// The line of Orca's OpenCode status plugin that posts to its hook server.
const openCodePluginPost = "  const url = `http://127.0.0.1:${coords.port}/hook/opencode`;\n"

func TestOpenCodeInTheWorkbenchGetsAStatusPluginPostingToTheBridge(t *testing.T) {
	r := newRig(t)
	opencode := filepath.Join(filepath.Dir(r.wrapper), "opencode")
	must(t, os.WriteFile(opencode, workbench.AgentWrapper, 0o755))
	r.wrapper = opencode
	r.script(filepath.Join(r.agentBin, "opencode"), "")
	plugin := filepath.Join(r.home, ".config", "opencode", "plugins", "orca-status.js")
	must(t, os.MkdirAll(filepath.Dir(plugin), 0o755))
	must(t, os.WriteFile(plugin, []byte(openCodePluginPost), 0o644))
	r.workbench("c0ffee")

	if out, err := r.run(r.worktree(), nil); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	line := r.execLine()
	_, after, ok := strings.Cut(line, "-e OPENCODE_CONFIG_DIR=")
	if !ok {
		t.Fatalf("no OPENCODE_CONFIG_DIR in %q", line)
	}
	dir := strings.Fields(after)[0]
	if !strings.HasPrefix(dir, filepath.Join(r.home, ".cache", "orcasm", "opencode-config")+"/") {
		t.Errorf("OPENCODE_CONFIG_DIR=%s is outside the Workbench's copy dir", dir)
	}
	got, err := os.ReadFile(filepath.Join(dir, "plugins", "orca-status.js"))
	must(t, err)
	want := "  const url = `http://${process.env.ORCASM_HOOK_HOST || \"127.0.0.1\"}:${coords.port}/hook/opencode`;\n"
	if string(got) != want {
		t.Errorf("plugin copy\n%s\nwant\n%s", got, want)
	}
	if orig, _ := os.ReadFile(plugin); string(orig) != openCodePluginPost {
		t.Errorf("Orca's plugin was changed:\n%s", orig)
	}
}

func TestCodexRunsAsItsNativeBinarySoTheWorkbenchNeedsNoNode(t *testing.T) {
	r := newRig(t)
	triple := map[string]string{"amd64": "x86_64-unknown-linux-musl", "arm64": "aarch64-unknown-linux-musl"}[runtime.GOARCH]
	pkg := map[string]string{"amd64": "codex-linux-x64", "arm64": "codex-linux-arm64"}[runtime.GOARCH]
	if triple == "" {
		t.Skip("no Codex native build for " + runtime.GOARCH)
	}
	// npm's layout: bin/codex -> lib/node_modules/@openai/codex/bin/codex.js
	codexPkg := filepath.Join(r.agentBin, "..", "lib", "node_modules", "@openai", "codex")
	native := filepath.Join(codexPkg, "node_modules", "@openai", pkg, "vendor", triple, "bin", "codex")
	r.script(filepath.Join(codexPkg, "bin", "codex.js"), "")
	r.script(native, "")
	must(t, os.Symlink(filepath.Join(codexPkg, "bin", "codex.js"), filepath.Join(r.agentBin, "codex")))
	codexWrapper := filepath.Join(filepath.Dir(r.wrapper), "codex")
	must(t, os.WriteFile(codexWrapper, workbench.AgentWrapper, 0o755))
	r.wrapper = codexWrapper
	r.workbench("c0ffee")

	if out, err := r.run(r.worktree(), nil, "--no-daemon"); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	nativeReal, _ := filepath.EvalSymlinks(native)
	if want := " c0ffee " + nativeReal + " --no-daemon"; !strings.HasSuffix(r.execLine(), want) {
		t.Errorf("docker exec line does not end with %q:\n%s", want, r.execLine())
	}
}
