package cli_test

import (
	"strings"
	"testing"
)

func TestUpgradeTakesAtMostOneSandbox(t *testing.T) {
	_, err := run(t, "upgrade", "a", "b")
	if err == nil || !strings.Contains(err.Error(), "accepts at most 1 arg") {
		t.Fatalf("err = %v, want an argument count error", err)
	}
}
