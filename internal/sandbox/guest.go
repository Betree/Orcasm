package sandbox

// orcasm inside the Sandbox: Orca's repo hooks run `orcasm worktree
// setup|archive` (internal/worktree), so the Sandbox gets a linux orcasm
// binary and the Project's Worktree config. The agent wrappers
// (internal/workbench, installed by Provision) use `orcasm worktree
// workbench`, and the hook relay unit (internal/hookrelay) runs orcasm.

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"runtime"

	"github.com/Betree/Orcasm/internal/wizard"
	"github.com/Betree/Orcasm/internal/worktree"
)

// WorktreesDir is where Orca creates Worktrees (owned by User).
const WorktreesDir = worktree.BasePath

// GuestMSB is the slice of microsandbox InstallOrcasm needs. msb.CLI
// implements it.
type GuestMSB interface {
	CopyIn(ctx context.Context, name, hostPath, guestPath string) error
	ExecStreaming(ctx context.Context, name, user string, out io.Writer, argv ...string) error
}

// stagedBinary is where the binary lands before the installer moves it.
const stagedBinary = "/tmp/orcasm.new"

// guestInstaller installs the staged binary and the Worktree config,
// creates WorktreesDir for User and starts the hook relay unit. ORCASM_ROOT
// relocates it all (and skips systemctl) and ORCASM_OWNER replaces User
// (tests).
// Args: install-orcasm <staged binary> <binary> <config file> <config json>
// <worktrees dir> <user> <unit name> <unit>.
const guestInstaller = `set -euo pipefail
root=${ORCASM_ROOT:-}
staged=$1 bin=$2 conf=$3 json=$4 worktrees=$5 owner=${ORCASM_OWNER:-$6} unit_name=$7 unit=$8
install -d "$root$(dirname "$bin")" "$root$(dirname "$conf")"
install -m 0755 "$root$staged" "$root$bin"
rm -f "$root$staged"
printf '%s\n' "$json" >"$root$conf"
install -d -o "$owner" -g "$owner" "$root$worktrees"
install -d "$root/etc/systemd/system"
printf '%s' "$unit" >"$root/etc/systemd/system/$unit_name.service"
if [ -z "$root" ]; then
  systemctl daemon-reload
  systemctl enable "$unit_name" >/dev/null 2>&1
  systemctl restart "$unit_name"
fi
`

// HookRelayUnit is the systemd unit relaying Orca's agent hook port to
// Docker's bridge, for agents in Workbenches (internal/hookrelay).
const HookRelayUnit = "orcasm-hook-relay"

// hookRelayUnit runs `orcasm worktree hook-relay` as User, who runs Orca
// (its endpoint file is in User's Orca user data).
var hookRelayUnit = `[Unit]
Description=Relay Orca's agent hooks from Workbenches (orcasm)
After=docker.service ` + OrcaUnit + `.service
Wants=docker.service

[Service]
User=` + User + `
Environment=HOME=/home/` + User + `
ExecStart=` + worktree.Binary + ` worktree hook-relay
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
`

// InstallOrcasm copies binary (orcasm built for the Sandbox: linux, the
// host's arch) into Sandbox cfg.Name, writes the Worktree config the hooks
// read, and creates WorktreesDir.
func InstallOrcasm(ctx context.Context, m GuestMSB, cfg wizard.Config, binary string, out io.Writer) error {
	main := MainPath(cfg.ProjectPath)
	wc := worktree.Config{
		Main:         main,
		ComposeFiles: cfg.Project.ComposeFiles,
		Workbench:    cfg.Project.Workbench,
		WebService:   cfg.Project.WebService,
		WebPort:      cfg.Project.WebPort,
		Harnesses:    cfg.Harnesses,
		Home:         "/home/" + User,
	}
	for _, s := range cfg.Secrets {
		wc.Secrets = append(wc.Secrets, s.Name)
	}
	raw, err := json.MarshalIndent(wc, "", "  ")
	if err != nil {
		return err
	}
	fmt.Fprintln(out, "==> Installing orcasm in the Sandbox for Orca's Worktree hooks")
	if err := m.CopyIn(ctx, cfg.Name, binary, stagedBinary); err != nil {
		return err
	}
	return m.ExecStreaming(ctx, cfg.Name, "", out, "bash", "-c", guestInstaller, "install-orcasm",
		stagedBinary, worktree.Binary, worktree.ConfigFile, string(raw), WorktreesDir, User,
		HookRelayUnit, hookRelayUnit)
}

// GuestBinary is the orcasm binary to install in Sandboxes: this very
// binary on a linux host (the Sandbox shares the host's arch), else the one
// ORCASM_LINUX_BINARY names.
func GuestBinary() (string, error) {
	if p := os.Getenv("ORCASM_LINUX_BINARY"); p != "" {
		return p, nil
	}
	if runtime.GOOS != "linux" {
		return "", fmt.Errorf("Sandboxes need a linux/%s orcasm binary: set ORCASM_LINUX_BINARY to one", runtime.GOARCH)
	}
	return os.Executable()
}
