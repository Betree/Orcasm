package selfupdate

import (
	"fmt"
	"strconv"
	"strings"
)

// Newer reports whether release tag latest is newer than current. GoReleaser
// stamps the version without its "v" (.goreleaser.yaml ldflags) while tags
// carry one, so either form is accepted. A "dev" build has no version to
// compare, which is an error the caller turns into a `--force` hint.
func Newer(current, latest string) (bool, error) {
	c, err := parse(current)
	if err != nil {
		return false, err
	}
	l, err := parse(latest)
	if err != nil {
		return false, err
	}
	for i := range c {
		if l[i] != c[i] {
			return l[i] > c[i], nil
		}
	}
	return false, nil
}

// parse reads "v1.2.3" or "1.2.3"; a pre-release suffix ("-rc1") is ignored.
func parse(v string) ([3]int, error) {
	var out [3]int
	core, _, _ := strings.Cut(strings.TrimPrefix(strings.TrimSpace(v), "v"), "-")
	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return out, fmt.Errorf("cannot compare version %q: want MAJOR.MINOR.PATCH", v)
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return out, fmt.Errorf("cannot compare version %q: want MAJOR.MINOR.PATCH", v)
		}
		out[i] = n
	}
	return out, nil
}
