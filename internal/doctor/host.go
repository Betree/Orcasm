package doctor

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/Betree/Orcasm/internal/hostport"
)

// RealHost probes the host orcasm runs on.
type RealHost struct{}

func (RealHost) OS() string { return runtime.GOOS }

func (RealHost) OpenKVM() error {
	f, err := os.OpenFile("/dev/kvm", os.O_RDWR, 0)
	if err != nil {
		return err
	}
	return f.Close()
}

// GlibcVersion asks getconf, which only knows GNU_LIBC_VERSION on glibc hosts.
func (RealHost) GlibcVersion() (string, error) {
	out, err := exec.Command("getconf", "GNU_LIBC_VERSION").Output()
	if err != nil {
		return "", fmt.Errorf("getconf GNU_LIBC_VERSION: %w", err)
	}
	return strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(string(out)), "glibc")), nil
}

func (RealHost) MsbVersion() (string, error) {
	out, err := exec.Command("msb", "--version").Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func (RealHost) LookPath(name string) (string, error) { return exec.LookPath(name) }

func (RealHost) FreePort(start int) (int, error) { return hostport.FirstFree(start) }
