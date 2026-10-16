package msb

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// CLI runs the msb command line.
type CLI struct {
	// Home overrides MSB_HOME for msb and for the files orcasm reads from it.
	// Empty means msb's default ($MSB_HOME, else ~/.microsandbox).
	Home string
}

func (c CLI) home() (string, error) {
	if c.Home != "" {
		return c.Home, nil
	}
	if h := os.Getenv("MSB_HOME"); h != "" {
		return h, nil
	}
	u, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(u, ".microsandbox"), nil
}

func (c CLI) command(args ...string) *exec.Cmd {
	cmd := exec.Command("msb", args...)
	if c.Home != "" {
		cmd.Env = append(os.Environ(), "MSB_HOME="+c.Home)
	}
	return cmd
}

// AuthorizeSSHKey runs `msb ssh authorize --file`, adding the key to msb's
// global authorized_keys (every sandbox, any user). It is idempotent.
func (c CLI) AuthorizeSSHKey(pubKeyFile string) error {
	out, err := c.command("ssh", "authorize", "--file", pubKeyFile).CombinedOutput()
	if err != nil {
		return fmt.Errorf("msb ssh authorize: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// SandboxHostKey returns the sandbox's SSH host public key ("<type> <base64>"),
// derived from msb's per-sandbox host key file (an msb-internal path, stable
// across restarts and regenerated on recreate; see microsandbox.md §11).
func (c CLI) SandboxHostKey(name string) (string, error) {
	home, err := c.home()
	if err != nil {
		return "", err
	}
	path := filepath.Join(home, "sandboxes", name, "ssh", "host_ed25519")
	out, err := exec.Command("ssh-keygen", "-y", "-f", path).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("ssh-keygen -y -f %s: %w: %s", path, err, strings.TrimSpace(string(out)))
	}
	f := strings.Fields(string(out))
	if len(f) < 2 {
		return "", fmt.Errorf("unexpected public key from %s: %q", path, out)
	}
	return f[0] + " " + f[1], nil
}
