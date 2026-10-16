//go:build e2e

// Package e2e_test is the end-to-end suite (ADR 0006): it builds orcasm and
// drives the binary through a whole Sandbox life, from the Wizard's hidden
// answers file to `orcasm rm`, against a real msb. CI runs it on every new
// Orca release before the compatibility file lists it
// (.github/workflows/e2e.yml, orca-watch.yml). It takes 10 to 20 minutes and
// needs KVM, msb and network:
//
//	ORCASM_E2E=1 go test -tags e2e -timeout 40m -v ./e2e/
//
// ORCASM_E2E_ORCA is the Orca release under test (default: whatever the
// compatibility file picks). ORCASM_E2E_FROM_ORCA, when set, is the release
// the Sandbox is created with, so `orcasm upgrade` moves it to
// ORCASM_E2E_ORCA. ORCASM_E2E_NAME names the throwaway Sandbox (default
// e2e-<time>); it must not exist.
package e2e_test

import (
	"bytes"
	"cmp"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"text/template"
	"time"

	"github.com/Betree/Orcasm/internal/guest"
	"github.com/Betree/Orcasm/internal/sandbox"
	"github.com/Betree/Orcasm/internal/stack"
)

// dummyToken is the host value of the answers file's secret. The Sandbox
// must only ever see msb's placeholder for it.
const dummyToken = "not-a-real-token"

func TestASandboxLifeThroughTheBinary(t *testing.T) {
	if os.Getenv("ORCASM_E2E") != "1" {
		t.Skip("ORCASM_E2E=1 not set")
	}
	candidate := strings.TrimPrefix(os.Getenv("ORCASM_E2E_ORCA"), "v")
	from := cmp.Or(strings.TrimPrefix(os.Getenv("ORCASM_E2E_FROM_ORCA"), "v"), candidate)
	name := cmp.Or(os.Getenv("ORCASM_E2E_NAME"), "e2e-"+strconv.FormatInt(time.Now().Unix(), 36))
	tmp := t.TempDir()

	bin := filepath.Join(tmp, "orcasm")
	build := exec.Command("go", "build", "-o", bin, "../cmd/orcasm")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}

	project := filepath.Join(tmp, "demo")
	copyDir(t, "testdata/project", project)
	for _, args := range [][]string{{"init", "-q", "-b", "main"}, {"add", "-A"}, {"-c", "user.name=e2e", "-c", "user.email=e2e@example.com", "commit", "-qm", "Fixture Project"}} {
		run(t, project, "git", args...)
	}
	port := freePort(t)
	answers := filepath.Join(tmp, "answers.toml")
	writeAnswers(t, answers, map[string]any{"ProjectPath": project, "Name": name, "HostPort": port})

	o := orcasm{t: t, bin: bin, dir: project, env: []string{
		"XDG_CONFIG_HOME=" + filepath.Join(tmp, "config"), // keep the runner's own orcasm state out
		"ORCASM_NO_UPGRADE_NOTICE=1",
		"GEMINI_API_KEY=" + dummyToken,
	}}
	t.Cleanup(func() {
		// A safety net when a step failed before `orcasm rm`.
		exec.Command(bin, "rm", "--force", name).Run()
		exec.Command("msb", "rm", "--force", name).Run()
	})
	// sh runs a bash login script in the Sandbox as dev, over `orcasm ssh`.
	sh := func(script string) string {
		t.Helper()
		return o.run("", nil, "ssh", name, "--", "bash -lc "+shellQuote(script))
	}

	o.run("", nil, "doctor")

	out := o.run("", orcaOverride(from), "create", "--answers", answers)
	if !strings.Contains(out, "Sandbox "+name+" is ready") {
		t.Fatalf("create did not report the Sandbox ready:\n%s", out)
	}
	if from != "" && !strings.Contains(out, "Using Orca "+from) {
		t.Errorf("create did not use Orca %s:\n%s", from, out)
	}

	if out := o.run("", nil, "ls"); !strings.Contains(out, name) {
		t.Errorf("ls does not list %s:\n%s", name, out)
	}
	checkWebUI(t, o, name, port)

	// The Sandbox as provisioned: Orca runs as dev, Docker works, TLS goes
	// through msb's CA, and the secret is only a placeholder.
	got := sh(`systemctl is-active orca
docker run --rm hello-world >/dev/null && echo docker-ok
env -i PATH=/usr/bin:/bin curl -fsS -o /dev/null https://github.com && echo tls-ok
git -C ` + sandbox.MainPath(project) + ` log -1 --format=%s
command -v claude >/dev/null && echo claude-ok
curl -sS -o /dev/null -w 'proxy %{http_code}\n' http://no-such-worktree.localhost/`)
	if want := "active\ndocker-ok\ntls-ok\nFixture Project\nclaude-ok\nproxy 404\n"; got != want {
		t.Errorf("Sandbox checks: got\n%s\nwant\n%s", got, want)
	}
	if got := sh(`echo "$GEMINI_API_KEY"`); strings.Contains(got, dummyToken) {
		t.Errorf("the secret's value reached the Sandbox: %q", got)
	}

	checkWorktree(t, sh, sandbox.MainPath(project))

	in := "y\n" // restarting Orca is confirmed
	out = o.run(in, orcaOverride(candidate), "upgrade", name)
	if !strings.Contains(out, name+" is upgraded") {
		t.Errorf("upgrade did not finish:\n%s", out)
	}
	if candidate != "" {
		if got := strings.TrimSpace(sh(`dpkg-query -W -f='${Version}' orca-ide`)); got != candidate {
			t.Errorf("after upgrade Orca is %q, want %s", got, candidate)
		}
	}
	if got := strings.TrimSpace(sh("systemctl is-active orca")); got != "active" {
		t.Errorf("orca after upgrade: %q", got)
	}
	checkWebUI(t, o, name, port)

	o.run("", nil, "rm", "--force", name)
	if out := o.run("", nil, "ls"); strings.Contains(out, name) {
		t.Errorf("ls still lists %s after rm:\n%s", name, out)
	}
}

// checkWebUI checks Orca's web UI answers on the host port and that
// `orcasm url` prints a pairing link.
func checkWebUI(t *testing.T, o orcasm, name string, port int) {
	t.Helper()
	if out := o.run("", nil, "url", name); !strings.Contains(out, "web-index.html#pairing=") {
		t.Errorf("url printed no pairing link:\n%s", out)
	}
	resp, err := http.Get("http://127.0.0.1:" + strconv.Itoa(port) + "/web-index.html")
	if err != nil {
		t.Errorf("web UI: %v", err)
		return
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), "<title>Orca Web</title>") {
		t.Errorf("web UI on host port %d: %d %.200s", port, resp.StatusCode, body)
	}
}

// checkWorktree creates a Worktree through Orca, as a user would, and checks
// its Stack comes up behind the proxy with a Workbench the harness runs in,
// then archives it and checks the Stack is gone.
func checkWorktree(t *testing.T, sh func(string) string, main string) {
	t.Helper()
	wt := "e2e-" + strconv.FormatInt(time.Now().Unix()%100000, 10)
	path := guest.WorktreesDir + "/" + filepath.Base(main) + "/" + wt
	project := stack.ProjectName(wt)
	sh("cd " + main + " && orca-ide worktree create --name " + wt + " --repo path:" + main + " --json >/dev/null")

	running := ""
	for i := 0; i < 120 && !(strings.Contains(running, "orcasm-workbench") && strings.Contains(running, "web")); i++ {
		time.Sleep(2 * time.Second)
		running = sh("docker compose -p " + project + " ps --status running --services 2>/dev/null || true")
	}
	if !strings.Contains(running, "web") || !strings.Contains(running, "orcasm-workbench") {
		t.Fatalf("Stack %s not up; running: %q; log:\n%s", project, running,
			sh("cat ~/.local/state/orcasm/stacks/"+project+"/up.log 2>&1 || true"))
	}
	if got := sh("curl -s -o /dev/null -w '%{http_code}' http://" + stack.Hostname(wt) + "/"); got != "200" {
		t.Errorf("http://%s/ answered %q", stack.Hostname(wt), got)
	}
	if got := sh("cd " + path + " && claude --version 2>/dev/null"); strings.TrimSpace(got) == "" {
		t.Error("claude --version in the Worktree printed nothing")
	}

	sh("orca-ide worktree rm --worktree path:" + path + " --run-hooks --json >/dev/null")
	if got := sh("docker ps -aq --filter label=com.docker.compose.project=" + project); strings.TrimSpace(got) != "" {
		t.Errorf("containers left after archive: %q", got)
	}
}

// orcasm runs the built binary, with the runner's environment plus env.
type orcasm struct {
	t        *testing.T
	bin, dir string
	env      []string
}

// run runs orcasm args with stdin and extra environment, failing the test
// when it fails, and returns its stdout.
func (o orcasm) run(stdin string, env []string, args ...string) string {
	o.t.Helper()
	cmd := exec.Command(o.bin, args...)
	cmd.Dir = o.dir
	cmd.Env = append(append(withoutOverride(os.Environ()), o.env...), env...)
	cmd.Stdin = strings.NewReader(stdin)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	start := time.Now()
	err := cmd.Run()
	o.t.Logf("orcasm %s (%s)\n%s%s", strings.Join(args, " "), time.Since(start).Round(time.Second), stdout.String(), stderr.String())
	if err != nil {
		o.t.Fatalf("orcasm %s: %v", strings.Join(args, " "), err)
	}
	return stdout.String()
}

// orcaOverride makes orcasm run Orca version, or what the compatibility
// file picks when version is empty.
func orcaOverride(version string) []string {
	if version == "" {
		return nil
	}
	return []string{sandbox.OrcaVersionEnv + "=" + version}
}

// withoutOverride drops an Orca override set on the runner: the suite sets
// it per command.
func withoutOverride(env []string) []string {
	var out []string
	for _, kv := range env {
		if !strings.HasPrefix(kv, sandbox.OrcaVersionEnv+"=") {
			out = append(out, kv)
		}
	}
	return out
}

func writeAnswers(t *testing.T, path string, data map[string]any) {
	t.Helper()
	tmpl, err := template.ParseFiles("testdata/answers.toml")
	if err != nil {
		t.Fatal(err)
	}
	var b bytes.Buffer
	if err := tmpl.Execute(&b, data); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

// freePort is a host port free right now, for Orca's web UI.
func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

func copyDir(t *testing.T, src, dst string) {
	t.Helper()
	err := filepath.WalkDir(src, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		raw, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(target, raw, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
}

func run(t *testing.T, dir, name string, args ...string) {
	t.Helper()
	c := exec.Command(name, args...)
	c.Dir = dir
	if out, err := c.CombinedOutput(); err != nil {
		t.Fatalf("%s %v: %v\n%s", name, args, err, out)
	}
}

func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }
