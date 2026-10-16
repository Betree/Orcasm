package cli_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/Betree/Orcasm/internal/cli"
)

func run(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	root := cli.NewRootCommand(cli.BuildInfo{Version: "1.2.3", Commit: "abc1234", Date: "2026-10-16"})
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(args)
	err := root.Execute()
	return out.String(), err
}

func TestVersionFlagPrintsVersion(t *testing.T) {
	out, err := run(t, "--version")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out, "orcasm 1.2.3") {
		t.Fatalf("want output starting with %q, got %q", "orcasm 1.2.3", out)
	}
}
