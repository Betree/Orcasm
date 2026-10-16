package sshaccess

import (
	"os"
	"path/filepath"
	"strings"
)

const linkComment = "# Added by orcasm: SSH access to orcasm Sandboxes (orcasm-<name>)"

func (a Access) userConfigPath() string { return filepath.Join(a.UserSSHDir, "config") }

func (a Access) includeLine() string { return "Include " + quote(a.configPath()) }

// UserConfigLinked reports whether the user's ssh config already includes
// orcasm's config.
func (a Access) UserConfigLinked() (bool, error) {
	data, err := os.ReadFile(a.userConfigPath())
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	for _, l := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(l) == a.includeLine() {
			return true, nil
		}
	}
	return false, nil
}

// LinkUserConfig adds `Include <orcasm ssh config>` to the top of the user's
// ssh config (so VS Code Remote-SSH lists orcasm-<name> hosts), creating the
// file if needed. Only call it with the user's consent. It is idempotent and
// leaves the rest of the file, its mode and any symlink untouched.
func (a Access) LinkUserConfig() error {
	if linked, err := a.UserConfigLinked(); err != nil || linked {
		return err
	}
	if err := os.MkdirAll(a.UserSSHDir, 0o700); err != nil {
		return err
	}
	path := a.userConfigPath()
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	// Include goes first: after a Host line ssh would scope it to that block.
	head := linkComment + "\n" + a.includeLine() + "\n\n"
	return os.WriteFile(path, append([]byte(head), data...), 0o600)
}

// UnlinkUserConfig removes what LinkUserConfig added. It is a no-op when the
// user's config does not include orcasm's.
func (a Access) UnlinkUserConfig() error {
	path := a.userConfigPath()
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	head := linkComment + "\n" + a.includeLine() + "\n\n"
	s := string(data)
	if strings.HasPrefix(s, head) {
		s = strings.TrimPrefix(s, head)
	} else {
		var kept []string
		for _, l := range strings.Split(s, "\n") {
			if t := strings.TrimSpace(l); t != a.includeLine() && t != linkComment {
				kept = append(kept, l)
			}
		}
		s = strings.Join(kept, "\n")
	}
	if s == string(data) {
		return nil
	}
	return os.WriteFile(path, []byte(s), 0o600)
}
