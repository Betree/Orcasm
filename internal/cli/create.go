package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
	"github.com/spf13/cobra"

	"github.com/Betree/Orcasm/internal/wizard"
)

func init() { register(newCreateCmd) }

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
			return toml.NewEncoder(out).Encode(cfg)
		},
	}
	// For non-interactive runs (CI, end-to-end tests): a TOML file shaped
	// like wizard.Config; omitted answers take their defaults.
	cmd.Flags().StringVar(&answersFile, "answers", "", "answers file (TOML) for a non-interactive run")
	_ = cmd.Flags().MarkHidden("answers")
	return cmd
}
