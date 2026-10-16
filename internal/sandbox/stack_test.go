package sandbox_test

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Betree/Orcasm/internal/sandbox"
)

// runStack runs the in-Sandbox `stack` command with Main's Stack configured
// as Provision leaves it, and a fake docker that prints its arguments.
func runStack(t *testing.T, main string, composeFiles []string, args ...string) (string, error) {
	t.Helper()
	return runStackWith(t, "#!/bin/sh\nprintf '[%s]' \"$@\"; echo\n", main, composeFiles, args...)
}

// runStackWith is runStack with docker being the given script.
func runStackWith(t *testing.T, docker, main string, composeFiles []string, args ...string) (string, error) {
	t.Helper()
	dir := t.TempDir()
	conf := filepath.Join(dir, "etc")
	bin := filepath.Join(dir, "bin")
	for _, d := range []string{conf, bin} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	files := ""
	for _, f := range composeFiles {
		files += f + "\n"
	}
	write(t, filepath.Join(conf, "main"), main+"\n", 0o644)
	write(t, filepath.Join(conf, "main-compose-files"), files, 0o644)
	write(t, filepath.Join(bin, "docker"), docker, 0o755)
	script := filepath.Join(dir, "stack")
	write(t, script, sandbox.StackCommand(), 0o755)

	cmd := exec.Command(script, args...)
	cmd.Env = append(os.Environ(), "ORCASM_CONF_DIR="+conf, "PATH="+bin+":"+os.Getenv("PATH"))
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func write(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
}

func TestStackUpStartsMainsStackDetachedFromTheProjectsComposeFiles(t *testing.T) {
	out, err := runStack(t, "/home/dev/demo", []string{"compose.yml", "api/compose.dev.yml"}, "up")
	if err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	want := "[compose][-p][main][-f][/home/dev/demo/compose.yml][-f][/home/dev/demo/api/compose.dev.yml][up][-d]\n"
	if out != want {
		t.Errorf("docker got %q, want %q", out, want)
	}
}

// composePublishing is a docker whose `compose config` shows the web
// service published on host port published; other calls print their args.
func composePublishing(published string) string {
	return `#!/bin/sh
for a in "$@"; do
  if [ "$a" = config ]; then
    printf 'name: main\nservices:\n  web:\n    image: nginx\n    ports:\n      - mode: ingress\n        target: 80\n        published: "%s"\n        protocol: tcp\n' ` + published + `
    exit 0
  fi
done
printf '[%s]' "$@"; echo
`
}

// The Worktree proxy serves http://<worktree>.localhost on the Sandbox's
// port 80, so Main's Stack cannot publish that port too.
func TestStackUpRefusesToPublishThePortOfTheWorktreeProxy(t *testing.T) {
	out, err := runStackWith(t, composePublishing("80"), "/home/dev/demo", []string{"compose.yml"}, "up")
	if err == nil || strings.Contains(out, "[up]") {
		t.Fatalf("want a refusal without starting the Stack, got %v: %q", err, out)
	}
	for _, want := range []string{"port 80", "proxy"} {
		if !strings.Contains(out, want) {
			t.Errorf("message %q does not mention %q", out, want)
		}
	}
}

func TestStackUpStartsAStackPublishingOtherPorts(t *testing.T) {
	out, err := runStackWith(t, composePublishing("8080"), "/home/dev/demo", []string{"compose.yml"}, "up")
	if err != nil || !strings.Contains(out, "[up][-d]") {
		t.Errorf("want the Stack started, got %v: %q", err, out)
	}
}

func TestStackDownStopsMainsStack(t *testing.T) {
	out, err := runStack(t, "/home/dev/demo", []string{"compose.yml"}, "down")
	if err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	want := "[compose][-p][main][-f][/home/dev/demo/compose.yml][down]\n"
	if out != want {
		t.Errorf("docker got %q, want %q", out, want)
	}
}

func TestStackPassesOtherComposeCommandsThrough(t *testing.T) {
	out, err := runStack(t, "/home/dev/my demo", []string{"compose.yml"}, "logs", "-f", "web")
	if err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	want := "[compose][-p][main][-f][/home/dev/my demo/compose.yml][logs][-f][web]\n"
	if out != want {
		t.Errorf("docker got %q, want %q", out, want)
	}
}

func TestStackRefusesWhenTheProjectHasNoComposeFiles(t *testing.T) {
	out, err := runStack(t, "/home/dev/demo", nil, "up")
	if err == nil || !strings.Contains(out, "no Compose files") || strings.Contains(out, "[compose]") {
		t.Errorf("want a refusal without running docker, got %v: %q", err, out)
	}
}

func TestStackWithoutACommandPrintsUsage(t *testing.T) {
	out, err := runStack(t, "/home/dev/demo", []string{"compose.yml"})
	if err == nil || !strings.Contains(out, "stack up") || strings.Contains(out, "[compose]") {
		t.Errorf("want usage and a failure, got %v: %q", err, out)
	}
}

// Provision installs `stack` in the Sandbox: replaying its root exec on a
// scratch root gives a working command for Main's Stack.
func TestProvisionInstallsTheStackCommandForMain(t *testing.T) {
	f := &fakeCreateMSB{}
	cfg := demoConfig()
	cfg.Project.ComposeFiles = []string{"docker-compose.yml", "ops/docker-compose.dev.yml"}
	if err := sandbox.Provision(context.Background(), f, cfg, sandbox.OrcaVersion, hostValues, io.Discard); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	installed := false
	for _, c := range f.execs {
		if c.user != "" || len(c.argv) < 4 || c.argv[0] != "bash" || !slices.Contains(c.argv, "/usr/local/bin/stack") {
			continue
		}
		cmd := exec.Command(c.argv[0], c.argv[1:]...)
		cmd.Env = append(os.Environ(), "ORCASM_ROOT="+root)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("install: %v: %s", err, out)
		}
		installed = true
	}
	if !installed {
		t.Fatalf("no root exec installs stack: %+v", f.execs)
	}

	bin := t.TempDir()
	write(t, filepath.Join(bin, "docker"), "#!/bin/sh\nprintf '[%s]' \"$@\"; echo\n", 0o755)
	cmd := exec.Command(filepath.Join(root, "usr/local/bin/stack"), "up")
	cmd.Env = append(os.Environ(), "ORCASM_CONF_DIR="+filepath.Join(root, "etc/orcasm"), "PATH="+bin+":"+os.Getenv("PATH"))
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	want := "[compose][-p][main][-f][/home/dev/demo/docker-compose.yml][-f][/home/dev/demo/ops/docker-compose.dev.yml][up][-d]\n"
	if string(out) != want {
		t.Errorf("docker got %q, want %q", out, want)
	}
}
