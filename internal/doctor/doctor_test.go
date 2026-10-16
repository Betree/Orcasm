package doctor_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/Betree/Orcasm/internal/doctor"
)

// fakeHost is a host where every probe succeeds unless a test overrides it.
type fakeHost struct {
	os       string
	kvmErr   error
	glibc    string
	glibcErr error
	msb      string
	msbErr   error
	missing  map[string]bool
	port     int
	portErr  error
}

func healthy() *fakeHost {
	return &fakeHost{os: "linux", glibc: "2.43", msb: "msb 0.7.8", port: 6768}
}

func (h *fakeHost) OS() string                    { return h.os }
func (h *fakeHost) OpenKVM() error                { return h.kvmErr }
func (h *fakeHost) GlibcVersion() (string, error) { return h.glibc, h.glibcErr }
func (h *fakeHost) MsbVersion() (string, error)   { return h.msb, h.msbErr }
func (h *fakeHost) FreePort(int) (int, error)     { return h.port, h.portErr }
func (h *fakeHost) LookPath(name string) (string, error) {
	if h.missing[name] {
		return "", errors.New("not found")
	}
	return "/usr/bin/" + name, nil
}

func check(t *testing.T, r doctor.Report, name string) doctor.Check {
	t.Helper()
	for _, c := range r.Checks {
		if c.Name == name {
			return c
		}
	}
	t.Fatalf("no %q check in report %+v", name, r.Checks)
	return doctor.Check{}
}

func TestHealthyHostPassesEveryCheck(t *testing.T) {
	r := doctor.Run(healthy())

	if !r.OK() {
		t.Fatalf("want healthy report, got %+v", r.Checks)
	}
	for _, name := range []string{"os", "kvm", "glibc", "msb", "port", "ssh"} {
		check(t, r, name)
	}
	if got := check(t, r, "port").Detail; !strings.Contains(got, "6768") {
		t.Fatalf("port detail should name the free port, got %q", got)
	}
}

func TestNonLinuxHostFailsOSCheck(t *testing.T) {
	h := healthy()
	h.os = "darwin"

	r := doctor.Run(h)

	if c := check(t, r, "os"); c.OK || !strings.Contains(c.Detail, "Linux") {
		t.Fatalf("want failing os check mentioning Linux, got %+v", c)
	}
	if r.OK() {
		t.Fatal("report should not be OK")
	}
}

func TestGlibcMustBeAtLeast228(t *testing.T) {
	for version, wantOK := range map[string]bool{"2.27": false, "2.28": true, "2.9": false, "2.43": true, "3.0": true} {
		h := healthy()
		h.glibc = version
		if c := check(t, doctor.Run(h), "glibc"); c.OK != wantOK {
			t.Errorf("glibc %s: want OK=%v, got %+v", version, wantOK, c)
		}
	}
}

func TestHostWithoutGlibcFails(t *testing.T) {
	h := healthy()
	h.glibcErr = errors.New("getconf: GNU_LIBC_VERSION: unknown variable")

	if c := check(t, doctor.Run(h), "glibc"); c.OK || !strings.Contains(c.Detail, "musl") {
		t.Fatalf("want failing glibc check mentioning musl, got %+v", c)
	}
}

func TestMissingMsbFailsWithInstallHint(t *testing.T) {
	h := healthy()
	h.msbErr = errors.New(`exec: "msb": executable file not found in $PATH`)

	if c := check(t, doctor.Run(h), "msb"); c.OK || !strings.Contains(c.Detail, "install.microsandbox.dev") {
		t.Fatalf("want failing msb check with install hint, got %+v", c)
	}
}

func TestMsbMustBeThePinnedVersion(t *testing.T) {
	h := healthy()
	h.msb = "msb 0.7.7"

	c := check(t, doctor.Run(h), "msb")

	if c.OK || !strings.Contains(c.Detail, "0.7.7") || !strings.Contains(c.Detail, "0.7.8") {
		t.Fatalf("want failing msb check naming found and pinned versions, got %+v", c)
	}
}

func TestNoFreePortFails(t *testing.T) {
	h := healthy()
	h.portErr = errors.New("no free port on 127.0.0.1 from 6768")

	if c := check(t, doctor.Run(h), "port"); c.OK {
		t.Fatalf("want failing port check, got %+v", c)
	}
}

func TestMissingSSHClientFails(t *testing.T) {
	h := healthy()
	h.missing = map[string]bool{"ssh": true}

	if c := check(t, doctor.Run(h), "ssh"); c.OK || !strings.Contains(c.Detail, "OpenSSH") {
		t.Fatalf("want failing ssh check with OpenSSH hint, got %+v", c)
	}
}

func TestInaccessibleKVMFailsWithKVMGroupHint(t *testing.T) {
	h := healthy()
	h.kvmErr = errors.New("open /dev/kvm: permission denied")

	c := check(t, doctor.Run(h), "kvm")

	if c.OK || !strings.Contains(c.Detail, "permission denied") || !strings.Contains(c.Detail, "kvm group") {
		t.Fatalf("want failing kvm check with cause and kvm group hint, got %+v", c)
	}
}
