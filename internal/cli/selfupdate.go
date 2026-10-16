package cli

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Betree/Orcasm/internal/doctor"
	"github.com/Betree/Orcasm/internal/msb"
	"github.com/Betree/Orcasm/internal/selfupdate"
)

func init() { register(newSelfUpdateCmd) }

// brewOrcasm is the cask GoReleaser publishes (.goreleaser.yaml homebrew_casks).
const brewOrcasm = "betree/tap/orcasm"

// msbOnlyFlag runs only the msb half of self-update. The updated binary is
// re-run with it, so the msb pin that counts is the new release's.
const msbOnlyFlag = "msb-only"

func newSelfUpdateCmd(info BuildInfo) *cobra.Command {
	var yes, force, msbOnly bool
	var tag string
	cmd := &cobra.Command{
		Use:   "self-update",
		Short: "Update orcasm to the latest release, and the host msb to the version it pins",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := cmd.OutOrStdout()
			if msbOnly {
				return updateMSB(cmd.Context(), out)
			}
			if _, err := selfupdate.Newer(info.Version, "0.0.0"); err != nil && !force {
				return fmt.Errorf("this orcasm is a development build (version %q) with nothing to compare a release to; rerun with --force to replace it anyway", info.Version)
			}
			exe, err := os.Executable()
			if err != nil {
				return err
			}
			resolved, err := filepath.EvalSymlinks(exe)
			if err != nil {
				return err
			}
			in := bufio.NewReader(cmd.InOrStdin())
			ask := func(q string) bool { return yes || confirm(in, out, q) }

			if selfupdate.Homebrew(resolved) {
				if !ask(fmt.Sprintf("orcasm was installed with Homebrew; run `brew upgrade --cask %s`?", brewOrcasm)) {
					return nil
				}
				if err := runStreaming(cmd.Context(), out, "brew", "upgrade", "--cask", brewOrcasm); err != nil {
					return fmt.Errorf("brew upgrade --cask %s: %w", brewOrcasm, err)
				}
				return finishWithNewBinary(cmd.Context(), out, exe)
			}

			releases := selfupdate.Releases{BaseURL: selfupdate.DefaultBaseURL}
			explicit := tag != ""
			if !explicit {
				if tag, err = releases.Latest(cmd.Context()); err != nil {
					return err
				}
			}
			if newer, err := selfupdate.Newer(info.Version, tag); err == nil && !newer && !explicit {
				fmt.Fprintf(out, "orcasm %s is already up to date\n", info.Version)
				return updateMSB(cmd.Context(), out)
			}
			if !ask(fmt.Sprintf("Update orcasm %s to %s?", info.Version, strings.TrimPrefix(tag, "v"))) {
				return nil
			}
			fmt.Fprintf(out, "==> Downloading orcasm %s\n", tag)
			if err := releases.Install(cmd.Context(), tag, runtime.GOOS, runtime.GOARCH, exe); err != nil {
				return err
			}
			fmt.Fprintf(out, "==> Installed orcasm %s to %s\n", tag, resolved)
			return finishWithNewBinary(cmd.Context(), out, exe)
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "do not ask for confirmation")
	cmd.Flags().BoolVar(&force, "force", false, "replace a development build too")
	// Mirrors install.sh's ORCASM_VERSION: install this tag, even an older one.
	cmd.Flags().StringVar(&tag, "version", "", "install this release tag instead of the latest")
	cmd.Flags().BoolVar(&msbOnly, msbOnlyFlag, false, "only bring the host msb to the pinned version")
	cmd.Flags().MarkHidden("version")
	cmd.Flags().MarkHidden(msbOnlyFlag)
	return cmd
}

// finishWithNewBinary hands the msb half to the binary just installed, then
// reminds that each Sandbox still runs its own copy of orcasm.
func finishWithNewBinary(ctx context.Context, out io.Writer, exe string) error {
	if err := runStreaming(ctx, out, exe, "self-update", "--"+msbOnlyFlag); err != nil {
		return fmt.Errorf("bring msb to the version the new orcasm pins: %w", err)
	}
	fmt.Fprintln(out, "Each Sandbox keeps its own orcasm and Orca until you run `orcasm upgrade <name>`.")
	return nil
}

// updateMSB brings the host msb to msb.PinnedVersion and, when msb changed,
// warns about running Sandboxes: their VM processes keep the old msb until
// restarted, and everything sharing MSB_HOME must run the same msb
// (docs/research/microsandbox.md). They are not restarted here because that
// kills agent sessions and mints a new Orca pairing URL.
func updateMSB(ctx context.Context, out io.Writer) error {
	u := selfupdate.MSBUpgrader{
		Want:          msb.PinnedVersion,
		InstallScript: msb.InstallScript,
		Version:       doctor.RealHost{}.MsbVersion,
		Path: func() (string, error) {
			p, err := exec.LookPath("msb")
			if err != nil {
				return "", err
			}
			return filepath.EvalSymlinks(p)
		},
		Run: runStreaming,
		Output: func(ctx context.Context, name string, args ...string) (string, error) {
			out, err := exec.CommandContext(ctx, name, args...).Output()
			return string(out), err
		},
	}
	changed, err := u.EnsurePinned(ctx, out)
	if changed {
		warnRunningSandboxes(ctx, out)
	}
	if err == nil {
		fmt.Fprintf(out, "msb %s is installed\n", msb.PinnedVersion)
	}
	return err
}

func warnRunningSandboxes(ctx context.Context, out io.Writer) {
	m, err := newManager()
	if err != nil {
		return
	}
	all, err := m.List(ctx)
	if err != nil {
		fmt.Fprintf(out, "warning: cannot list Sandboxes (%v); restart any running one so it uses the new msb\n", err)
		return
	}
	var running []string
	for _, s := range all {
		if s.Running() {
			running = append(running, s.Name)
		}
	}
	if len(running) == 0 {
		return
	}
	fmt.Fprintf(out, "warning: running Sandboxes keep the old msb until restarted: %s\n", strings.Join(running, ", "))
	fmt.Fprintln(out, "Restart each with `orcasm stop <name> && orcasm start <name>` when no agent is busy.")
}

// runStreaming runs a command with its output passed through to out.
func runStreaming(ctx context.Context, out io.Writer, name string, args ...string) error {
	c := exec.CommandContext(ctx, name, args...)
	c.Stdin = os.Stdin
	c.Stdout = out
	c.Stderr = os.Stderr
	return c.Run()
}
