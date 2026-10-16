package sandbox

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/Betree/Orcasm/internal/guest"
	"github.com/Betree/Orcasm/internal/msb"
	"github.com/Betree/Orcasm/internal/repository"
	"github.com/Betree/Orcasm/internal/wizard"
)

// SSHAccess is the host's SSH access to Sandboxes. sshaccess.Access
// implements it.
type SSHAccess interface {
	// Add makes the Sandbox reachable as orcasm-<name> through orcasm's own
	// ssh config.
	Add(ctx context.Context, name string) error
	// LinkUserConfig includes orcasm's ssh config from ~/.ssh/config. Only
	// with the user's consent.
	LinkUserConfig() error
	// GitSSHCommand is the ssh command git uses to reach orcasm-<name>.
	GitSSHCommand() string
}

// Creator creates a Sandbox from a Wizard Config, end to end. NewCreator
// builds the real one; each step is a field so tests can stand in for msb.
type Creator struct {
	// Provision creates the Sandbox and installs everything in it.
	Provision func(ctx context.Context, cfg wizard.Config, out io.Writer) error
	// InstallOrcasm puts orcasm and the Project's Worktree config in the
	// Sandbox, for Orca's repo hooks (InstallOrcasm).
	InstallOrcasm func(ctx context.Context, cfg wizard.Config, out io.Writer) error
	// Copy copies the Project into Sandbox name.
	Copy func(ctx context.Context, name string, opts repository.CopyOptions) error
	SSH  SSHAccess
	// AddRemote adds the host git remote orcasm-<name> (repository.AddSandboxRemote).
	AddRemote func(ctx context.Context, hostRepo, name, guestPath, sshCommand string) error
	// RegisterInOrca makes guestPath an Orca project in Sandbox name. It
	// fails until Orca's runtime serves.
	RegisterInOrca func(ctx context.Context, name, guestPath string) error
	// SetHooks stores orcasm's setup and archive hooks in Orca's settings of
	// the project at guestPath (SetOrcaHooks).
	SetHooks func(ctx context.Context, name, guestPath string) error
	// ConfigDir keeps each Sandbox's Config (SaveConfig).
	ConfigDir string
	// OrcaVersion is the Orca release Provision installs, recorded with
	// SaveInstalled.
	OrcaVersion string
	// OrcaWait bounds how long registration waits for Orca's runtime, trying
	// every Poll.
	OrcaWait, Poll time.Duration
}

// NewCreator returns the Creator driving msb through m. guestBinary is the
// orcasm installed in the Sandbox (GuestBinary), orcaVersion the Orca
// release (DesiredOrca).
func NewCreator(m msb.CLI, ssh SSHAccess, secretValue SecretValue, configDir, guestBinary, orcaVersion string) Creator {
	return Creator{
		Provision: func(ctx context.Context, cfg wizard.Config, out io.Writer) error {
			return Provision(ctx, m, cfg, orcaVersion, secretValue, out)
		},
		InstallOrcasm: func(ctx context.Context, cfg wizard.Config, out io.Writer) error {
			return InstallOrcasm(ctx, m, cfg, guestBinary, out)
		},
		Copy: func(ctx context.Context, name string, opts repository.CopyOptions) error {
			return repository.Copy(ctx, m.Sandbox(name), opts)
		},
		SSH:       ssh,
		AddRemote: repository.AddSandboxRemote,
		RegisterInOrca: func(ctx context.Context, name, guestPath string) error {
			return repository.RegisterInOrca(ctx, m.Sandbox(name), guest.User, guestPath)
		},
		SetHooks: func(ctx context.Context, name, guestPath string) error {
			_, err := m.Exec(ctx, name, guest.User, guest.Binary, "worktree", "install-hooks", guestPath)
			return err
		},
		ConfigDir:   configDir,
		OrcaVersion: orcaVersion,
		OrcaWait:    2 * time.Minute,
		Poll:        2 * time.Second,
	}
}

// Create provisions the Sandbox described by cfg, copies the Project into
// Main, gives the host SSH and git access to it, and registers Main in Orca.
// Progress goes to out.
func (c Creator) Create(ctx context.Context, cfg wizard.Config, out io.Writer) error {
	if err := c.Provision(ctx, cfg, out); err != nil {
		return err
	}
	if err := SaveInstalled(c.ConfigDir, cfg.Name, Installed{Orca: c.OrcaVersion}); err != nil {
		return err
	}
	if err := c.InstallOrcasm(ctx, cfg, out); err != nil {
		return fmt.Errorf("install orcasm in %s: %w", cfg.Name, err)
	}
	// Starting the Sandbox again needs its secret specs, whatever fails next.
	if err := SaveConfig(c.ConfigDir, cfg); err != nil {
		return err
	}
	main := MainPath(cfg.ProjectPath)
	fmt.Fprintf(out, "==> Copying the Project into %s\n", main)
	err := c.Copy(ctx, cfg.Name, repository.CopyOptions{
		HostPath:     cfg.ProjectPath,
		GuestPath:    main,
		Owner:        guest.User,
		IgnoredFiles: cfg.Project.BringFiles,
	})
	if err != nil {
		return fmt.Errorf("copy the Project into %s: %w", cfg.Name, err)
	}
	fmt.Fprintln(out, "==> Setting up SSH and the host git remote")
	if err := c.SSH.Add(ctx, cfg.Name); err != nil {
		return fmt.Errorf("SSH access to %s: %w", cfg.Name, err)
	}
	if cfg.LinkSSHConfig {
		if err := c.SSH.LinkUserConfig(); err != nil {
			return fmt.Errorf("include orcasm's ssh config in ~/.ssh/config: %w", err)
		}
	}
	err = c.AddRemote(ctx, cfg.ProjectPath, cfg.Name, main, c.SSH.GitSSHCommand())
	if errors.Is(err, repository.ErrOwnSSHCommand) {
		fmt.Fprintf(out, "Note: %v\n", err)
	} else if err != nil {
		return fmt.Errorf("host git remote for %s: %w", cfg.Name, err)
	}
	fmt.Fprintln(out, "==> Registering Main in Orca")
	if err := c.registerWhenOrcaServes(ctx, cfg.Name, main); err != nil {
		return err
	}
	fmt.Fprintln(out, "==> Setting Orca's Worktree hooks (setup starts the Stack, archive tears it down)")
	if err := c.SetHooks(ctx, cfg.Name, main); err != nil {
		return fmt.Errorf("set Orca hooks in %s: %w", cfg.Name, err)
	}
	return nil
}

// registerWhenOrcaServes retries registration while Orca's runtime starts:
// its web UI answers before the runtime takes commands.
func (c Creator) registerWhenOrcaServes(ctx context.Context, name, main string) error {
	ctx, cancel := context.WithTimeout(ctx, c.OrcaWait)
	defer cancel()
	for {
		err := c.RegisterInOrca(ctx, name, main)
		if err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("Orca in %s did not take the project: %w", name, err)
		case <-time.After(c.Poll):
		}
	}
}
