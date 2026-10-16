// Package version orders the release numbers orcasm compares: its own
// (GoReleaser stamps them without the tag's "v"), Orca's and msb's. They
// follow Semantic Versioning's precedence: dotted numbers compared part by
// part (a missing part counts as 0, so "1.4" is "1.4.0"), a pre-release
// ("1.0.0-rc1") before its release, and build metadata ("+linux") ignored.
// A leading "v" is ignored.
package version

import (
	"strconv"
	"strings"
)

// parsed is a release: its numbers and its pre-release identifiers (none
// for a release proper).
type parsed struct {
	nums []int
	pre  []string
}

func parse(v string) (parsed, bool) {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	v, _, _ = strings.Cut(v, "+")
	core, pre, hasPre := strings.Cut(v, "-")
	var p parsed
	for _, s := range strings.Split(core, ".") {
		n, err := strconv.Atoi(s)
		if err != nil || n < 0 {
			return parsed{}, false
		}
		p.nums = append(p.nums, n)
	}
	if hasPre {
		p.pre = strings.Split(pre, ".")
		for _, id := range p.pre {
			if id == "" {
				return parsed{}, false
			}
		}
	}
	return p, true
}

// IsRelease reports whether v is a release number, unlike a development
// build's "dev".
func IsRelease(v string) bool {
	_, ok := parse(v)
	return ok
}

// Compare orders two versions: -1 if a is older than b, 0 if they are the
// same release, 1 if a is newer. Something that is not a release number
// (IsRelease) is older than every release, and the same as another one.
func Compare(a, b string) int {
	pa, okA := parse(a)
	pb, okB := parse(b)
	switch {
	case !okA && !okB:
		return 0
	case !okA:
		return -1
	case !okB:
		return 1
	}
	for i := 0; i < max(len(pa.nums), len(pb.nums)); i++ {
		if c := cmpInt(at(pa.nums, i), at(pb.nums, i)); c != 0 {
			return c
		}
	}
	return comparePre(pa.pre, pb.pre)
}

// comparePre orders pre-releases of the same release (SemVer §11): none
// comes last; numeric identifiers compare as numbers and before
// alphanumeric ones, which compare as text; a longer list wins a tie.
func comparePre(a, b []string) int {
	switch {
	case len(a) == 0 && len(b) == 0:
		return 0
	case len(a) == 0:
		return 1
	case len(b) == 0:
		return -1
	}
	for i := 0; i < min(len(a), len(b)); i++ {
		x, errX := strconv.Atoi(a[i])
		y, errY := strconv.Atoi(b[i])
		var c int
		switch {
		case errX == nil && errY == nil:
			c = cmpInt(x, y)
		case errX == nil:
			c = -1
		case errY == nil:
			c = 1
		default:
			c = compareText(a[i], b[i])
		}
		if c != 0 {
			return c
		}
	}
	return cmpInt(len(a), len(b))
}

// compareText orders alphanumeric identifiers, with any run of digits in
// them compared as a number, so "rc10" follows "rc9" as people mean it.
func compareText(a, b string) int {
	for a != "" && b != "" {
		ra, na := leadingRun(a)
		rb, nb := leadingRun(b)
		var c int
		if na && nb {
			x, _ := strconv.Atoi(ra)
			y, _ := strconv.Atoi(rb)
			c = cmpInt(x, y)
		} else {
			c = strings.Compare(ra, rb)
		}
		if c != 0 {
			return c
		}
		a, b = a[len(ra):], b[len(rb):]
	}
	return cmpInt(len(a), len(b))
}

// leadingRun is s's leading run of digits, or of non-digits.
func leadingRun(s string) (run string, digits bool) {
	digits = s[0] >= '0' && s[0] <= '9'
	i := 1
	for i < len(s) && (s[i] >= '0' && s[i] <= '9') == digits {
		i++
	}
	return s[:i], digits
}

func at(nums []int, i int) int {
	if i < len(nums) {
		return nums[i]
	}
	return 0
}

func cmpInt(x, y int) int {
	switch {
	case x < y:
		return -1
	case x > y:
		return 1
	}
	return 0
}
