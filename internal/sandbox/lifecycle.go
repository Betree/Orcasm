// Package sandbox creates and manages Sandboxes: the msb sandboxes orcasm
// made, found through the labels it puts on them.
package sandbox

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/Betree/Orcasm/internal/msb"
)

// Labels orcasm puts on every Sandbox it creates (msb `--label KEY=VALUE`).
const (
	// LabelOrcasm marks a Sandbox as orcasm's. Its value is always "1": msb
	// matches a bare `--label orcasm` selector against an empty value only.
	LabelOrcasm = "orcasm"
	// LabelProject holds the Project on the host (absolute).
	LabelProject = "orcasm.project"
	// LabelMain holds Main's absolute path in the guest.
	LabelMain = "orcasm.main"
)

// CreateLabels returns the `--label` values to create a Sandbox with, for the
// Project at projectPath (absolute, on the host) whose Main lives at main in the
// guest.
func CreateLabels(projectPath, main string) []msb.Label {
	return []msb.Label{{Key: LabelOrcasm, Value: "1"}, {Key: LabelProject, Value: projectPath}, {Key: LabelMain, Value: main}}
}

// orcasmLabel selects the sandboxes orcasm made.
var orcasmLabel = msb.Label{Key: LabelOrcasm, Value: "1"}

// MSB is the part of microsandbox the lifecycle needs. msb.CLI implements it.
type MSB interface {
	msb.Sandboxes
	List(ctx context.Context, labels ...msb.Label) ([]msb.Listing, error)
	Labels(ctx context.Context, name string) (map[string]string, error)
	// Start boots a sandbox, valuing its secrets with secrets.
	Start(ctx context.Context, name string, secrets []msb.Secret) error
	Stop(ctx context.Context, name string, force bool) error
	Remove(ctx context.Context, name string) error
}

// Sandbox is one of orcasm's Sandboxes as the host sees it.
type Sandbox struct {
	Name        string
	Status      msb.Status
	ProjectPath string // the Project's absolute path on the host
	Main        string // Main's path in the guest
}

// Running reports whether the Sandbox is up.
func (s Sandbox) Running() bool { return s.Status == msb.Running }

// SSHEntries drops a Sandbox's host SSH entry. sshaccess.Access implements it.
type SSHEntries interface {
	Remove(name string) error
}

// Manager drives the lifecycle of Sandboxes.
type Manager struct {
	MSB MSB
	// SSH is only needed to remove Sandboxes.
	SSH SSHEntries
	// Labels is only needed to migrate Sandboxes (it records LabelSchema).
	Labels LabelSetter
	// Installer is only needed to upgrade Sandboxes: it installs in them.
	Installer GuestMSB
	// StartSecrets returns the named Sandbox's secrets with their values:
	// msb reads each secret's value from the host on every start, so the
	// values must be supplied again. Nil means none.
	StartSecrets func(ctx context.Context, name string) ([]msb.Secret, error)
	// ConfigDir holds the Configs Sandboxes were created from (SaveConfig);
	// Remove forgets them. Empty means none are kept.
	ConfigDir string
	// Poll is how often URL rereads Orca's log while waiting (default 1s).
	Poll time.Duration
}

// List returns every Sandbox, sorted by name.
func (m Manager) List(ctx context.Context) ([]Sandbox, error) {
	return m.list(ctx, orcasmLabel)
}

// InProject returns the Sandboxes of the Project at projectPath (absolute).
func (m Manager) InProject(ctx context.Context, projectPath string) ([]Sandbox, error) {
	return m.list(ctx, orcasmLabel, msb.Label{Key: LabelProject, Value: projectPath})
}

// Get returns the named Sandbox. A sandbox orcasm did not make is not one.
func (m Manager) Get(ctx context.Context, name string) (Sandbox, error) {
	all, err := m.List(ctx)
	if err != nil {
		return Sandbox{}, err
	}
	for _, s := range all {
		if s.Name == name {
			return s, nil
		}
	}
	return Sandbox{}, fmt.Errorf("no Sandbox named %q (see `orcasm ls`)", name)
}

// Start boots the named Sandbox; Orca's service starts with it. Starting a
// running Sandbox does nothing.
func (m Manager) Start(ctx context.Context, name string) error {
	s, err := m.Get(ctx, name)
	if err != nil || s.Running() {
		return err
	}
	var secrets []msb.Secret
	if m.StartSecrets != nil {
		if secrets, err = m.StartSecrets(ctx, name); err != nil {
			return fmt.Errorf("secrets for %q: %w", name, err)
		}
	}
	return m.MSB.Start(ctx, name, secrets)
}

// Stop shuts the named Sandbox down: gracefully, or at once with force.
func (m Manager) Stop(ctx context.Context, name string, force bool) error {
	if _, err := m.Get(ctx, name); err != nil {
		return err
	}
	return m.MSB.Stop(ctx, name, force)
}

func (m Manager) list(ctx context.Context, labels ...msb.Label) ([]Sandbox, error) {
	listed, err := m.MSB.List(ctx, labels...)
	if err != nil {
		return nil, err
	}
	out := make([]Sandbox, 0, len(listed))
	for _, l := range listed {
		labels, err := m.MSB.Labels(ctx, l.Name)
		if err != nil {
			return nil, err
		}
		out = append(out, Sandbox{Name: l.Name, Status: l.Status, ProjectPath: labels[LabelProject], Main: labels[LabelMain]})
	}
	slices.SortFunc(out, func(a, b Sandbox) int { return strings.Compare(a.Name, b.Name) })
	return out, nil
}
