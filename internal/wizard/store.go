package wizard

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// ProjectFile is the committable file at the Project root holding the
// project-level answers.
const ProjectFile = "orcasm.toml"

// machineFile, in the config dir, holds the machine-level answers.
const machineFile = "config.toml"

// machineAnswers are the answers that belong to this host rather than to the
// Project: how big Sandboxes are, which harnesses to install and which host
// tokens may be shared (by name only: token values are never saved).
type machineAnswers struct {
	Resources Resources   `toml:"resources"`
	Harnesses []string    `toml:"harnesses"`
	Secrets   []string    `toml:"secrets"`
	Git       GitIdentity `toml:"git,omitempty"`
}

// savedProject is the Project as written to orcasm.toml. Lists are always
// written, so that an empty list (e.g. no files to bring) is remembered.
type savedProject struct {
	ComposeFiles []string `toml:"compose_files"`
	Workbench    string   `toml:"workbench,omitempty"`
	WebService   string   `toml:"web_service,omitempty"`
	WebPort      int      `toml:"web_port,omitempty"`
	BringFiles   []string `toml:"bring_files"`
}

// loadSaved returns the saved answers for this folder and config dir, as a
// partial Config (missing files give zero values).
func loadSaved(folder, configDir string) (Config, error) {
	var saved Config
	var m machineAnswers
	if err := decodeIfExists(filepath.Join(configDir, machineFile), &m); err != nil {
		return Config{}, err
	}
	saved.Resources, saved.Harnesses, saved.Git = m.Resources, m.Harnesses, m.Git
	for _, name := range m.Secrets {
		saved.Secrets = append(saved.Secrets, Secret{Name: name})
	}
	var p savedProject
	if err := decodeIfExists(filepath.Join(folder, ProjectFile), &p); err != nil {
		return Config{}, err
	}
	saved.Project = Project(p)
	return saved, nil
}

func decodeIfExists(path string, v any) error {
	_, err := toml.DecodeFile(path, v)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("reading %s: %w", path, err)
	}
	return nil
}

// save writes cfg's answers as defaults for the next run.
func save(cfg Config, configDir string) error {
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		return err
	}
	harnesses := cfg.Harnesses
	if harnesses == nil {
		harnesses = []string{}
	}
	secrets := []string{}
	for _, s := range cfg.Secrets {
		secrets = append(secrets, s.Name)
	}
	if err := encode(filepath.Join(configDir, machineFile), machineAnswers{cfg.Resources, harnesses, secrets, cfg.Git}); err != nil {
		return err
	}
	p := savedProject(cfg.Project)
	if p.ComposeFiles == nil {
		p.ComposeFiles = []string{}
	}
	if p.BringFiles == nil {
		p.BringFiles = []string{}
	}
	return encode(filepath.Join(cfg.Folder, ProjectFile), p)
}

func encode(path string, v any) error {
	var buf bytes.Buffer
	if err := toml.NewEncoder(&buf).Encode(v); err != nil {
		return err
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		return fmt.Errorf("saving answers: %w", err)
	}
	return nil
}
