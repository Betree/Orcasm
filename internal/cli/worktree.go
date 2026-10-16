package cli

// `orcasm worktree ...` runs inside the Sandbox, from Orca's repo hooks
// (see internal/worktree); hidden, as nobody types it on the host.

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

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
				h, err := guestHooks()
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
				if err := h.Setup(cmd.Context(), path); err != nil {
					return err
				}
				fmt.Fprintf(cmd.OutOrStdout(), "Starting the Stack of %s in the background; log: %s\n",
					worktree.Name(path), filepath.Join(h.StateDir(worktree.Name(path)), worktree.LogFile))
				if h.Config.WebService != "" {
					fmt.Fprintf(cmd.OutOrStdout(), "Its web app will be at http://%s/\n", stack.Hostname(worktree.Name(path)))
				}
				return nil
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
				err := worktree.Up(cmd.Context(), streamDocker, args[0])
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
				h, err := guestHooks()
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
					Bridge:   hookrelay.DockerBridge,
					Log:      cmd.ErrOrStderr(),
				}.Run(cmd.Context())
			},
		},
		&cobra.Command{
			Use:   "install-hooks REPO",
			Short: "Make Orca run these hooks for the repo registered at REPO",
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

// guestHooks are the hooks of the Sandbox's Project; ORCASM_CONF_DIR
// relocates /etc/orcasm (as for `stack`).
func guestHooks() (worktree.Hooks, error) {
	file := worktree.ConfigFile
	if dir := os.Getenv("ORCASM_CONF_DIR"); dir != "" {
		file = filepath.Join(dir, filepath.Base(file))
	}
	cfg, err := worktree.ReadConfig(file)
	if err != nil {
		return worktree.Hooks{}, err
	}
	return worktree.Hooks{Config: cfg, Docker: stack.ExecDocker, Detach: detach}, nil
}

// streamDocker runs docker with its output on ours.
func streamDocker(ctx context.Context, args ...string) ([]byte, error) {
	c := exec.CommandContext(ctx, "docker", args...)
	c.Stdout, c.Stderr = os.Stdout, os.Stderr
	if err := c.Run(); err != nil {
		return nil, fmt.Errorf("docker %s: %w", strings.Join(args, " "), err)
	}
	return nil, nil
}

// detach starts this binary with argv in its own session, so it outlives
// the hook (and Orca's process-group kill on timeout), logging to dir.
func detach(dir string, argv ...string) error {
	self, err := os.Executable()
	if err != nil {
		return err
	}
	log, err := os.OpenFile(filepath.Join(dir, worktree.LogFile), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	defer log.Close()
	c := exec.Command(self, argv...)
	c.Stdout, c.Stderr = log, log
	c.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := c.Start(); err != nil {
		return err
	}
	return c.Process.Release()
}
