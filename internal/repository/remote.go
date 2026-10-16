package repository

import "context"

// SSHHost is the ~/.ssh/config host alias for a Sandbox (written by the SSH
// setup step), which the host git remote goes through.
func SSHHost(sandbox string) string { return "orcasm-" + sandbox }

// AddSandboxRemote adds (or repoints) a remote named after the Sandbox's SSH
// host alias in the host Repository, so `git fetch orcasm-<sandbox>` pulls
// agents' work from guestPath. Fetching runs no Sandbox code on the host.
func AddSandboxRemote(ctx context.Context, hostRepo, sandbox, guestPath string) error {
	name := SSHHost(sandbox)
	url := name + ":" + guestPath
	if _, err := hostGit(ctx, hostRepo, "remote", "get-url", name); err == nil {
		_, err = hostGit(ctx, hostRepo, "remote", "set-url", name, url)
		return err
	}
	_, err := hostGit(ctx, hostRepo, "remote", "add", name, url)
	return err
}
