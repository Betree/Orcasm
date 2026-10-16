package selfupdate_test

import (
	"testing"

	"github.com/Betree/Orcasm/internal/selfupdate"
)

func TestHomebrewRecognisesBinariesBrewOwns(t *testing.T) {
	for _, p := range []string{
		"/opt/homebrew/Caskroom/orcasm/0.2.0/orcasm",
		"/usr/local/Caskroom/orcasm/0.2.0/orcasm",
		"/home/linuxbrew/.linuxbrew/Caskroom/orcasm/0.2.0/orcasm",
		"/home/linuxbrew/.linuxbrew/Cellar/microsandbox/0.7.8/bin/msb",
	} {
		if !selfupdate.Homebrew(p) {
			t.Errorf("Homebrew(%q) = false, want true", p)
		}
	}
}

func TestHomebrewLeavesOtherInstallsToSelfUpdate(t *testing.T) {
	for _, p := range []string{
		"/usr/local/bin/orcasm",
		"/home/dev/.local/bin/orcasm",
		"/home/dev/.microsandbox/bin/msb",
		"/home/dev/src/Caskroomish/orcasm",
	} {
		if selfupdate.Homebrew(p) {
			t.Errorf("Homebrew(%q) = true, want false", p)
		}
	}
}
