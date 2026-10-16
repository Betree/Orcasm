package msb

import (
	"fmt"
	"os/exec"
	"strings"
)

// SandboxNames lists the names of every msb sandbox on the host.
func SandboxNames() ([]string, error) {
	out, err := exec.Command("msb", "list", "--quiet").Output()
	if err != nil {
		return nil, fmt.Errorf("msb list: %w", err)
	}
	return strings.Fields(string(out)), nil
}
