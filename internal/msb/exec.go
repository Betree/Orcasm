package msb

import (
	"bytes"
	"context"
	"io"
)

// Exec runs argv in the named sandbox as user ("" means root) with no stdin
// and returns its stdout. A non-zero exit becomes an error carrying stderr.
// On a stopped sandbox msb 0.7.8 still runs it, in a transient boot without
// systemd.
func (c CLI) Exec(ctx context.Context, name, user string, argv ...string) (string, error) {
	var out bytes.Buffer
	err := c.ExecStreaming(ctx, name, user, &out, argv...)
	return out.String(), err
}

// ExecStreaming is Exec writing argv's stdout to out as it comes, for long
// commands whose progress the user watches.
func (c CLI) ExecStreaming(ctx context.Context, name, user string, out io.Writer, argv ...string) error {
	args := []string{"exec", "--quiet", "--no-stdin"}
	if user != "" {
		args = append(args, "--user", user)
	}
	return c.run(ctx, call{args: append(append(args, name, "--"), argv...), stdout: out})
}

// CopyIn copies a host file to an absolute path in the named sandbox. The
// copy is owned by root with mode 0644.
func (c CLI) CopyIn(ctx context.Context, name, hostPath, guestPath string) error {
	return c.run(ctx, call{args: []string{"copy", "--quiet", hostPath, name + ":" + guestPath}})
}

// Sandboxes runs commands in, and copies files into, sandboxes by name. CLI
// implements it; tests fake it.
type Sandboxes interface {
	Exec(ctx context.Context, name, user string, argv ...string) (string, error)
	CopyIn(ctx context.Context, name, hostPath, guestPath string) error
}

// Sandbox is one sandbox, addressed by name: the shape packages working in a
// single sandbox (repository) take.
type Sandbox struct {
	Name string
	MSB  Sandboxes
}

// Exec runs argv in the sandbox; see CLI.Exec.
func (s Sandbox) Exec(ctx context.Context, user string, argv ...string) (string, error) {
	return s.MSB.Exec(ctx, s.Name, user, argv...)
}

// CopyIn copies a host file into the sandbox; see CLI.CopyIn.
func (s Sandbox) CopyIn(ctx context.Context, hostPath, guestPath string) error {
	return s.MSB.CopyIn(ctx, s.Name, hostPath, guestPath)
}
