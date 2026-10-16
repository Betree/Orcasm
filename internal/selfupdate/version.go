package selfupdate

import (
	"fmt"

	"github.com/Betree/Orcasm/internal/version"
)

// Newer reports whether release tag latest is newer than current (in
// internal/version's order, so a pre-release is older than its release).
// GoReleaser stamps the version without its "v" (.goreleaser.yaml ldflags)
// while tags carry one, so either form is accepted. A "dev" build has no
// version to compare, which is an error the caller turns into a `--force`
// hint.
func Newer(current, latest string) (bool, error) {
	for _, v := range []string{current, latest} {
		if !version.IsRelease(v) {
			return false, fmt.Errorf("cannot compare version %q: want a release number like 1.2.3", v)
		}
	}
	return version.Compare(latest, current) > 0, nil
}
