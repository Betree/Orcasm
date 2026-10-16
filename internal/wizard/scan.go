package wizard

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
)

// scan is what the Wizard reads from the Project folder to offer choices.
type scan struct {
	// composeFiles are the Compose-looking files at the Project root.
	composeFiles []string
	// defaultComposeFiles are the files `docker compose` loads by default.
	defaultComposeFiles []string
	// ignored are gitignored, untracked paths at the Project root; directories
	// end with '/'.
	ignored []string
}

// Files `docker compose` loads by default, in its order of preference, each
// with its override file.
var standardComposeFiles = [][2]string{
	{"compose.yaml", "compose.override.yaml"},
	{"compose.yml", "compose.override.yml"},
	{"docker-compose.yaml", "docker-compose.override.yaml"},
	{"docker-compose.yml", "docker-compose.override.yml"},
}

func scanProject(folder string) scan {
	var s scan
	for _, pattern := range []string{"*compose*.yaml", "*compose*.yml"} {
		matches, _ := filepath.Glob(filepath.Join(folder, pattern))
		for _, m := range matches {
			s.composeFiles = append(s.composeFiles, filepath.Base(m))
		}
	}
	slices.Sort(s.composeFiles)
	for _, std := range standardComposeFiles {
		if slices.Contains(s.composeFiles, std[0]) {
			s.defaultComposeFiles = []string{std[0]}
			if slices.Contains(s.composeFiles, std[1]) {
				s.defaultComposeFiles = append(s.defaultComposeFiles, std[1])
			}
			break
		}
	}
	out, err := exec.Command("git", "-C", folder, "ls-files", "--others", "--ignored",
		"--exclude-standard", "--directory", "--no-empty-directory").Output()
	if err == nil {
		for line := range strings.Lines(string(out)) {
			if line = strings.TrimSpace(line); line != "" {
				s.ignored = append(s.ignored, line)
			}
		}
	}
	return s
}

// defaultBringFiles are the env files among the gitignored paths: the usual
// thing a Project needs that git doesn't carry.
func (s scan) defaultBringFiles() []string {
	var files []string
	for _, p := range s.ignored {
		if strings.HasPrefix(p, ".env") && !strings.HasSuffix(p, "/") {
			files = append(files, p)
		}
	}
	return files
}

// composeServices lists the services defined across the given Compose files.
func composeServices(folder string, files []string) ([]string, error) {
	var services []string
	for _, f := range files {
		data, err := os.ReadFile(filepath.Join(folder, f))
		if err != nil {
			return nil, fmt.Errorf("compose file %s: %w", f, err)
		}
		var doc struct {
			Services map[string]yaml.Node `yaml:"services"`
		}
		if err := yaml.Unmarshal(data, &doc); err != nil {
			return nil, fmt.Errorf("compose file %s: %w", f, err)
		}
		for name := range doc.Services {
			if !slices.Contains(services, name) {
				services = append(services, name)
			}
		}
	}
	slices.Sort(services)
	return services, nil
}
