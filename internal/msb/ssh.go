package msb

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// AuthorizeSSHKey runs `msb ssh authorize --file`, adding the key to msb's
// global authorized_keys (every sandbox, any user). It is idempotent.
func (c CLI) AuthorizeSSHKey(ctx context.Context, pubKeyFile string) error {
	return c.run(ctx, call{args: []string{"ssh", "authorize", "--file", pubKeyFile}})
}

// SandboxHostKey returns the sandbox's SSH host public key ("<type> <base64>"),
// derived from msb's per-sandbox host key file (an msb-internal path, stable
// across restarts and regenerated on recreate; see microsandbox.md §11).
//
// msb 0.7.8 only generates that key on the sandbox's first `msb ssh serve`,
// so a serve with no client is run first when it is missing (its handshake
// failure is expected).
func (c CLI) SandboxHostKey(ctx context.Context, name string) (string, error) {
	home, err := c.home()
	if err != nil {
		return "", err
	}
	path := filepath.Join(home, "sandboxes", name, "ssh", "host_ed25519")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		_ = c.run(ctx, call{args: []string{"ssh", "serve", name, "--stdio"}, stdin: strings.NewReader("")})
	}
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
