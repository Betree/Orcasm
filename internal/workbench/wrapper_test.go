package workbench_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Betree/Orcasm/internal/workbench"
)

// rig runs the agent wrapper as `claude` against fake docker/socat/pgrep and
// a fake real agent, all recording their argv under rec.
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
		worktrees: filepath.Join(root, "home", "orca", "workspaces"),
		bin:       filepath.Join(root, "fakebin"),
		agentBin:  filepath.Join(root, "agentbin"),
		rec:       filepath.Join(root, "rec"),
		wrapper:   filepath.Join(root, "wrappers", "claude"),
	}
	for _, d := range []string{r.home, r.worktrees, r.bin, r.agentBin, r.rec, filepath.Dir(r.wrapper)} {
		must(t, os.MkdirAll(d, 0o755))
	}
	must(t, os.WriteFile(r.wrapper, workbench.AgentWrapper, 0o755))
	r.script(filepath.Join(r.agentBin, "claude"), `echo "real claude $*"`)
	r.script(filepath.Join(r.bin, "docker"), `echo "$*" >>"$REC/docker"
case $1 in ps) cat "$REC/ps-output" 2>/dev/null ;; esac`)
	r.script(filepath.Join(r.bin, "socat"), `echo "$*" >"$REC/socat"`)
	r.script(filepath.Join(r.bin, "pgrep"), `exit 1`)
	return r
}

func (r *rig) script(path, body string) {
	r.t.Helper()
	must(r.t, os.WriteFile(path, []byte("#!/bin/bash\n"+body+"\n"), 0o755))
}

// workbench makes `docker ps` report a running Workbench for worktree.
func (r *rig) workbench(id, worktree string) {
	must(r.t, os.WriteFile(filepath.Join(r.rec, "ps-output"), []byte(id+" "+worktree+"\n"), 0o644))
}

func (r *rig) run(dir string, env []string, args ...string) (string, error) {
	cmd := exec.Command(r.wrapper, args...)
	cmd.Dir = dir
	cmd.Env = append([]string{
		"HOME=" + r.home,
		"PATH=" + r.bin + ":/usr/bin:/bin",
		"REC=" + r.rec,
		"ORCASM_AGENT_BIN_DIR=" + r.agentBin,
	}, env...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func (r *rig) recorded(name string) string {
	b, _ := os.ReadFile(filepath.Join(r.rec, name))
	return string(b)
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func TestOutsideAWorktreeTheAgentRunsInTheSandbox(t *testing.T) {
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
	if strings.Contains(r.recorded("docker"), "exec") {
		t.Errorf("docker exec was called: %s", r.recorded("docker"))
	}
}

func (r *rig) execLine() string {
	for _, l := range strings.Split(r.recorded("docker"), "\n") {
		if strings.HasPrefix(l, "exec ") {
			return l
		}
	}
	return ""
}

func TestInAWorktreeTheAgentRunsInItsWorkbench(t *testing.T) {
	r := newRig(t)
	wt := filepath.Join(r.worktrees, "demo", "wt1")
	sub := filepath.Join(wt, "pkg")
	must(t, os.MkdirAll(sub, 0o755))
	r.workbench("c0ffee", wt)

	out, err := r.run(sub, []string{"ORCA_PANE_KEY=tab:pane", "ORCA_AGENT_HOOK_TOKEN=secret", "UNRELATED=x"}, "--resume", "abc")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if strings.Contains(out, "real claude") {
		t.Errorf("ran in the Sandbox: %s", out)
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
	wt := filepath.Join(r.worktrees, "demo", "wt1")
	must(t, os.MkdirAll(wt, 0o755))
	r.workbench("c0ffee", wt)
	if out, err := r.run(wt, nil); err != nil {
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
}

func TestTheHookPortIsRelayedFromTheBridgeToOrca(t *testing.T) {
	r := newRig(t)
	wt := filepath.Join(r.worktrees, "demo", "wt1")
	must(t, os.MkdirAll(wt, 0o755))
	r.workbench("c0ffee", wt)
	endpoint := filepath.Join(r.home, "endpoint.env")
	// Orca rewrites this file on restart; the env of an old shell goes stale.
	must(t, os.WriteFile(endpoint, []byte("ORCA_AGENT_HOOK_PORT=40001\nORCA_AGENT_HOOK_VERSION=1\n"), 0o600))

	if out, err := r.run(wt, []string{"ORCA_AGENT_HOOK_PORT=1", "ORCA_AGENT_HOOK_ENDPOINT=" + endpoint}); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	want := "TCP-LISTEN:40001,bind=172.17.0.1,reuseaddr,fork TCP:127.0.0.1:40001\n"
	var got string
	for i := 0; i < 50 && got == ""; i++ { // socat is started in the background
		time.Sleep(20 * time.Millisecond)
		got = r.recorded("socat")
	}
	if got != want {
		t.Errorf("socat got %q, want %q", got, want)
	}
}

func TestARunningRelayIsReused(t *testing.T) {
	r := newRig(t)
	r.script(filepath.Join(r.bin, "pgrep"), `exit 0`)
	wt := filepath.Join(r.worktrees, "demo", "wt1")
	must(t, os.MkdirAll(wt, 0o755))
	r.workbench("c0ffee", wt)

	if out, err := r.run(wt, []string{"ORCA_AGENT_HOOK_PORT=40001"}); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	time.Sleep(200 * time.Millisecond)
	if got := r.recorded("socat"); got != "" {
		t.Errorf("started a second relay: %q", got)
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
	must(t, os.MkdirAll(filepath.Dir(native), 0o755))
	must(t, os.MkdirAll(filepath.Join(codexPkg, "bin"), 0o755))
	r.script(filepath.Join(codexPkg, "bin", "codex.js"), "")
	r.script(native, "")
	must(t, os.Symlink(filepath.Join(codexPkg, "bin", "codex.js"), filepath.Join(r.agentBin, "codex")))
	codexWrapper := filepath.Join(filepath.Dir(r.wrapper), "codex")
	must(t, os.WriteFile(codexWrapper, workbench.AgentWrapper, 0o755))
	r.wrapper = codexWrapper
	wt := filepath.Join(r.worktrees, "demo", "wt1")
	must(t, os.MkdirAll(wt, 0o755))
	r.workbench("c0ffee", wt)

	if out, err := r.run(wt, nil, "--no-daemon"); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	nativeReal, _ := filepath.EvalSymlinks(native)
	if want := " c0ffee " + nativeReal + " --no-daemon"; !strings.HasSuffix(r.execLine(), want) {
		t.Errorf("docker exec line does not end with %q:\n%s", want, r.execLine())
	}
}

func TestAWorktreeWithoutARunningWorkbenchFails(t *testing.T) {
	r := newRig(t)
	wt := filepath.Join(r.worktrees, "demo", "wt1")
	must(t, os.MkdirAll(wt, 0o755))

	out, err := r.run(wt, nil)
	if err == nil {
		t.Fatalf("succeeded: %s", out)
	}
	if !strings.Contains(out, "Workbench") || strings.Contains(out, "real claude") {
		t.Errorf("got %q", out)
	}
}
