package cli_test

import (
	"strings"
	"testing"
)

// The real host decides pass or fail, so this only asserts that every check
// is reported and that failures surface as a command error.
func TestDoctorReportsEveryCheck(t *testing.T) {
	out, err := run(t, "doctor")

	for _, name := range []string{"os", "kvm", "glibc", "msb", "port", "ssh"} {
		if !strings.Contains(out, name) {
			t.Errorf("doctor output missing %q check:\n%s", name, out)
		}
	}
	if failed := strings.Contains(out, "FAIL"); failed != (err != nil) {
		t.Fatalf("FAIL in output (%v) must match command error (%v):\n%s", failed, err, out)
	}
}
