package msb

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// CLI runs the msb command line. Every msb call orcasm makes goes through
// its run method, so MSB_HOME and error reporting are handled once.
type CLI struct {
	// Home overrides MSB_HOME for msb and for the files orcasm reads from it.
	// Empty means msb's default ($MSB_HOME, else ~/.microsandbox).
	Home string
}

// Sandbox returns the named sandbox, driven by this CLI.
func (c CLI) Sandbox(name string) Sandbox { return Sandbox{Name: name, MSB: c} }

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

// call is one msb invocation.
type call struct {
	args []string
	// secrets are put in msb's environment (msb reads secret values from
	// host variables, never from argv).
	secrets []Secret
	// stdin feeds msb; nil means none.
	stdin io.Reader
	// stdout receives msb's output; nil discards it.
	stdout io.Writer
}

// run runs msb. A failure carries the last lines msb wrote to stderr.
func (c CLI) run(ctx context.Context, cl call) error {
	cmd := exec.CommandContext(ctx, "msb", cl.args...)
	var env []string
	for _, s := range cl.secrets {
		env = append(env, s.Name+"="+s.Value)
	}
	if c.Home != "" {
		env = append(env, "MSB_HOME="+c.Home)
	}
	if len(env) > 0 {
		cmd.Env = append(os.Environ(), env...)
	}
	var stderr bytes.Buffer
	cmd.Stdin, cmd.Stdout, cmd.Stderr = cl.stdin, cl.stdout, &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("msb %s: %w: %s", cl.args[0], err, lastLines(stderr.String(), 20))
	}
	return nil
}

// output runs msb and returns its stdout.
func (c CLI) output(ctx context.Context, args ...string) (string, error) {
	var out bytes.Buffer
	err := c.run(ctx, call{args: args, stdout: &out})
	return out.String(), err
}

func lastLines(s string, n int) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}
