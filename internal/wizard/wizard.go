package wizard

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/BurntSushi/toml"
)

// Host is what the Wizard knows about the machine it runs on. NewHost probes
// the real one; tests pass fakes.
type Host struct {
	Cwd       string
	CPUs      int
	MemoryGiB int
	// Harnesses are the supported harnesses found installed on the host.
	Harnesses     []string
	SandboxExists func(name string) bool
	PortFree      func(port int) bool
}

// Options configures one Wizard run.
type Options struct {
	Host Host
	// ConfigDir holds machine-level answers (normally ~/.config/orcasm).
	ConfigDir string
	// AnswersFile, when set, makes the run non-interactive: its answers
	// override defaults and nothing is prompted.
	AnswersFile string
}

const (
	firstHostPort     = 6768
	hostReservedGiB   = 8
	defaultRootDisk   = 32
	defaultDockerDisk = 100
)

// Run collects a Config.
func Run(opts Options) (Config, error) {
	var answers Config
	if opts.AnswersFile != "" {
		if _, err := toml.DecodeFile(opts.AnswersFile, &answers); err != nil {
			return Config{}, fmt.Errorf("reading answers file: %w", err)
		}
	}
	h := opts.Host
	folder := resolveFolder(h.Cwd, answers.Folder)
	cfg := defaults(h, folder, scanProject(folder))
	overlay(&cfg, answers)
	if err := validate(cfg, h); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func resolveFolder(cwd, folder string) string {
	if folder == "" {
		return cwd
	}
	if !filepath.IsAbs(folder) {
		folder = filepath.Join(cwd, folder)
	}
	return filepath.Clean(folder)
}

func defaults(h Host, folder string, s scan) Config {
	return Config{
		Folder: folder,
		Name:   sanitizeName(filepath.Base(folder)),
		Resources: Resources{
			CPUs:          max(1, h.CPUs/2),
			MemoryGiB:     max(1, min(h.MemoryGiB/2, h.MemoryGiB-hostReservedGiB)),
			RootDiskGiB:   defaultRootDisk,
			DockerDiskGiB: defaultDockerDisk,
		},
		HostPort:  firstFreePort(h.PortFree),
		Harnesses: h.Harnesses,
		Project: Project{
			ComposeFiles: s.defaultComposeFiles,
			BringFiles:   s.defaultBringFiles(),
		},
	}
}

// overlay copies every answer that was given (non-zero) over cfg.
func overlay(cfg *Config, a Config) {
	set(&cfg.Name, a.Name)
	set(&cfg.Resources.CPUs, a.Resources.CPUs)
	set(&cfg.Resources.MemoryGiB, a.Resources.MemoryGiB)
	set(&cfg.Resources.RootDiskGiB, a.Resources.RootDiskGiB)
	set(&cfg.Resources.DockerDiskGiB, a.Resources.DockerDiskGiB)
	set(&cfg.HostPort, a.HostPort)
	if a.Harnesses != nil {
		cfg.Harnesses = a.Harnesses
	}
	if a.Project.ComposeFiles != nil {
		cfg.Project.ComposeFiles = a.Project.ComposeFiles
	}
	set(&cfg.Project.Workbench, a.Project.Workbench)
	set(&cfg.Project.WebService, a.Project.WebService)
	set(&cfg.Project.WebPort, a.Project.WebPort)
	if a.Project.BringFiles != nil {
		cfg.Project.BringFiles = a.Project.BringFiles
	}
}

func set[T comparable](dst *T, v T) {
	var zero T
	if v != zero {
		*dst = v
	}
}

var validName = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)

func sanitizeName(s string) string {
	s = strings.ToLower(s)
	s = regexp.MustCompile(`[^a-z0-9._-]+`).ReplaceAllString(s, "-")
	return strings.TrimLeft(s, "-._")
}

func validate(cfg Config, h Host) error {
	var errs []error
	if st, err := os.Stat(cfg.Folder); err != nil || !st.IsDir() {
		errs = append(errs, fmt.Errorf("folder %s is not a directory", cfg.Folder))
	}
	switch {
	case !validName.MatchString(cfg.Name):
		errs = append(errs, fmt.Errorf("sandbox name %q: use lowercase letters, digits, '.', '_' and '-'", cfg.Name))
	case h.SandboxExists(cfg.Name):
		errs = append(errs, fmt.Errorf("a Sandbox named %q already exists", cfg.Name))
	}
	if r := cfg.Resources; r.CPUs < 1 || r.CPUs > h.CPUs {
		errs = append(errs, fmt.Errorf("cpus must be between 1 and %d", h.CPUs))
	}
	if r := cfg.Resources; r.MemoryGiB < 1 || r.MemoryGiB >= h.MemoryGiB {
		errs = append(errs, fmt.Errorf("memory must be between 1 and %d GiB", h.MemoryGiB-1))
	}
	if r := cfg.Resources; r.RootDiskGiB < 1 || r.DockerDiskGiB < 1 {
		errs = append(errs, errors.New("disk sizes must be at least 1 GiB"))
	}
	switch {
	case cfg.HostPort < 1024 || cfg.HostPort > 65535:
		errs = append(errs, fmt.Errorf("host port %d must be between 1024 and 65535", cfg.HostPort))
	case !h.PortFree(cfg.HostPort):
		errs = append(errs, fmt.Errorf("host port %d is already in use", cfg.HostPort))
	}
	for _, name := range cfg.Harnesses {
		if !slices.Contains(SupportedHarnesses, name) {
			errs = append(errs, fmt.Errorf("unknown harness %q (supported: %s)", name, strings.Join(SupportedHarnesses, ", ")))
		}
	}
	errs = append(errs, validateProject(cfg.Folder, cfg.Project)...)
	return errors.Join(errs...)
}

func validateProject(folder string, p Project) []error {
	var errs []error
	services, err := composeServices(folder, p.ComposeFiles)
	if err != nil {
		return []error{err}
	}
	for _, svc := range []struct{ role, name string }{{"workbench", p.Workbench}, {"web service", p.WebService}} {
		if svc.name != "" && !slices.Contains(services, svc.name) {
			errs = append(errs, fmt.Errorf("%s %q is not a service of the compose files %v", svc.role, svc.name, p.ComposeFiles))
		}
	}
	if p.WebService != "" && (p.WebPort < 1 || p.WebPort > 65535) {
		errs = append(errs, fmt.Errorf("web service %q needs its web port", p.WebService))
	}
	for _, f := range p.BringFiles {
		if !filepath.IsLocal(f) {
			errs = append(errs, fmt.Errorf("file to bring %q must be inside the Project folder", f))
		} else if _, err := os.Stat(filepath.Join(folder, f)); err != nil {
			errs = append(errs, fmt.Errorf("file to bring %q: %w", f, err))
		}
	}
	return errs
}

func firstFreePort(free func(int) bool) int {
	for p := firstHostPort; p < 65536; p++ {
		if free(p) {
			return p
		}
	}
	return 0
}
