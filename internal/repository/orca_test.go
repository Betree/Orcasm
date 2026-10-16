package repository_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/Betree/Orcasm/internal/repository"
)

type call struct {
	user string
	argv []string
}

// recordingSandbox records Exec calls and answers them with err.
type recordingSandbox struct {
	calls []call
	err   error
}

func (r *recordingSandbox) Exec(_ context.Context, user string, argv ...string) (string, error) {
	r.calls = append(r.calls, call{user, argv})
	return "", r.err
}

func (r *recordingSandbox) CopyIn(context.Context, string, string) error { return nil }

func TestRegisterInOrcaAddsRepoAsOrcaUser(t *testing.T) {
	sb := &recordingSandbox{}

	if err := repository.RegisterInOrca(context.Background(), sb, "orca", "/home/orca/project"); err != nil {
		t.Fatal(err)
	}

	if len(sb.calls) != 1 {
		t.Fatalf("calls = %v", sb.calls)
	}
	c := sb.calls[0]
	if c.user != "orca" {
		t.Errorf("ran as %q, want orca", c.user)
	}
	if !slices.Equal(c.argv, []string{"orca-ide", "repo", "add", "--path", "/home/orca/project"}) {
		t.Errorf("argv = %q", c.argv)
	}
}

func TestRegisterInOrcaReportsFailure(t *testing.T) {
	sb := &recordingSandbox{err: errors.New("no running runtime")}

	err := repository.RegisterInOrca(context.Background(), sb, "orca", "/home/orca/project")

	if err == nil || !strings.Contains(err.Error(), "no running runtime") {
		t.Fatalf("err = %v", err)
	}
}
