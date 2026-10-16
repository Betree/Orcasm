package cli

// `orcasm worktree ...` runs inside the Sandbox, from Orca's repo hooks
// (see internal/worktree); hidden, as nobody types it on the host.

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/Betree/Orcasm/internal/hookrelay"
	"github.com/Betree/Orcasm/internal/orca"
	"github.com/Betree/Orcasm/internal/stack"
	"github.com/Betree/Orcasm/internal/worktree"
)

func init() { register(newWorktreeCmd) }

func newWorktreeCmd(BuildInfo) *cobra.Command {
	cmd := &cobra.Command{
		Use:    "worktree",
		Short:  "Orca's hooks for a Worktree's Stack (runs inside the Sandbox)",
		Hidden: true,
	}
	hook := func(use, short string, run func(worktree.Hooks, *cobra.Command, string) error) *cobra.Command {
		return &cobra.Command{
			Use:   use,
			Short: short,
			Args:  cobra.NoArgs,
			RunE: func(cmd *cobra.Command, _ []string) error {
				path := os.Getenv("ORCA_WORKTREE_PATH")
				if path == "" {
					return fmt.Errorf("ORCA_WORKTREE_PATH is not set: Orca runs this as a repo hook")
				}
				h, err := worktree.Guest()
				if err != nil {
					return err
				}
				return run(h, cmd, path)
			},
		}
	}
	cmd.AddCommand(
		hook("setup", "Start the Worktree's Stack in the background (Orca setup hook)",
			func(h worktree.Hooks, cmd *cobra.Command, path string) error {
				return h.Setup(cmd.Context(), path, cmd.OutOrStdout())
			}),
		hook("archive", "Tear down the Worktree's Stack (Orca archive hook)",
			func(h worktree.Hooks, cmd *cobra.Command, path string) error {
				return h.Archive(cmd.Context(), path)
			}),
		&cobra.Command{
			Use:    "up DIR",
			Short:  "Build and start the Stack saved in DIR (detached by setup)",
			Args:   cobra.ExactArgs(1),
			Hidden: true,
			RunE: func(cmd *cobra.Command, args []string) error {
				// Compose's progress goes to the log setup made our output.
				err := worktree.Up(cmd.Context(), stack.StreamDocker(cmd.OutOrStdout()), args[0])
				if err == nil {
					fmt.Fprintln(cmd.OutOrStdout(), "orcasm: the Stack is up")
				}
				return err
			},
		},
		&cobra.Command{
			Use:   "workbench DIR",
			Short: "Wait for the Stack of the Worktree holding DIR and print its Workbench container (agent wrapper)",
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				h, err := worktree.Guest()
				if err != nil {
					return err
				}
				id, err := h.Workbench(cmd.Context(), args[0], cmd.ErrOrStderr())
				if err != nil {
					return err
				}
				fmt.Fprintln(cmd.OutOrStdout(), id)
				return nil
			},
		},
		&cobra.Command{
			Use:   "hook-relay",
			Short: "Relay Orca's agent hook port from Docker's bridge to Orca (systemd unit)",
			Args:  cobra.NoArgs,
			RunE: func(cmd *cobra.Command, _ []string) error {
				rt, err := orca.DefaultRuntime()
				if err != nil {
					return err
				}
				return hookrelay.Relay{
					Endpoint: rt.HookEndpointFile(),
					Bridge:   hookrelay.DockerBridge(stack.ExecDocker),
					Log:      cmd.ErrOrStderr(),
				}.Run(cmd.Context())
			},
		},
		&cobra.Command{
			Use:   "install-hooks REPOSITORY",
			Short: "Make Orca run these hooks for the Repository registered at path REPOSITORY",
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				rt, err := orca.DefaultRuntime()
				if err != nil {
					return err
				}
				return rt.UpdateRepo(cmd.Context(), args[0], worktree.OrcaSettings())
			},
		},
	)
	return cmd
}
