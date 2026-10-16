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

func TestUpgradeCanBypassTheCompatibilityFileWithLatest(t *testing.T) {
	_, err := run(t, "upgrade", "--latest", "a", "b")
	if err == nil || !strings.Contains(err.Error(), "accepts at most 1 arg") {
		t.Fatalf("err = %v, want --latest accepted (then an argument count error)", err)
	}
}
