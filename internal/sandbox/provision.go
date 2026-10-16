package sandbox

// Provisioning: a microsandbox VM with systemd, Docker, Orca (served to the
// host's browser), the selected agent harnesses and gh, all used through the
// non-root User.

import (
	"context"
	_ "embed"
	"fmt"
	"io"
	"path"
	"path/filepath"
	"slices"
	"strconv"

	"github.com/Betree/Orcasm/internal/msb"
	"github.com/Betree/Orcasm/internal/wizard"
)

const (
	// Image is the Sandbox base image (systemd as PID 1).
	Image = "ghcr.io/superradcompany/ubuntu-systemd:24.04"
	// User is the guest user owning Main and running Orca and agents
	// (ADR 0003; SSH logs in as it, see docs/research/microsandbox.md §11).
	User = "dev"
	// OrcaVersion is the Orca release installed in new Sandboxes (ADR 0006).
	OrcaVersion = "1.4.227"
	// OrcaPort is the guest port `orca-ide serve` listens on.
	OrcaPort = 6768
	// Schema is the last migration a freshly provisioned Sandbox is at,
	// recorded in the `orcasm.schema` label (ADR 0006). 0 = no migrations yet.
	Schema = 0
)

// Labels Provision adds to CreateLabels.
const (
	LabelPort   = "orcasm.port"   // host port of Orca's web UI
	LabelSchema = "orcasm.schema" // last applied migration
)

// CreateMSB is the slice of microsandbox Provision needs. msb.CLI
// implements it.
type CreateMSB interface {
	Create(ctx context.Context, opts msb.CreateOptions) error
	Remove(ctx context.Context, name string) error
	List(ctx context.Context, labels ...msb.Label) ([]msb.Listing, error)
	ExecStreaming(ctx context.Context, name, user string, out io.Writer, argv ...string) error
}

//go:embed provision.sh
var provisionScript string

// MainPath is where Main lives in the Sandbox for the Project at folder.
// It exists (empty, owned by User) once Provision returns, and is the
// working directory of msb exec and SSH sessions.
func MainPath(folder string) string {
	return path.Join("/home", User, filepath.Base(folder))
}

// SecretValue reads a Secret's value on the host (wizard.Host.SecretValue).
type SecretValue func(wizard.Secret) (string, error)

// Provision creates the Sandbox described by cfg and installs everything in
// it, streaming progress to out. It returns once Orca's web UI answers on
// the guest port; the host reaches it on 127.0.0.1:cfg.HostPort.
//
// cfg.Secrets become msb secrets, valued through secretValue; every later
// `msb start` of the Sandbox needs those values again.
func Provision(ctx context.Context, m CreateMSB, cfg wizard.Config, secretValue SecretValue, out io.Writer) error {
	// Checked here, not just in the Wizard: a failed create is cleaned up
	// with `msb rm`, which must never hit someone else's sandbox.
	all, err := m.List(ctx)
	if err != nil {
		return err
	}
	if slices.ContainsFunc(all, func(l msb.Listing) bool { return l.Name == cfg.Name }) {
		return fmt.Errorf("a sandbox named %s already exists", cfg.Name)
	}
	main := MainPath(cfg.Folder)
	secrets, err := valued(cfg.Secrets, secretValue)
	if err != nil {
		return err
	}
	opts := msb.CreateOptions{
		Image: Image,
		Name:  cfg.Name,
		Labels: append(CreateLabels(cfg.Folder, main),
			msb.Label{Key: LabelPort, Value: strconv.Itoa(cfg.HostPort)},
			msb.Label{Key: LabelSchema, Value: strconv.Itoa(Schema)}),
		CPUs:        cfg.Resources.CPUs,
		MemoryGiB:   cfg.Resources.MemoryGiB,
		RootDiskGiB: cfg.Resources.RootDiskGiB,
		// Docker's data must not sit on the overlay root (microsandbox.md §8).
		OwnedDisks: []msb.OwnedDisk{{Path: "/var/lib/docker", SizeGiB: cfg.Resources.DockerDiskGiB}},
		Ports:      []msb.Port{{HostAddr: "127.0.0.1", Host: cfg.HostPort, Guest: OrcaPort}},
		Shell:      "/bin/bash",
		Workdir:    main,
		Mkdirs:     []string{main},
		Secrets:    secrets,
	}
	fmt.Fprintf(out, "==> Creating Sandbox %s\n", cfg.Name)
	if err := m.Create(ctx, opts); err != nil {
		// A failed create still leaves a stopped record holding the name.
		_ = m.Remove(ctx, cfg.Name)
		return fmt.Errorf("create Sandbox %s: %w", cfg.Name, err)
	}
	argv := append([]string{"bash", "-c", provisionScript, "provision",
		User, strconv.Itoa(cfg.HostPort), OrcaVersion, main, cfg.Git.Name, cfg.Git.Email},
		cfg.Harnesses...)
	if err := m.ExecStreaming(ctx, cfg.Name, "", out, argv...); err != nil {
		return fmt.Errorf("provision Sandbox %s (kept for inspection; `msb rm --force %s` to discard): %w",
			cfg.Name, cfg.Name, err)
	}
	return nil
}
