package repository_test

import (
	"context"
	"testing"

	"github.com/Betree/Orcasm/internal/repository"
)

func TestAddSandboxRemotePointsAtSSHAlias(t *testing.T) {
	host := newHostRepo(t)

	if err := repository.AddSandboxRemote(context.Background(), host, "dev", "/home/orca/project"); err != nil {
		t.Fatal(err)
	}

	if got := git(t, host, "remote", "get-url", "orcasm-dev"); got != "orcasm-dev:/home/orca/project" {
		t.Errorf("url = %q", got)
	}
}

func TestAddSandboxRemoteIsIdempotent(t *testing.T) {
	host := newHostRepo(t)
	ctx := context.Background()

	if err := repository.AddSandboxRemote(ctx, host, "dev", "/old"); err != nil {
		t.Fatal(err)
	}
	if err := repository.AddSandboxRemote(ctx, host, "dev", "/home/orca/project"); err != nil {
		t.Fatal(err)
	}

	if got := git(t, host, "remote", "get-url", "orcasm-dev"); got != "orcasm-dev:/home/orca/project" {
		t.Errorf("url = %q", got)
	}
}

func TestRemoveSandboxRemoteDropsIt(t *testing.T) {
	host := newHostRepo(t)
	ctx := context.Background()
	if err := repository.AddSandboxRemote(ctx, host, "dev", "/home/dev/project"); err != nil {
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
