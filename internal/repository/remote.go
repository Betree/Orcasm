package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// SSHHost is the ssh host alias of a Sandbox (sshaccess.HostAlias), which the
// host git remote goes through.
func SSHHost(sandbox string) string { return "orcasm-" + sandbox }

// ErrOwnSSHCommand is returned by AddSandboxRemote when the host Repository
// already runs git over ssh with a command of the user's own.
var ErrOwnSSHCommand = errors.New("core.sshCommand is already set")

// AddSandboxRemote adds (or repoints) a remote named after the Sandbox's SSH
// host alias in the host Repository, so `git fetch orcasm-<sandbox>` pulls
// agents' work from guestPath. Fetching runs no Sandbox code on the host.
//
// The alias is only known to orcasm's ssh config, which ~/.ssh/config may not
// include, so git's ssh command is set to sshCommand (normally
// sshaccess.Access.GitSSHCommand, which also reads the user's ssh config). A
// core.sshCommand of the user's own is kept: the remote is still added, and
// an error wrapping ErrOwnSSHCommand says what fetching needs.
func AddSandboxRemote(ctx context.Context, hostRepo, sandbox, guestPath, sshCommand string) error {
	name := SSHHost(sandbox)
	url := name + ":" + guestPath
	if _, err := hostGit(ctx, hostRepo, "remote", "get-url", name); err == nil {
		if _, err := hostGit(ctx, hostRepo, "remote", "set-url", name, url); err != nil {
			return err
		}
	} else if _, err := hostGit(ctx, hostRepo, "remote", "add", name, url); err != nil {
		return err
	}
	current := sshCommandOf(ctx, hostRepo)
	if current != "" && current != sshCommand {
		return fmt.Errorf("%w (%q): fetching from %s needs orcasm's ssh config, e.g. `git -c core.sshCommand=%q fetch %s`",
			ErrOwnSSHCommand, current, name, sshCommand, name)
	}
	if _, err := hostGit(ctx, hostRepo, "config", "--local", "core.sshCommand", sshCommand); err != nil {
		return err
	}
	// Remembered so RemoveSandboxRemote only ever unsets orcasm's own value.
	_, err := hostGit(ctx, hostRepo, "config", "--local", ownSSHCommandKey, sshCommand)
	return err
}

// ownSSHCommandKey records the core.sshCommand orcasm set.
const ownSSHCommandKey = "orcasm.sshCommand"

// RemoveSandboxRemote drops the remote AddSandboxRemote added, along with its
// remote-tracking branches, and orcasm's core.sshCommand once no Sandbox
// remote is left. Without such a remote it does nothing.
func RemoveSandboxRemote(ctx context.Context, hostRepo, sandbox string) error {
	name := SSHHost(sandbox)
	if _, err := hostGit(ctx, hostRepo, "remote", "get-url", name); err != nil {
		return nil
	}
	if _, err := hostGit(ctx, hostRepo, "remote", "remove", name); err != nil {
		return err
	}
	remotes, err := hostGit(ctx, hostRepo, "remote")
	if err != nil {
		return err
	}
	for _, r := range strings.Fields(remotes) {
		if strings.HasPrefix(r, SSHHost("")) {
			return nil
		}
	}
	own, _ := hostGit(ctx, hostRepo, "config", "--local", ownSSHCommandKey)
	if own == "" {
		return nil
	}
	if local, _ := hostGit(ctx, hostRepo, "config", "--local", "core.sshCommand"); strings.TrimSpace(local) == strings.TrimSpace(own) {
		if _, err := hostGit(ctx, hostRepo, "config", "--local", "--unset", "core.sshCommand"); err != nil {
			return err
		}
	}
	_, err = hostGit(ctx, hostRepo, "config", "--local", "--unset", ownSSHCommandKey)
	return err
}

// sshCommandOf is the effective core.sshCommand of the Repository ("" when
// unset).
func sshCommandOf(ctx context.Context, repo string) string {
	out, _ := hostGit(ctx, repo, "config", "core.sshCommand")
	return strings.TrimSpace(out)
}
