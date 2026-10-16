package version_test

import (
	"testing"

	"github.com/Betree/Orcasm/internal/version"
)

func TestReleasesCompareNumericallyPartByPart(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want int
	}{
		{"1.4.227", "1.4.230", -1},
		{"1.4.30", "1.4.227", -1},
		{"v1.5.0", "1.4.999", 1},
		{"1.4.227", "1.4.227", 0},
		{"1.4", "1.4.0", 0},
		{"0.10.0", "v0.9.0", 1},
	} {
		if got := version.Compare(c.a, c.b); got != c.want {
			t.Errorf("Compare(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestAPreReleaseComesBeforeItsRelease(t *testing.T) {
	// Semantic Versioning's order: 1.0.0-alpha < 1.0.0-alpha.1 <
	// 1.0.0-beta.2 < 1.0.0-beta.11 < 1.0.0-rc1 < 1.0.0.
	order := []string{"0.9.9", "1.0.0-alpha", "1.0.0-alpha.1", "1.0.0-beta.2", "1.0.0-beta.11", "1.0.0-rc1", "1.0.0", "v1.0.1-rc.1"}
	for i := range order {
		for j := range order {
			want := 0
			if i < j {
				want = -1
			} else if i > j {
				want = 1
			}
			if got := version.Compare(order[i], order[j]); got != want {
				t.Errorf("Compare(%q, %q) = %d, want %d", order[i], order[j], got, want)
			}
		}
	}
}

func TestBuildMetadataDoesNotOrderReleases(t *testing.T) {
	if got := version.Compare("1.2.3+linux", "1.2.3"); got != 0 {
		t.Errorf("Compare = %d, want 0", got)
	}
}

func TestOnlyDottedNumbersAreReleases(t *testing.T) {
	for v, want := range map[string]bool{
		"1.2.3": true, "v0.3.0": true, "1.0.0-rc1": true, "1.4": true,
		"dev": false, "": false, "1.x": false, "v": false, "1..2": false, "1.0.0-": false,
	} {
		if got := version.IsRelease(v); got != want {
			t.Errorf("IsRelease(%q) = %v, want %v", v, got, want)
		}
	}
}

func TestSomethingThatIsNotAReleaseIsOlderThanAnyRelease(t *testing.T) {
	if got := version.Compare("dev", "0.0.1"); got != -1 {
		t.Errorf("Compare(dev, 0.0.1) = %d, want -1", got)
	}
	if got := version.Compare("dev", "dev"); got != 0 {
		t.Errorf("Compare(dev, dev) = %d, want 0", got)
	}
}
