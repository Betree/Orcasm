package selfupdate_test

import (
	"testing"

	"github.com/Betree/Orcasm/internal/selfupdate"
)

func TestNewerComparesReleaseTagsWithStampedVersions(t *testing.T) {
	for _, c := range []struct {
		current, latest string
		want            bool
	}{
		{"0.2.0", "v0.3.0", true},
		{"0.2.0", "v0.2.0", false},
		{"0.10.0", "v0.9.0", false},
		{"0.9.1", "v0.10.0", true},
		{"1.0.0", "v0.99.99", false},
		{"v0.2.0", "0.2.1", true},
	} {
		got, err := selfupdate.Newer(c.current, c.latest)
		if err != nil {
			t.Fatalf("Newer(%q, %q): %v", c.current, c.latest, err)
		}
		if got != c.want {
			t.Errorf("Newer(%q, %q) = %v, want %v", c.current, c.latest, got, c.want)
		}
	}
}

func TestNewerRejectsVersionsItCannotCompare(t *testing.T) {
	for _, current := range []string{"dev", "", "1.2"} {
		if _, err := selfupdate.Newer(current, "v0.3.0"); err == nil {
			t.Errorf("Newer(%q, v0.3.0): want an error", current)
		}
	}
}
