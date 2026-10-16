package msb

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// Sandbox is a running microsandbox sandbox, addressed by name. Exec and
// CopyIn drive `msb exec` / `msb copy` (validated against PinnedVersion).
type Sandbox struct {
	Name string
}

// Exec runs argv in the sandbox as user ("" means root) with no stdin and
// returns its stdout. A non-zero exit becomes an error carrying stderr.
func (s Sandbox) Exec(ctx context.Context, user string, argv ...string) (string, error) {
	args := []string{"exec", "--quiet", "--no-stdin"}
	if user != "" {
		args = append(args, "--user", user)
	}
	args = append(append(args, s.Name, "--"), argv...)
	return run(ctx, args...)
}

// CopyIn copies a host file to an absolute guest path. The copy is owned by
// root with mode 0644.
func (s Sandbox) CopyIn(ctx context.Context, hostPath, guestPath string) error {
	_, err := run(ctx, "copy", "--quiet", hostPath, s.Name+":"+guestPath)
	return err
}

func run(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "msb", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return stdout.String(), fmt.Errorf("msb %s: %w: %s", args[0], err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}
