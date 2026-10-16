package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/Betree/Orcasm/internal/sandbox"
)

func init() { register(newUpgradeCmd) }

func newUpgradeCmd(BuildInfo) *cobra.Command {
	cmd := sandboxCmd("upgrade", "Bring a Sandbox up to this orcasm in place", func(cmd *cobra.Command, m sandbox.Manager, name string) error {
		out := cmd.OutOrStdout()
		if err := m.Upgrade(cmd.Context(), name, out); err != nil {
			return err
		}
		fmt.Fprintf(out, "%s is up to date.\n", name)
		return nil
	})
	cmd.Long = "Apply the Sandbox's pending migrations, keeping its sign-ins and work.\n" +
		"A stopped Sandbox is started first and left running."
	return cmd
}
