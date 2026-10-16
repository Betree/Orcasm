package orca_test

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Betree/Orcasm/internal/orca"
)

// fakeRuntime serves Orca's runtime socket from userData the way `orca-ide
// serve` does, answering one request with reply.
func fakeRuntime(t *testing.T, reply string) (userData string, requests chan map[string]any) {
	t.Helper()
	// Unix socket paths are short: keep it out of t.TempDir's long path.
	userData, err := os.MkdirTemp("", "orca")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(userData) })
	sock := filepath.Join(userData, "o-1.sock")
	l, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	meta := `{"runtimeId":"r","pid":1,"transports":[{"kind":"unix","endpoint":"` + sock +
		`"},{"kind":"websocket","endpoint":"ws://0.0.0.0:6768"}],"authToken":"tok"}`
	if err := os.WriteFile(filepath.Join(userData, "orca-runtime.json"), []byte(meta), 0o600); err != nil {
		t.Fatal(err)
	}
	requests = make(chan map[string]any, 1)
	go func() {
		c, err := l.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		line, _ := bufio.NewReader(c).ReadString('\n')
		var req map[string]any
		json.Unmarshal([]byte(line), &req)
		requests <- req
		c.Write([]byte(strings.ReplaceAll(reply, "ID", req["id"].(string)) + "\n"))
	}()
	return userData, requests
}

func TestUpdateRepoSendsTheUpdatesToTheRuntime(t *testing.T) {
	userData, requests := fakeRuntime(t, `{"id":"ID","ok":true,"result":{"repo":{}}}`)
	hooks := orca.HookSettings{
		Mode:           "auto",
		SetupRunPolicy: "run-by-default",
		Scripts:        orca.Scripts{Setup: "orcasm worktree setup", Archive: "orcasm worktree archive"},
	}
	err := orca.Runtime{UserData: userData}.UpdateRepo(context.Background(), "/home/dev/shop",
		orca.RepoUpdates{HookSettings: &hooks, WorktreeBasePath: "/worktrees"})
	if err != nil {
		t.Fatal(err)
	}
	req := <-requests
	if req["authToken"] != "tok" || req["method"] != "repo.update" {
		t.Errorf("request %v", req)
	}
	got, _ := json.Marshal(req["params"])
	want := `{"repo":"path:/home/dev/shop","updates":{"hookSettings":{"mode":"auto","scripts":{"archive":"orcasm worktree archive","setup":"orcasm worktree setup"},"setupRunPolicy":"run-by-default"},"worktreeBasePath":"/worktrees"}}`
	if string(got) != want {
		t.Errorf("params\n%s\nwant\n%s", got, want)
	}
}

func TestUpdateRepoReportsTheRuntimesError(t *testing.T) {
	userData, _ := fakeRuntime(t, `{"id":"ID","ok":false,"error":{"code":"repo_not_found","message":"Repo not found"}}`)
	err := orca.Runtime{UserData: userData}.UpdateRepo(context.Background(), "/nope", orca.RepoUpdates{WorktreeBasePath: "/w"})
	if err == nil || !strings.Contains(err.Error(), "Repo not found") {
		t.Errorf("err = %v", err)
	}
}
