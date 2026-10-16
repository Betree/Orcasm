package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Betree/Orcasm/internal/msb"
	"github.com/Betree/Orcasm/internal/sandbox"
	"github.com/Betree/Orcasm/internal/sshaccess"
	"github.com/Betree/Orcasm/internal/wizard"
)

func init() { register(newCreateCmd) }

// create builds the Sandbox from the Wizard's Config; tests swap it out.
var create = func(ctx context.Context, cfg wizard.Config, secretValue sandbox.SecretValue, out io.Writer) error {
	ssh, err := sshaccess.Default(msb.CLI{})
	if err != nil {
		return err
	}
	dir, err := sandboxConfigDir()
	if err != nil {
		return err
	}
	guestBinary, err := sandbox.GuestBinary()
	if err != nil {
		return err
	}
	orca, from, err := sandbox.NewOrcaSource(build.Version).Desired(ctx, false)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "==> Using Orca %s (%s)\n", orca, from)
	return sandbox.NewCreator(msb.CLI{}, ssh, secretValue, dir, guestBinary, orca).Create(ctx, cfg, out)
}

func newCreateCmd(BuildInfo) *cobra.Command {
	var answersFile string
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a Sandbox for a Project through the Wizard",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			host, err := wizard.NewHost()
			if err != nil {
				return err
			}
			if ssh, err := sshaccess.Default(msb.CLI{}); err == nil {
				host.SSHConfigLinked = func() bool {
					linked, err := ssh.UserConfigLinked()
					return linked || err != nil // unreadable: don't offer to edit it
				}
			}
			userConfig, err := os.UserConfigDir()
			if err != nil {
				return err
			}
			cfg, err := wizard.Run(wizard.Options{
				Host:        host,
				ConfigDir:   filepath.Join(userConfig, "orcasm"),
				AnswersFile: answersFile,
				In:          cmd.InOrStdin(),
				Out:         cmd.OutOrStdout(),
			})
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			printSummary(out, cfg)
			if err := create(cmd.Context(), cfg, host.SecretValue, out); err != nil {
				return err
			}
			fmt.Fprintf(out, "Sandbox %s is ready. Orca's web UI is served on http://127.0.0.1:%d/ (`orcasm url %s` prints the link that pairs a browser)\n",
				cfg.Name, cfg.HostPort, cfg.Name)
			return nil
		},
	}
	// For non-interactive runs (CI, end-to-end tests): a TOML file shaped
	// like wizard.Config; omitted answers take their defaults.
	cmd.Flags().StringVar(&answersFile, "answers", "", "answers file (TOML) for a non-interactive run")
	_ = cmd.Flags().MarkHidden("answers")
	return cmd
}

// printSummary says in a few lines what is about to be created.
func printSummary(out io.Writer, cfg wizard.Config) {
	r := cfg.Resources
	fmt.Fprintf(out, "Creating Sandbox %s for %s: %d CPUs, %d GiB memory, %d GiB root disk, %d GiB Docker disk.\n",
		cfg.Name, cfg.ProjectPath, r.CPUs, r.MemoryGiB, r.RootDiskGiB, r.DockerDiskGiB)
	harnesses := "none"
	if len(cfg.Harnesses) > 0 {
		harnesses = strings.Join(cfg.Harnesses, ", ")
	}
	fmt.Fprintf(out, "Harnesses: %s.\n", harnesses)
	var shared []string
	for _, s := range cfg.Secrets {
		shared = append(shared, fmt.Sprintf("%s (sent only to %s)", s.Name, strings.Join(s.Hosts, ", ")))
	}
	if cfg.Git != (wizard.GitIdentity{}) {
		shared = append(shared, fmt.Sprintf("git identity %s <%s>", cfg.Git.Name, cfg.Git.Email))
	}
	if len(shared) > 0 {
		fmt.Fprintf(out, "Shared: %s.\n", strings.Join(shared, "; "))
	}
}

// sandboxConfigDir holds the Config each Sandbox was created from.
func sandboxConfigDir() (string, error) {
	userConfig, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(userConfig, "orcasm", "sandboxes"), nil
}
