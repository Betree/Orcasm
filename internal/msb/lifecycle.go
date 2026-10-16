package msb

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// Sandbox statuses as `msb list --format json` reports them (0.7.8).
const (
	Running = "Running"
	Stopped = "Stopped"
)

// Listing is one sandbox as `msb list` shows it.
type Listing struct {
	Name   string `json:"name"`
	Status string `json:"status"`
}

// List returns the sandboxes carrying every given label ("KEY=VALUE"; a bare
// KEY only matches an empty value, so always give one).
func (c CLI) List(ctx context.Context, labels ...string) ([]Listing, error) {
	args := []string{"list", "--format", "json"}
	for _, l := range labels {
		args = append(args, "--label", l)
	}
	out, err := c.run(ctx, args...)
	if err != nil {
		return nil, err
	}
	var list []Listing
	if err := json.Unmarshal([]byte(out), &list); err != nil {
		return nil, fmt.Errorf("msb list: unexpected output: %w", err)
	}
	return list, nil
}

// Labels returns the sandbox's labels, image labels included.
func (c CLI) Labels(ctx context.Context, name string) (map[string]string, error) {
	out, err := c.run(ctx, "inspect", "--format", "json", name)
	if err != nil {
		return nil, err
	}
	var inspect struct {
		Config struct {
			Labels map[string]string `json:"labels"`
		} `json:"config"`
	}
	if err := json.Unmarshal([]byte(out), &inspect); err != nil {
		return nil, fmt.Errorf("msb inspect %s: unexpected output: %w", name, err)
	}
	return inspect.Config.Labels, nil
}

// Start boots a stopped sandbox, with env ("KEY=VALUE") added to msb's
// environment: msb reads secret values from host variables at each start.
// msb refuses a sandbox that is already running.
func (c CLI) Start(ctx context.Context, name string, env []string) error {
	_, err := c.runEnv(ctx, env, "start", "--quiet", name)
	return err
}

// Stop shuts a sandbox down, waiting for a graceful shutdown unless force
// kills it at once. Stopping a stopped sandbox succeeds.
func (c CLI) Stop(ctx context.Context, name string, force bool) error {
	args := []string{"stop", "--quiet", name}
	if force {
		args = append(args, "--force")
	}
	_, err := c.run(ctx, args...)
	return err
}

// Remove deletes a sandbox and its disks, stopping it first if needed.
func (c CLI) Remove(ctx context.Context, name string) error {
	_, err := c.run(ctx, "remove", "--quiet", "--force", name)
	return err
}

// Exec runs argv in the named sandbox; see Sandbox.Exec. On a stopped sandbox
// msb 0.7.8 still runs it, in a transient boot without systemd.
func (c CLI) Exec(ctx context.Context, name, user string, argv ...string) (string, error) {
	args := []string{"exec", "--quiet", "--no-stdin"}
	if user != "" {
		args = append(args, "--user", user)
	}
	return c.run(ctx, append(append(args, name, "--"), argv...)...)
}

func (c CLI) run(ctx context.Context, args ...string) (string, error) {
	return c.runEnv(ctx, nil, args...)
}

func (c CLI) runEnv(ctx context.Context, env []string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "msb", args...)
	if c.Home != "" {
		env = append(env, "MSB_HOME="+c.Home)
	}
	if len(env) > 0 {
		cmd.Env = append(os.Environ(), env...)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return stdout.String(), fmt.Errorf("msb %s: %w: %s", args[0], err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}
