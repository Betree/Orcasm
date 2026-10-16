package sandbox

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/Betree/Orcasm/internal/guest"
	"github.com/Betree/Orcasm/internal/stack"
	"github.com/Betree/Orcasm/internal/version"
)

// UpgradeOptions are what Upgrade reconciles the Sandbox with.
type UpgradeOptions struct {
	// Orca is the desired Orca release (OrcaSource.Desired) and OrcaFrom
	// where it comes from, for the progress lines.
	Orca, OrcaFrom string
	// GuestBinary is the orcasm to install in the Sandbox (GuestBinary).
	GuestBinary string
	// Confirm asks question and reports whether to go ahead; nil means no.
	// It is asked before restarting Orca under running agents.
	Confirm func(question string) bool
}

// UpgradeResult says what Upgrade changed.
type UpgradeResult struct {
	// OrcaRestarted means Orca's service restarted: it has a new web
	// client URL (Manager.URL).
	OrcaRestarted bool
}

// upgradeOrca installs Orca at a release and restarts its service (orca.sh).
// Args: upgrade-orca <user> <hostport> <version>.
var upgradeOrca = orcaFunctions + `set -euo pipefail
install_orca "$3"
start_orca "$1" "$2"
`

// upgradeHarnesses brings each harness to its latest release (orca.sh).
// Args: upgrade-harnesses <harness...>.
var upgradeHarnesses = orcaFunctions + `set -euo pipefail
install_harnesses "$@"
`

// agentProcesses counts the Sandbox user's agent processes: the wrappers
// (/usr/local/bin/<harness>, which stay in the foreground for agents in
// Workbenches) and the harnesses themselves, on Main or in a Workbench
// (whose processes the Sandbox sees). Orca's own terminals do not count.
var agentProcesses = `pgrep -c -u ` + guest.User + ` -f '(^|/bin/bash )/usr/local/bin/(claude|codex|opencode)( |$)|` +
	stack.HarnessLayer + `/' || true`

// Upgrade brings the named Sandbox up to this orcasm in place (ADR 0006):
// first its pending migrations, then version reconciliation. A stopped
// Sandbox is started first, since both need systemd and Orca's service; it
// is left running.
//
// Reconciliation reinstalls orcasm's own layer in the Sandbox (orcasm, its
// config, the agent wrappers, the stack command, the proxy), all
// idempotent, then updates the harnesses to their latest releases unless
// agents run, then installs opts.Orca if the installed Orca is older,
// asking opts.Confirm first if agents run, since the restart can end their
// sessions. It never downgrades Orca. Workbenches are left alone: each picks
// up this orcasm's Workbench layer at its next agent launch, when idle
// (internal/worktree).
func (m Manager) Upgrade(ctx context.Context, name string, opts UpgradeOptions, out io.Writer) (UpgradeResult, error) {
	var res UpgradeResult
	s, err := m.Get(ctx, name)
	if err != nil {
		return res, err
	}
	if !s.Running() {
		fmt.Fprintf(out, "==> Starting %s\n", name)
		if err := m.Start(ctx, name); err != nil {
			return res, err
		}
	}
	if err := m.Migrate(ctx, name, out); err != nil {
		return res, err
	}
	cfg, err := LoadConfig(m.ConfigDir, name)
	if err != nil {
		return res, err
	}
	installed, err := m.installedOrca(ctx, name)
	if err != nil {
		return res, err
	}
	agents, err := m.runningAgents(ctx, name)
	if err != nil {
		return res, err
	}

	fmt.Fprintln(out, "==> Reinstalling orcasm's layer in the Sandbox")
	stackArgv, err := stackInstallArgv(MainPath(cfg.ProjectPath), cfg.Project.ComposeFiles)
	if err != nil {
		return res, err
	}
	if err := m.Installer.ExecStreaming(ctx, name, "", out, agentsInstallArgv(cfg.Harnesses)...); err != nil {
		return res, fmt.Errorf("install the agent wrappers in %s: %w", name, err)
	}
	if err := InstallOrcasm(ctx, m.Installer, cfg, opts.GuestBinary, out); err != nil {
		return res, fmt.Errorf("install orcasm in %s: %w", name, err)
	}
	if err := m.Installer.ExecStreaming(ctx, name, "", out, stackArgv...); err != nil {
		return res, fmt.Errorf("install the stack command in %s: %w", name, err)
	}
	if err := m.Installer.ExecStreaming(ctx, name, "", out, proxyArgv()...); err != nil {
		return res, fmt.Errorf("restart the Worktree proxy in %s: %w", name, err)
	}
	fmt.Fprintln(out, "Workbenches take the new Workbench layer at their next agent launch, once idle.")

	switch {
	case len(cfg.Harnesses) == 0:
	case agents > 0:
		fmt.Fprintf(out, "Harnesses: skipped while agents run in %s; rerun `orcasm upgrade %s` once they are done.\n", name, name)
	default:
		argv := append([]string{"bash", "-c", upgradeHarnesses, "upgrade-harnesses"}, cfg.Harnesses...)
		if err := m.Installer.ExecStreaming(ctx, name, "", out, argv...); err != nil {
			return res, fmt.Errorf("upgrade the harnesses of %s: %w", name, err)
		}
	}

	fmt.Fprintf(out, "Orca: installed %s, desired %s", orNone(installed), opts.Orca)
	if opts.OrcaFrom != "" {
		fmt.Fprintf(out, " (%s)", opts.OrcaFrom)
	}
	fmt.Fprintln(out)
	switch {
	case installed != "" && version.Compare(installed, opts.Orca) >= 0:
		if installed != opts.Orca {
			fmt.Fprintf(out, "Orca %s is newer than %s; kept.\n", installed, opts.Orca)
		} else {
			fmt.Fprintln(out, "Orca is up to date.")
		}
	case agents > 0 && (opts.Confirm == nil || !opts.Confirm(fmt.Sprintf(
		"Restarting Orca in %s may end the agent sessions running there. Upgrade Orca to %s now?", name, opts.Orca))):
		fmt.Fprintf(out, "Orca: not upgraded; run `orcasm upgrade %s` once the agents are done.\n", name)
	default:
		argv := []string{"bash", "-c", upgradeOrca, "upgrade-orca", guest.User, strconv.Itoa(cfg.HostPort), opts.Orca}
		if err := m.Installer.ExecStreaming(ctx, name, "", out, argv...); err != nil {
			return res, fmt.Errorf("upgrade Orca in %s: %w", name, err)
		}
		installed, res.OrcaRestarted = opts.Orca, true
	}
	if err := SaveInstalled(m.ConfigDir, name, Installed{Orca: installed}); err != nil {
		return res, err
	}
	return res, nil
}

// installedOrca is the Sandbox's orca-ide package version ("" if none).
func (m Manager) installedOrca(ctx context.Context, name string) (string, error) {
	v, err := m.MSB.Exec(ctx, name, "", "sh", "-c", `dpkg-query -W -f='${Version}' orca-ide 2>/dev/null || true`)
	if err != nil {
		return "", fmt.Errorf("read the Orca installed in %s: %w", name, err)
	}
	return strings.TrimSpace(v), nil
}

// runningAgents counts the agent processes in the Sandbox (agentProcesses).
func (m Manager) runningAgents(ctx context.Context, name string) (int, error) {
	raw, err := m.MSB.Exec(ctx, name, "", "sh", "-c", agentProcesses)
	if err != nil {
		return 0, fmt.Errorf("look for running agents in %s: %w", name, err)
	}
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return 0, fmt.Errorf("look for running agents in %s: unexpected %q", name, raw)
	}
	return n, nil
}

func orNone(v string) string {
	if v == "" {
		return "none"
	}
	return v
}
