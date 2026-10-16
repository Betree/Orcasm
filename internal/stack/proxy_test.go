package stack_test

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Betree/Orcasm/internal/stack"
)

// TestRealProxyServesTheWorktreesWebServiceOnItsHostname starts a Stack
// whose web service publishes no port and fetches http://<worktree>.localhost
// through the Sandbox's proxy (sandbox.ProxyImage, started by Provision). Run
// it inside a provisioned Sandbox with ORCASM_TEST_PROXY=1.
func TestRealProxyServesTheWorktreesWebServiceOnItsHostname(t *testing.T) {
	if os.Getenv("ORCASM_TEST_PROXY") != "1" {
		t.Skip("set ORCASM_TEST_PROXY=1 in a provisioned Sandbox")
	}
	ctx := context.Background()
	checkout := t.TempDir()
	compose := `
services:
  web:
    image: nginx:1.27-alpine
    ports: ["8080:80"]
    command: ["sh", "-c", "echo hello-from-the-worktree > /usr/share/nginx/html/index.html && exec nginx -g 'daemon off;'"]
`
	if err := os.WriteFile(filepath.Join(checkout, "compose.yml"), []byte(compose), 0o644); err != nil {
		t.Fatal(err)
	}
	st, err := stack.Generate(ctx, stack.ExecDocker, stack.Spec{
		Worktree:     "Spike/Proxy",
		Checkouts:    []stack.Checkout{{Repository: "shop", Path: checkout}},
		ComposeFiles: []string{"compose.yml"},
		WebService:   "web",
		WebPort:      80,
		Home:         t.TempDir(),
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

	url := "http://" + stack.Hostname("Spike/Proxy") + "/"
	var status int
	var body string
	for deadline := time.Now().Add(30 * time.Second); time.Now().Before(deadline); time.Sleep(500 * time.Millisecond) {
		status, body = get(t, url)
		if status == 200 {
			break
		}
	}
	if status != 200 || strings.TrimSpace(body) != "hello-from-the-worktree" {
		t.Fatalf("GET %s = %d %q", url, status, body)
	}
	if s, _ := get(t, "http://no-such-worktree.localhost/"); s != 404 {
		t.Errorf("an unknown Worktree answered %d, want 404", s)
	}
}

// get fetches url from the loopback: Go, unlike curl and Chromium, does not
// resolve *.localhost by itself, so dial 127.0.0.1:80 with url's Host.
func get(t *testing.T, url string) (int, string) {
	t.Helper()
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.URL.Host = "127.0.0.1"
	req.Host = strings.TrimSuffix(strings.TrimPrefix(url, "http://"), "/")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, err.Error()
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}
