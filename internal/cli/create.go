package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
	"github.com/spf13/cobra"

	"github.com/Betree/Orcasm/internal/msb"
	"github.com/Betree/Orcasm/internal/sandbox"
	"github.com/Betree/Orcasm/internal/wizard"
)

func init() { register(newCreateCmd) }

// provision creates and provisions the Sandbox; tests swap it out.
var provision = func(ctx context.Context, cfg wizard.Config, secretValue sandbox.SecretValue, out io.Writer) error {
	return sandbox.Provision(ctx, msb.CLI{}, cfg, secretValue, out)
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
			fmt.Fprintln(out, "# Sandbox configuration")
			if err := toml.NewEncoder(out).Encode(cfg); err != nil {
				return err
			}
			if err := provision(cmd.Context(), cfg, host.SecretValue, out); err != nil {
				return err
			}
			// Starting the Sandbox again needs its secret specs.
			if err := sandbox.SaveConfig(filepath.Join(userConfig, "orcasm", "sandboxes"), cfg); err != nil {
				return err
			}
			fmt.Fprintf(out, "Sandbox %s is ready. Orca's web UI is served on http://127.0.0.1:%d/\n", cfg.Name, cfg.HostPort)
			return nil
		},
	}
	// For non-interactive runs (CI, end-to-end tests): a TOML file shaped
	// like wizard.Config; omitted answers take their defaults.
	cmd.Flags().StringVar(&answersFile, "answers", "", "answers file (TOML) for a non-interactive run")
	_ = cmd.Flags().MarkHidden("answers")
	return cmd
}
