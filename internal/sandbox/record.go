package sandbox

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"

	"github.com/Betree/Orcasm/internal/msb"
	"github.com/Betree/Orcasm/internal/wizard"
)

// The Config a Sandbox was created from is kept on the host in
// <dir>/<name>.toml (dir is normally ~/.config/orcasm/sandboxes). It holds
// secret specs, never values: starting the Sandbox again reads each value
// from the host with them.

func configPath(dir, name string) string { return filepath.Join(dir, name+".toml") }

// SaveConfig keeps cfg as the Config of Sandbox cfg.Name.
func SaveConfig(dir string, cfg wizard.Config) error {
	var buf bytes.Buffer
	if err := toml.NewEncoder(&buf).Encode(cfg); err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	return os.WriteFile(configPath(dir, cfg.Name), buf.Bytes(), 0o600)
}

// LoadConfig returns the Config Sandbox name was created from.
func LoadConfig(dir, name string) (wizard.Config, error) {
	var cfg wizard.Config
	if _, err := toml.DecodeFile(configPath(dir, name), &cfg); err != nil {
		return wizard.Config{}, fmt.Errorf("config of Sandbox %s: %w", name, err)
	}
	return cfg, nil
}

// RemoveConfig forgets Sandbox name's Config and Installed record; it is a
// no-op if there are none.
func RemoveConfig(dir, name string) error {
	for _, p := range []string{configPath(dir, name), installedPath(dir, name)} {
		if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

// Installed records, on the host, what create and upgrade last installed in
// a Sandbox, so the daily upgrade notice can tell an upgrade is pending
// without exec'ing into it (which boots a stopped one). It lives next to the
// Config, in <dir>/<name>.installed.toml.
type Installed struct {
	// Orca is the Orca release (orca-ide package version).
	Orca string `toml:"orca"`
}

func installedPath(dir, name string) string { return filepath.Join(dir, name+".installed.toml") }

// SaveInstalled records what is installed in Sandbox name.
func SaveInstalled(dir, name string, in Installed) error {
	var buf bytes.Buffer
	if err := toml.NewEncoder(&buf).Encode(in); err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	return os.WriteFile(installedPath(dir, name), buf.Bytes(), 0o600)
}

// LoadInstalled reads what is recorded as installed in Sandbox name; with no
// record (a Sandbox made before it existed) nothing is.
func LoadInstalled(dir, name string) (Installed, error) {
	var in Installed
	_, err := toml.DecodeFile(installedPath(dir, name), &in)
	if errors.Is(err, os.ErrNotExist) {
		return Installed{}, nil
	}
	if err != nil {
		return Installed{}, fmt.Errorf("installed versions of Sandbox %s: %w", name, err)
	}
	return in, nil
}

// Secrets are the secrets `msb start` needs for Sandbox name: each kept
// secret spec valued from the host. A Sandbox without a kept Config has no
// secrets.
func Secrets(dir, name string, value SecretValue) ([]msb.Secret, error) {
	cfg, err := LoadConfig(dir, name)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return valued(cfg.Secrets, value)
}

// valued gives each secret spec its host value.
func valued(specs []wizard.Secret, value SecretValue) ([]msb.Secret, error) {
	var out []msb.Secret
	for _, s := range specs {
		v, err := value(s)
		if err != nil {
			return nil, fmt.Errorf("secret %s: %w", s.Name, err)
		}
		out = append(out, msb.Secret{Name: s.Name, Hosts: s.Hosts, Value: v})
	}
	return out, nil
}
