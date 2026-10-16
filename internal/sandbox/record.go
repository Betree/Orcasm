package sandbox

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"

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

// RemoveConfig forgets Sandbox name's Config; it is a no-op if there is none.
func RemoveConfig(dir, name string) error {
	if err := os.Remove(configPath(dir, name)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
