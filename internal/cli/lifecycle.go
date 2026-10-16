package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/Betree/Orcasm/internal/msb"
	"github.com/Betree/Orcasm/internal/sandbox"
	"github.com/Betree/Orcasm/internal/sshaccess"
	"github.com/Betree/Orcasm/internal/wizard"
)

// urlWait bounds how long `url` (and bare `orcasm` after a start) waits for
// Orca to print its web client URL.
const urlWait = 2 * time.Minute

func init() {
	register(newLsCmd)
	register(newStartCmd)
	register(newStopCmd)
	register(newURLCmd)
	register(newRmCmd)
	register(newSSHCmd)
}

func newManager() (sandbox.Manager, error) {
	ssh, err := sshaccess.Default(msb.CLI{})
	if err != nil {
		return sandbox.Manager{}, err
	}
	dir, err := sandboxConfigDir()
	if err != nil {
		return sandbox.Manager{}, err
	}
	startEnv := func(_ context.Context, name string) ([]string, error) {
		host, err := wizard.NewHost()
		if err != nil {
			return nil, err
		}
		return sandbox.SecretEnv(dir, name, host.SecretValue)
	}
	return sandbox.Manager{MSB: msb.CLI{}, SSH: ssh, ConfigDir: dir, StartEnv: startEnv}, nil
}

// sandboxArg is the Sandbox named in args or, without one, the current
// folder's only Sandbox.
func sandboxArg(ctx context.Context, m sandbox.Manager, args []string) (string, error) {
	if len(args) == 1 {
		return args[0], nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	here, err := m.InFolder(ctx, cwd)
	if err != nil {
		return "", err
	}
	switch len(here) {
	case 1:
		return here[0].Name, nil
	case 0:
		return "", fmt.Errorf("no Sandbox for %s; name one (see `orcasm ls`)", cwd)
	default:
		return "", fmt.Errorf("%s has several Sandboxes; name one (see `orcasm ls`)", cwd)
	}
}

// sandboxCmd builds a command acting on one Sandbox, named or the current
// folder's.
func sandboxCmd(use, short string, run func(cmd *cobra.Command, m sandbox.Manager, name string) error) *cobra.Command {
	return &cobra.Command{
		Use:   use + " [name]",
		Short: short,
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			m, err := newManager()
			if err != nil {
				return err
			}
			name, err := sandboxArg(cmd.Context(), m, args)
			if err != nil {
				return err
			}
			return run(cmd, m, name)
		},
	}
}

func newLsCmd(BuildInfo) *cobra.Command {
	return &cobra.Command{
		Use:     "ls",
		Aliases: []string{"list"},
		Short:   "List Sandboxes",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			m, err := newManager()
			if err != nil {
				return err
			}
			all, err := m.List(cmd.Context())
			if err != nil {
				return err
			}
			printSandboxes(cmd.OutOrStdout(), all)
			return nil
		},
	}
}

func printSandboxes(out io.Writer, sbs []sandbox.Sandbox) {
	if len(sbs) == 0 {
		fmt.Fprintln(out, "No Sandboxes yet.")
		return
	}
	w := tabwriter.NewWriter(out, 0, 0, 3, ' ', 0)
	fmt.Fprintln(w, "NAME\tSTATUS\tFOLDER")
	for _, s := range sbs {
		fmt.Fprintf(w, "%s\t%s\t%s\n", s.Name, strings.ToLower(s.Status), s.Folder)
	}
	w.Flush()
}

func newStartCmd(BuildInfo) *cobra.Command {
	return sandboxCmd("start", "Start a Sandbox and print its URL", func(cmd *cobra.Command, m sandbox.Manager, name string) error {
		if err := m.Start(cmd.Context(), name); err != nil {
			return err
		}
		return printURL(cmd, m, name)
	})
}

func newStopCmd(BuildInfo) *cobra.Command {
	var force bool
	cmd := sandboxCmd("stop", "Stop a Sandbox (agent sessions end; files are kept)", func(cmd *cobra.Command, m sandbox.Manager, name string) error {
		if err := m.Stop(cmd.Context(), name, force); err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Stopped %s.\n", name)
		return nil
	})
	cmd.Flags().BoolVarP(&force, "force", "f", false, "kill the Sandbox at once instead of shutting it down")
	return cmd
}

func newURLCmd(BuildInfo) *cobra.Command {
	return sandboxCmd("url", "Print the link that opens a Sandbox's Orca web UI", func(cmd *cobra.Command, m sandbox.Manager, name string) error {
		return printURL(cmd, m, name)
	})
}

func printURL(cmd *cobra.Command, m sandbox.Manager, name string) error {
	ctx, cancel := context.WithTimeout(cmd.Context(), urlWait)
	defer cancel()
	url, err := m.URL(ctx, name)
	if err != nil {
		return err
	}
	fmt.Fprintln(cmd.OutOrStdout(), url)
	return nil
}

func newRmCmd(BuildInfo) *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "rm <name>",
		Short: "Delete a Sandbox and the host state pointing at it",
		Long: "Delete a Sandbox, its SSH entry and the orcasm-<name> git remote. If Main or a Worktree\n" +
			"has uncommitted changes or commits the host lacks, it asks you to type the name first.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			m, err := newManager()
			if err != nil {
				return err
			}
			name := args[0]
			in := bufio.NewReader(cmd.InOrStdin())
			out := cmd.OutOrStdout()
			err = m.Remove(cmd.Context(), name, sandbox.RemoveOptions{
				Force: force,
				Confirm: func(work []string) bool {
					fmt.Fprintf(out, "%s may hold unsaved work:\n", name)
					for _, w := range work {
						fmt.Fprintf(out, "  - %s\n", w)
					}
					fmt.Fprintf(out, "Type %q to remove it anyway: ", name)
					answer, _ := in.ReadString('\n')
					return strings.TrimSpace(answer) == name
				},
			})
			if err != nil {
				return err
			}
			fmt.Fprintf(out, "Removed %s.\n", name)
			return nil
		},
	}
	cmd.Flags().BoolVarP(&force, "force", "f", false, "remove without checking for unsaved work")
	return cmd
}

func newSSHCmd(BuildInfo) *cobra.Command {
	return &cobra.Command{
		Use:   "ssh [name] [-- command...]",
		Short: "Open a shell in a Sandbox as dev, over SSH",
		RunE: func(cmd *cobra.Command, args []string) error {
			m, err := newManager()
			if err != nil {
				return err
			}
			named, remote := args, []string(nil)
			if at := cmd.ArgsLenAtDash(); at >= 0 {
				named, remote = args[:at], args[at:]
			}
			if len(named) > 1 {
				return fmt.Errorf("expected at most one Sandbox name, got %q", named)
			}
			name, err := sandboxArg(cmd.Context(), m, named)
			if err != nil {
				return err
			}
			if _, err := m.Get(cmd.Context(), name); err != nil {
				return err
			}
			access, err := sshaccess.Default(msb.CLI{})
			if err != nil {
				return err
			}
			argv := append(access.Command(name), remote...)
			ssh := exec.CommandContext(cmd.Context(), argv[0], argv[1:]...)
			ssh.Stdin, ssh.Stdout, ssh.Stderr = cmd.InOrStdin(), cmd.OutOrStdout(), cmd.ErrOrStderr()
			var exit *exec.ExitError
			if err := ssh.Run(); errors.As(err, &exit) {
				os.Exit(exit.ExitCode())
			} else if err != nil {
				return err
			}
			return nil
		},
	}
}
