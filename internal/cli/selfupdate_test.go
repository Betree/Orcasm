package cli_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/Betree/Orcasm/internal/cli"
)

func TestSelfUpdateRefusesADevelopmentBuildWithoutForce(t *testing.T) {
	var out bytes.Buffer
	root := cli.NewRootCommand(cli.BuildInfo{Version: "dev"})
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"self-update"})

	err := root.Execute()
	if err == nil || !strings.Contains(err.Error(), "--force") {
		t.Fatalf("want an error pointing at --force, got %v", err)
	}
}
