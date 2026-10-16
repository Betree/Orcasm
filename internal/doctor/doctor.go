// Package doctor checks whether the host can run a Sandbox.
package doctor

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/Betree/Orcasm/internal/hostport"
	"github.com/Betree/Orcasm/internal/msb"
)

// Host is what doctor probes. RealHost probes the host it runs on.
type Host interface {
	OS() string
	OpenKVM() error
	GlibcVersion() (string, error)
	MsbVersion() (string, error)
	LookPath(name string) (string, error)
	FreePort(start int) (int, error)
}

// Check is the outcome of one readiness check.
type Check struct {
	Name   string
	OK     bool
	Detail string
}

// Report is the outcome of every check, in a stable order.
type Report struct {
	Checks []Check
}

// OK reports whether every check passed.
func (r Report) OK() bool {
	for _, c := range r.Checks {
		if !c.OK {
			return false
		}
	}
	return true
}

// Run probes the host and reports on each check.
func Run(h Host) Report {
	return Report{Checks: []Check{
		checkOS(h),
		checkKVM(h),
		checkGlibc(h),
		checkMsb(h),
		checkPort(h),
		checkSSH(h),
	}}
}

func pass(name, detail string) Check { return Check{Name: name, OK: true, Detail: detail} }

func fail(name, detail string) Check { return Check{Name: name, Detail: detail} }

func checkOS(h Host) Check {
	if os := h.OS(); os != "linux" {
		return fail("os", fmt.Sprintf("orcasm supports Linux hosts only, this host is %s", os))
	}
	return pass("os", "linux")
}

func checkKVM(h Host) Check {
	if err := h.OpenKVM(); err != nil {
		return fail("kvm", fmt.Sprintf("%v; add your user to the kvm group (sudo usermod -aG kvm $USER) and log in again", err))
	}
	return pass("kvm", "/dev/kvm is readable and writable")
}

// minGlibc is the oldest glibc microsandbox supports.
var minGlibc = [2]int{2, 28}

func checkGlibc(h Host) Check {
	v, err := h.GlibcVersion()
	if err != nil {
		return fail("glibc", fmt.Sprintf("cannot find glibc (%v); microsandbox does not support musl-only hosts", err))
	}
	major, minor, ok := parseMajorMinor(v)
	if !ok {
		return fail("glibc", fmt.Sprintf("cannot parse glibc version %q", v))
	}
	if major < minGlibc[0] || (major == minGlibc[0] && minor < minGlibc[1]) {
		return fail("glibc", fmt.Sprintf("glibc %s is too old; microsandbox needs %d.%d or newer", v, minGlibc[0], minGlibc[1]))
	}
	return pass("glibc", "glibc "+v)
}

func parseMajorMinor(v string) (major, minor int, ok bool) {
	a, b, found := strings.Cut(strings.TrimSpace(v), ".")
	if !found {
		return 0, 0, false
	}
	b, _, _ = strings.Cut(b, ".")
	major, err1 := strconv.Atoi(a)
	minor, err2 := strconv.Atoi(b)
	return major, minor, err1 == nil && err2 == nil
}

func checkMsb(h Host) Check {
	install := fmt.Sprintf("install msb %s with: curl -fsSL https://install.microsandbox.dev | sh", msb.PinnedVersion)
	out, err := h.MsbVersion()
	if err != nil {
		return fail("msb", fmt.Sprintf("msb not found (%v); %s", err, install))
	}
	// `msb --version` prints "msb 0.7.8".
	v := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(out), "msb"))
	if v != msb.PinnedVersion {
		return fail("msb", fmt.Sprintf("found msb %s, orcasm needs msb %s; %s", v, msb.PinnedVersion, install))
	}
	return pass("msb", "msb "+v)
}

func checkPort(h Host) Check {
	p, err := h.FreePort(hostport.OrcaWebUI)
	if err != nil {
		return fail("port", fmt.Sprintf("no free host port for the Orca web UI: %v", err))
	}
	return pass("port", fmt.Sprintf("%d is free for the Orca web UI", p))
}

func checkSSH(h Host) Check {
	p, err := h.LookPath("ssh")
	if err != nil {
		return fail("ssh", "ssh client not found; install OpenSSH (e.g. openssh-clients or openssh-client)")
	}
	return pass("ssh", p)
}
