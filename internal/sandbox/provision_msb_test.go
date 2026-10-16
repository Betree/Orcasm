//go:build msb

package sandbox_test

// Provisions a real Sandbox (several minutes, needs network) and removes it:
//
//	ORCASM_TEST_PROVISION_SANDBOX=<throwaway name> ORCASM_TEST_PORT=<free host port> \
//	  go test -tags msb -timeout 20m ./internal/sandbox/

import (
	"context"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"

	"github.com/Betree/Orcasm/internal/msb"
	"github.com/Betree/Orcasm/internal/sandbox"
	"github.com/Betree/Orcasm/internal/wizard"
)

func TestProvisionARealSandbox(t *testing.T) {
	name := os.Getenv("ORCASM_TEST_PROVISION_SANDBOX")
	port, _ := strconv.Atoi(os.Getenv("ORCASM_TEST_PORT"))
	if name == "" || port == 0 {
		t.Skip("ORCASM_TEST_PROVISION_SANDBOX / ORCASM_TEST_PORT not set")
	}
	ctx := context.Background()
	t.Cleanup(func() { exec.Command("msb", "rm", "--force", name).Run() })
	cfg := wizard.Config{
		ProjectPath: "/home/someone/code/demo",
		Name:        name,
		Resources:   wizard.Resources{CPUs: 4, MemoryGiB: 8, RootDiskGiB: 32, DockerDiskGiB: 100},
		HostPort:    port,
		Harnesses:   []string{"claude", "codex", "opencode"},
		Secrets:     []wizard.Secret{{Name: "ORCASM_TEST_TOKEN", Hosts: []string{"example.com"}}},
		Git:         wizard.GitIdentity{Name: "Ada Lovelace", Email: "ada@example.com"},
		Project:     wizard.Project{ComposeFiles: []string{"compose.yml"}},
	}
	value := func(wizard.Secret) (string, error) { return "not-a-real-token", nil }

	if err := sandbox.Provision(ctx, msb.CLI{}, cfg, value, testWriter{t}); err != nil {
		t.Fatal(err)
	}

	resp, err := http.Get("http://127.0.0.1:" + strconv.Itoa(port) + "/web-index.html")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || !strings.Contains(string(body), "<title>Orca Web</title>") {
		t.Errorf("web UI on host port: %d %.200s", resp.StatusCode, body)
	}

	sb := msb.CLI{}.Sandbox(name)
	checks := `pwd; stat -c %U "$PWD"; id -nG | tr ' ' '\n' | grep -x docker
for c in git rsync curl gh claude codex opencode; do command -v $c >/dev/null || echo "missing $c"; done
docker run --rm hello-world >/dev/null && echo docker-ok
env -i PATH=/usr/bin:/bin curl -fsS -o /dev/null https://github.com && echo tls-ok
systemctl is-active orca; ps -o user= -C orca-ide | sort -u
git config user.name; echo "$ORCASM_TEST_TOKEN"
curl -sS -o /dev/null -w 'proxy %{http_code}\n' http://no-such-worktree.localhost/`
	out, err := sb.Exec(ctx, sandbox.User, "bash", "-lc", checks)
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	want := "/home/dev/demo\ndev\ndocker\ndocker-ok\ntls-ok\nactive\ndev\nAda Lovelace\n$MSB_ORCASM_TEST_TOKEN\nproxy 404\n"
	if out != want {
		t.Errorf("got\n%s\nwant\n%s", out, want)
	}

	// Main's Stack: nothing runs until `stack up`, which keeps published ports.
	stack := `set -e
cat >compose.yml <<'EOF'
services:
  web:
    image: nginx:alpine
    ports: ["8080:80"]
EOF
echo "before: $(docker ps -q --filter label=com.docker.compose.project | wc -l)"
stack up --wait >/dev/null 2>&1
curl -fsS -o /dev/null http://127.0.0.1:8080/ && echo web-ok
docker compose ls -q
stack down >/dev/null 2>&1
echo "after: $(docker ps -aq --filter label=com.docker.compose.project | wc -l)"`
	out, err = sb.Exec(ctx, sandbox.User, "bash", "-lc", stack)
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if want := "before: 0\nweb-ok\nmain\nafter: 0\n"; out != want {
		t.Errorf("stack: got\n%s\nwant\n%s", out, want)
	}
}

type testWriter struct{ t *testing.T }

func (w testWriter) Write(p []byte) (int, error) {
	w.t.Log(strings.TrimRight(string(p), "\n"))
	return len(p), nil
}
