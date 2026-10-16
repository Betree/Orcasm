package repository_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Betree/Orcasm/internal/repository"
)

func TestAddSandboxRemotePointsAtSSHAlias(t *testing.T) {
	host := newHostRepo(t)

	if err := repository.AddSandboxRemote(context.Background(), host, "dev", "/home/orca/project", "ssh"); err != nil {
		t.Fatal(err)
	}

	if got := git(t, host, "remote", "get-url", "orcasm-dev"); got != "orcasm-dev:/home/orca/project" {
		t.Errorf("url = %q", got)
	}
}

func TestAddSandboxRemoteIsIdempotent(t *testing.T) {
	host := newHostRepo(t)
	ctx := context.Background()

	if err := repository.AddSandboxRemote(ctx, host, "dev", "/old", "ssh"); err != nil {
		t.Fatal(err)
	}
	if err := repository.AddSandboxRemote(ctx, host, "dev", "/home/orca/project", "ssh"); err != nil {
		t.Fatal(err)
	}

	if got := git(t, host, "remote", "get-url", "orcasm-dev"); got != "orcasm-dev:/home/orca/project" {
		t.Errorf("url = %q", got)
	}
}

func TestRemoveSandboxRemoteDropsIt(t *testing.T) {
	host := newHostRepo(t)
	ctx := context.Background()
	if err := repository.AddSandboxRemote(ctx, host, "dev", "/home/dev/project", "ssh"); err != nil {
		t.Fatal(err)
	}

	if err := repository.RemoveSandboxRemote(ctx, host, "dev"); err != nil {
		t.Fatal(err)
	}

	if got := git(t, host, "remote"); got != "" {
		t.Errorf("remotes left: %q", got)
	}
}

func TestRemoveSandboxRemoteWithoutRemoteIsNoop(t *testing.T) {
	if err := repository.RemoveSandboxRemote(context.Background(), newHostRepo(t), "dev"); err != nil {
		t.Fatal(err)
	}
}

// fakeSSH writes an ssh stand-in that records the host it was asked for in
// log and runs the remote command locally, as if the guest were the host.
func fakeSSH(t *testing.T) (command, log string) {
	t.Helper()
	dir := t.TempDir()
	log = filepath.Join(dir, "hosts")
	script := filepath.Join(dir, "ssh")
	write(t, script, "#!/bin/sh\nfor a; do host=$cmd; cmd=$a; done\necho \"$host\" >> '"+log+"'\nexec sh -c \"$cmd\"\n")
	if err := os.Chmod(script, 0o755); err != nil {
		t.Fatal(err)
	}
	return script, log
}

// The remote must work whether or not the user let orcasm include its ssh
// config from ~/.ssh/config.
func TestFetchingFromTheSandboxRemoteGoesThroughOrcasmsSSHCommand(t *testing.T) {
	host := newHostRepo(t)
	guest := filepath.Join(t.TempDir(), "main")
	git(t, host, "clone", "-q", host, guest)
	git(t, guest, "checkout", "-q", "-b", "agent-work")
	git(t, guest, "commit", "-q", "--allow-empty", "-m", "from the Sandbox")
	ssh, log := fakeSSH(t)

	if err := repository.AddSandboxRemote(context.Background(), host, "dev", guest, ssh); err != nil {
		t.Fatal(err)
	}
	git(t, host, "fetch", "-q", "orcasm-dev")

	if got := git(t, host, "log", "-1", "--format=%s", "orcasm-dev/agent-work"); got != "from the Sandbox" {
		t.Errorf("fetched %q", got)
	}
	if hosts, _ := os.ReadFile(log); strings.TrimSpace(string(hosts)) != "orcasm-dev" {
		t.Errorf("ssh asked for %q, want orcasm-dev", hosts)
	}
}

func TestAddSandboxRemoteKeepsTheUsersOwnSSHCommand(t *testing.T) {
	host := newHostRepo(t)
	git(t, host, "config", "core.sshCommand", "ssh -i ~/.ssh/work")

	err := repository.AddSandboxRemote(context.Background(), host, "dev", "/home/dev/app", "ssh -F /orcasm/config")
	if err == nil || !strings.Contains(err.Error(), "core.sshCommand") {
		t.Fatalf("want an error explaining the remote needs orcasm's ssh config, got %v", err)
	}

	if got := git(t, host, "config", "core.sshCommand"); got != "ssh -i ~/.ssh/work" {
		t.Errorf("core.sshCommand = %q", got)
	}
	if got := git(t, host, "remote", "get-url", "orcasm-dev"); got != "orcasm-dev:/home/dev/app" {
		t.Errorf("remote still wanted, got %q", got)
	}
}

func TestRemovingTheLastSandboxRemoteDropsOrcasmsSSHCommand(t *testing.T) {
	host := newHostRepo(t)
	ctx := context.Background()
	for _, sb := range []string{"one", "two"} {
		if err := repository.AddSandboxRemote(ctx, host, sb, "/home/dev/app", "ssh -F /orcasm/config"); err != nil {
			t.Fatal(err)
		}
	}

	if err := repository.RemoveSandboxRemote(ctx, host, "one"); err != nil {
		t.Fatal(err)
	}
	if got := git(t, host, "config", "core.sshCommand"); got != "ssh -F /orcasm/config" {
		t.Errorf("with orcasm-two left, core.sshCommand = %q", got)
	}
	if err := repository.RemoveSandboxRemote(ctx, host, "two"); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("git", "-C", host, "config", "--local", "core.sshCommand").Output(); err == nil {
		t.Errorf("core.sshCommand left behind: %q", out)
	}
}
