package cli

import (
	"bufio"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/Betree/Orcasm/internal/sandbox"
)

func init() { register(newUpgradeCmd) }

func newUpgradeCmd(info BuildInfo) *cobra.Command {
	var latest bool
	cmd := sandboxCmd("upgrade", "Bring a Sandbox up to this orcasm in place", func(cmd *cobra.Command, m sandbox.Manager, name string) error {
		ctx, out := cmd.Context(), cmd.OutOrStdout()
		orca, from, err := sandbox.NewOrcaSource(info.Version).Desired(ctx, latest)
		if err != nil {
			return err
		}
		binary, err := sandbox.GuestBinary()
		if err != nil {
			return err
		}
		in := bufio.NewReader(cmd.InOrStdin())
		res, err := m.Upgrade(ctx, name, sandbox.UpgradeOptions{
			Orca:        orca,
			OrcaFrom:    from,
			GuestBinary: binary,
			Confirm:     func(q string) bool { return confirm(in, out, q) },
		}, out)
		if err != nil {
			return err
		}
		if res.OrcaRestarted {
			// Each Orca start mints a new pairing link.
			fmt.Fprintf(out, "Orca restarted; its new web UI link for %s:\n", name)
			if err := printURL(cmd, m, name); err != nil {
				return err
			}
		}
		fmt.Fprintf(out, "%s is upgraded.\n", name)
		return nil
	})
	cmd.Long = "Apply the Sandbox's pending migrations, keeping its sign-ins and work, then bring\n" +
		"what runs in it up to date: orcasm itself, the agent harnesses (latest releases) and\n" +
		"Orca (the newest release tested with this orcasm, or Orca's latest with --latest).\n" +
		"It asks before restarting Orca while agents run. A stopped Sandbox is started first\n" +
		"and left running. Workbenches are updated at their next agent launch."
	cmd.Flags().BoolVar(&latest, "latest", false, "install Orca's latest release, even if untested with this orcasm")
	return cmd
}
