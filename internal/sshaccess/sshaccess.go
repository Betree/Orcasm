// Package sshaccess gives the host SSH access to Sandboxes as the `dev`
// user, so VS Code Remote-SSH (and plain ssh) can reach them as
// `orcasm-<name>`.
//
// Orcasm keeps its own key, ssh config and known_hosts under Dir and never
// touches the user's other keys. The user's ~/.ssh/config only gains one
// `Include` line, and only when the caller (with the user's consent) asks for
// it via LinkUserConfig. See docs/research/microsandbox.md §11.
package sshaccess

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// MSB is the part of microsandbox sshaccess needs. msb.CLI implements it.
type MSB interface {
	// AuthorizeSSHKey adds the public key in pubKeyFile to msb's global
	// authorized_keys. It must be idempotent.
	AuthorizeSSHKey(ctx context.Context, pubKeyFile string) error
	// SandboxHostKey returns the sandbox's SSH host public key
	// ("<type> <base64>").
	SandboxHostKey(ctx context.Context, name string) (string, error)
}

// Access manages SSH access to Sandboxes.
type Access struct {
	// UserSSHDir is the user's ssh directory (normally ~/.ssh).
	UserSSHDir string
	// Dir is orcasm's own ssh directory (normally ~/.config/orcasm/ssh).
	Dir string
	MSB MSB
}

// Default returns an Access rooted at the real user's home and config dirs.
func Default(m MSB) (Access, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Access{}, err
	}
	cfg, err := os.UserConfigDir()
	if err != nil {
		return Access{}, err
	}
	return Access{
		UserSSHDir: filepath.Join(home, ".ssh"),
		Dir:        filepath.Join(cfg, "orcasm", "ssh"),
		MSB:        m,
	}, nil
}

// HostAlias is the ssh host name of a Sandbox.
func HostAlias(name string) string { return "orcasm-" + name }

// Command is the ssh command line that logs into the Sandbox, with extra ssh
// options before the host. It reads orcasm's own ssh config, so it works
// whether or not ~/.ssh/config includes it.
func (a Access) Command(name string, opts ...string) []string {
	argv := append([]string{"ssh", "-F", a.configPath()}, opts...)
	return append(argv, HostAlias(name))
}

// GitSSHCommand is the ssh command (a shell command line, for git's
// core.sshCommand) that reaches `orcasm-<name>` hosts without the user's
// ~/.ssh/config including orcasm's, while every other host keeps the user's
// and the system's ssh settings. Add writes the config file it reads.
func (a Access) GitSSHCommand() string {
	return "ssh -F " + shellQuote(a.gitConfigPath())
}

func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// gitConfig is read by GitSSHCommand: `-F` replaces ssh's usual config files,
// so it includes them back after orcasm's own Host entries.
func (a Access) gitConfig() string {
	return "# Written by orcasm: its Sandboxes, then your usual ssh config.\n" +
		"Include " + quote(a.configPath()) + "\n" +
		"Include " + quote(a.userConfigPath()) + "\n" +
		"Include /etc/ssh/ssh_config\n"
}

func (a Access) gitConfigPath() string  { return filepath.Join(a.Dir, "git_ssh_config") }
func (a Access) configPath() string     { return filepath.Join(a.Dir, "config") }
func (a Access) keyPath() string        { return filepath.Join(a.Dir, "id_ed25519") }
func (a Access) knownHostsPath() string { return filepath.Join(a.Dir, "known_hosts") }

// Add makes the Sandbox reachable as `ssh orcasm-<name>`: it ensures orcasm's
// key exists and is authorized in msb, records the Sandbox's host key, and
// writes its Host entry to orcasm's ssh config. Calling it again refreshes
// the entry (e.g. after the Sandbox was recreated with a new host key).
func (a Access) Add(ctx context.Context, name string) error {
	if err := os.MkdirAll(a.Dir, 0o700); err != nil {
		return err
	}
	if err := a.ensureKey(); err != nil {
		return err
	}
	if err := a.MSB.AuthorizeSSHKey(ctx, a.keyPath()+".pub"); err != nil {
		return fmt.Errorf("authorize orcasm key in msb: %w", err)
	}
	hostKey, err := a.MSB.SandboxHostKey(ctx, name)
	if err != nil {
		return fmt.Errorf("read host key of sandbox %s: %w", name, err)
	}
	alias := HostAlias(name)
	if err := setKnownHost(a.knownHostsPath(), alias, alias+" "+strings.TrimSpace(hostKey)+"\n"); err != nil {
		return err
	}
	if err := writeFile(a.gitConfigPath(), a.gitConfig()); err != nil {
		return err
	}
	return setEntry(a.configPath(), alias, a.hostBlock(name))
}

// setKnownHost replaces every known_hosts line for alias with line (or
// removes them when line is empty).
func setKnownHost(path, alias, line string) error {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) && line == "" {
		return nil
	}
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	var out strings.Builder
	for _, l := range strings.Split(string(data), "\n") {
		if l == "" || strings.HasPrefix(l, alias+" ") {
			continue
		}
		out.WriteString(l + "\n")
	}
	out.WriteString(line)
	return writeFile(path, out.String())
}

// Remove forgets the Sandbox: its Host entry and pinned host key are dropped.
// orcasm's key stays authorized in msb (msb has no revoke and the key is
// shared by all Sandboxes). Removing an unknown Sandbox is a no-op.
func (a Access) Remove(name string) error {
	alias := HostAlias(name)
	if err := setEntry(a.configPath(), alias, ""); err != nil {
		return err
	}
	return setKnownHost(a.knownHostsPath(), alias, "")
}

// ensureKey generates orcasm's ed25519 key pair with ssh-keygen unless it
// already exists.
func (a Access) ensureKey() error {
	if _, err := os.Stat(a.keyPath()); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	out, err := exec.Command("ssh-keygen", "-q", "-t", "ed25519", "-N", "",
		"-C", "orcasm", "-f", a.keyPath()).CombinedOutput()
	if err != nil {
		return fmt.Errorf("ssh-keygen: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func (a Access) hostBlock(name string) string {
	return fmt.Sprintf(`Host %s
  User dev
  ProxyCommand msb ssh serve %s --stdio --no-inactivity-timeout
  IdentityFile %s
  IdentitiesOnly yes
  UserKnownHostsFile %s
  StrictHostKeyChecking yes
  ServerAliveInterval 30
`, HostAlias(name), name, quote(a.keyPath()), quote(a.knownHostsPath()))
}

// setEntry replaces (or appends) the `Host <alias>` block in an
// orcasm-owned ssh config file.
func setEntry(path, alias, block string) error {
	blocks, err := readBlocks(path)
	if err != nil {
		return err
	}
	if blocks == nil && block == "" {
		return nil
	}
	out := []string{}
	for _, b := range blocks {
		if blockAlias(b) != alias {
			out = append(out, b)
		}
	}
	if block != "" {
		out = append(out, block)
	}
	return writeFile(path, strings.Join(out, "\n"))
}

// readBlocks splits an orcasm-owned ssh config into Host blocks.
func readBlocks(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var blocks []string
	var cur strings.Builder
	sc := bufio.NewScanner(strings.NewReader(string(data)))
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "Host ") && cur.Len() > 0 {
			blocks = append(blocks, cur.String())
			cur.Reset()
		}
		if strings.TrimSpace(line) == "" {
			continue
		}
		cur.WriteString(line + "\n")
	}
	if cur.Len() > 0 {
		blocks = append(blocks, cur.String())
	}
	return blocks, sc.Err()
}

func blockAlias(block string) string {
	first, _, _ := strings.Cut(block, "\n")
	return strings.TrimSpace(strings.TrimPrefix(first, "Host "))
}

// quote quotes a path for ssh_config if it contains whitespace.
func quote(p string) string {
	if strings.ContainsAny(p, " \t") {
		return `"` + p + `"`
	}
	return p
}

// writeFile atomically replaces path with content (mode 0600).
func writeFile(path, content string) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.WriteString(content); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
