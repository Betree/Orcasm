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
	"github.com/Betree/Orcasm/internal/wizard"
)

// UpgradeOptions are what Upgrade reconciles the Sandbox with.
type UpgradeOptions struct {
	// Orca is the desired Orca release (OrcaSource.Desired).
	Orca OrcaTarget
	// GuestBinary is the orcasm to install in the Sandbox (GuestBinary),
	// and Orcasm its version (the host's BuildInfo.Version), recorded.
	GuestBinary, Orcasm string
	// Confirm asks question and reports whether to go ahead; nil means no.
	// It is asked before what can end running agents' sessions.
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

// agentProcesses counts the Sandbox user's agent processes of any harness
// orcasm supports, on Main or in a Workbench (whose processes the Sandbox
// sees).
var agentProcesses = `pgrep -c -u ` + guest.User + ` -f '` + stack.AgentProcesses(wizard.SupportedHarnesses) + `' || true`

// Upgrade brings the named Sandbox up to this orcasm in place (ADR 0006):
// first its pending migrations, then version reconciliation. A stopped
// Sandbox is started first, since both need systemd and Orca's service; it
// is left running.
//
// Reconciliation reinstalls orcasm's own layer in the Sandbox (orcasm, its
// config, the agent wrappers, the stack command, the proxy), all
// idempotent and harmless to running agents. Then it updates the harnesses
// to their latest releases (they float, ADR 0006) and installs opts.Orca if
// the installed Orca is older, restarting it. Both can end the sessions of
// running agents (a harness's files replaced under it, Orca's terminals
// restarted), so while agents run it asks opts.Confirm once for both, and
// does neither without consent. It never downgrades Orca. Workbenches are
// left alone: each picks up this orcasm's Workbench layer at its next agent
// launch, when idle (internal/worktree).
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
	if err := m.reinstallOrcasmLayer(ctx, cfg, opts.GuestBinary, out); err != nil {
		return res, err
	}

	orcaBehind := m.reportOrca(installed, opts.Orca, out)
	harnesses := len(cfg.Harnesses) > 0
	if orcaBehind || harnesses {
		ok, err := m.mayInterruptAgents(ctx, name, orcaBehind, harnesses, opts, out)
		if err != nil {
			return res, err
		}
		if !ok {
			harnesses, orcaBehind = false, false
		}
	}
	if harnesses {
		argv := append([]string{"bash", "-c", upgradeHarnesses, "upgrade-harnesses"}, cfg.Harnesses...)
		if err := m.Installer.ExecStreaming(ctx, name, "", out, argv...); err != nil {
			return res, fmt.Errorf("upgrade the harnesses of %s: %w", name, err)
		}
	}
	if orcaBehind {
		argv := []string{"bash", "-c", upgradeOrca, "upgrade-orca", guest.User, strconv.Itoa(cfg.HostPort), opts.Orca.Version}
		if err := m.Installer.ExecStreaming(ctx, name, "", out, argv...); err != nil {
			return res, fmt.Errorf("upgrade Orca in %s: %w", name, err)
		}
		installed, res.OrcaRestarted = opts.Orca.Version, true
	}
	if err := SaveInstalled(m.ConfigDir, name, Installed{Orca: installed, Orcasm: opts.Orcasm}); err != nil {
		return res, err
	}
	return res, nil
}

// reinstallOrcasmLayer reinstalls orcasm's own layer, the binary included.
func (m Manager) reinstallOrcasmLayer(ctx context.Context, cfg wizard.Config, binary string, out io.Writer) error {
	wrappers, rest, err := orcasmLayer(cfg)
	if err != nil {
		return err
	}
	if err := runSteps(ctx, m.Installer, cfg.Name, out, wrappers); err != nil {
		return err
	}
	fmt.Fprintln(out, "==> Installing this orcasm in the Sandbox")
	if err := InstallOrcasm(ctx, m.Installer, cfg, binary, out); err != nil {
		return fmt.Errorf("install orcasm in %s: %w", cfg.Name, err)
	}
	if err := runSteps(ctx, m.Installer, cfg.Name, out, rest...); err != nil {
		return err
	}
	fmt.Fprintln(out, "Workbenches take the new Workbench layer at their next agent launch, once idle.")
	return nil
}

// reportOrca prints installed vs desired Orca and reports whether the
// installed one is older, so to be upgraded.
func (m Manager) reportOrca(installed string, desired OrcaTarget, out io.Writer) (behind bool) {
	fmt.Fprintf(out, "Orca: installed %s, desired %s", orNone(installed), desired.Version)
	if desired.From != "" {
		fmt.Fprintf(out, " (%s)", desired.From)
	}
	fmt.Fprintln(out)
	switch {
	case installed == "" || version.Compare(installed, desired.Version) < 0:
		return true
	case installed != desired.Version:
		fmt.Fprintf(out, "Orca %s is newer than %s; kept.\n", installed, desired.Version)
	default:
		fmt.Fprintln(out, "Orca is up to date.")
	}
	return false
}

// mayInterruptAgents reports whether the harness update and Orca restart
// may go ahead: at once with no agent running, else only once confirmed.
func (m Manager) mayInterruptAgents(ctx context.Context, name string, orca, harnesses bool, opts UpgradeOptions, out io.Writer) (bool, error) {
	agents, err := m.runningAgents(ctx, name)
	if err != nil || agents == 0 {
		return err == nil, err
	}
	var what []string
	if harnesses {
		what = append(what, "updating the agent harnesses")
	}
	if orca {
		what = append(what, "restarting Orca to upgrade it to "+opts.Orca.Version)
	}
	q := fmt.Sprintf("Agents are running in %s; %s may end their sessions. Go ahead now?", name, strings.Join(what, " and "))
	if opts.Confirm != nil && opts.Confirm(q) {
		return true, nil
	}
	fmt.Fprintf(out, "Not %s; run `orcasm upgrade %s` once the agents are done.\n", strings.Join(what, " nor "), name)
	return false, nil
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
