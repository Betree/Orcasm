package wizard

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"

	"github.com/Betree/Orcasm/internal/hostport"
	"github.com/Betree/Orcasm/internal/msb"
)

// NewHost probes the machine orcasm runs on.
func NewHost() (Host, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return Host{}, err
	}
	mem, err := memoryGiB()
	if err != nil {
		return Host{}, fmt.Errorf("reading host memory: %w", err)
	}
	var harnesses []string
	for _, name := range SupportedHarnesses {
		if _, err := exec.LookPath(name); err == nil {
			harnesses = append(harnesses, name)
		}
	}
	return Host{
		Cwd:       cwd,
		CPUs:      runtime.NumCPU(),
		MemoryGiB: mem,
		Harnesses: harnesses,
		SandboxExists: func(name string) bool {
			// Without msb there are no Sandboxes; `orcasm doctor` reports it.
			names, _ := msb.SandboxNames()
			return slices.Contains(names, name)
		},
		FirstFreePort: hostport.FirstFree,
		Getenv:        os.Getenv,
		Output: func(name string, args ...string) (string, error) {
			out, err := exec.Command(name, args...).Output()
			return string(out), err
		},
		OpenCodeAuthFile: openCodeAuthFile(),
	}, nil
}

// openCodeAuthFile is where OpenCode keeps its credentials (XDG data dir on
// every OS), or "" without a home.
func openCodeAuthFile() string {
	data := os.Getenv("XDG_DATA_HOME")
	if data == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		data = filepath.Join(home, ".local", "share")
	}
	return filepath.Join(data, "opencode", "auth.json")
}

// memoryGiB is the host's total memory, rounded to the nearest GiB.
func memoryGiB() (int, error) {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) >= 2 && fields[0] == "MemTotal:" {
			kib, err := strconv.Atoi(fields[1])
			if err != nil {
				return 0, err
			}
			return (kib + 512*1024) / (1024 * 1024), nil
		}
	}
	return 0, fmt.Errorf("no MemTotal in /proc/meminfo")
}
