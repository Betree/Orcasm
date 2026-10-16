package msb

import (
	"context"
	"encoding/json"
	"fmt"
)

// Status is a sandbox's state as `msb list --format json` reports it.
type Status string

// Sandbox statuses (msb 0.7.8).
const (
	Running Status = "Running"
	Stopped Status = "Stopped"
)

// Label is an msb sandbox label (`--label KEY=VALUE`).
type Label struct {
	Key, Value string
}

func (l Label) String() string { return l.Key + "=" + l.Value }

// Listing is one sandbox as `msb list` shows it.
type Listing struct {
	Name   string `json:"name"`
	Status Status `json:"status"`
}

// List returns the sandboxes carrying every given label (an empty Value only
// matches an empty value, so always give one). Without labels it lists every
// sandbox.
func (c CLI) List(ctx context.Context, labels ...Label) ([]Listing, error) {
	args := []string{"list", "--format", "json"}
	for _, l := range labels {
		args = append(args, "--label", l.String())
	}
	out, err := c.output(ctx, args...)
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
	out, err := c.output(ctx, "inspect", "--format", "json", name)
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

// Start boots a stopped sandbox. msb reads secret values from host variables
// at each start, so secrets gives the value of every secret the sandbox was
// created with. msb refuses a sandbox that is already running.
func (c CLI) Start(ctx context.Context, name string, secrets []Secret) error {
	return c.run(ctx, call{args: []string{"start", "--quiet", name}, secrets: secrets})
}

// Stop shuts a sandbox down, waiting for a graceful shutdown unless force
// kills it at once. Stopping a stopped sandbox succeeds.
func (c CLI) Stop(ctx context.Context, name string, force bool) error {
	args := []string{"stop", "--quiet", name}
	if force {
		args = append(args, "--force")
	}
	return c.run(ctx, call{args: args})
}

// Remove deletes a sandbox and its disks, stopping it first if needed.
func (c CLI) Remove(ctx context.Context, name string) error {
	return c.run(ctx, call{args: []string{"remove", "--quiet", "--force", name}})
}
