package wizard_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/Betree/Orcasm/internal/wizard"
)

// fakeHost is a 16-CPU / 32 GiB host with claude and codex installed, no
// existing Sandboxes and every port free.
func fakeHost(cwd string) wizard.Host {
	return wizard.Host{
		Cwd:           cwd,
		CPUs:          16,
		MemoryGiB:     32,
		Harnesses:     []string{"claude", "codex"},
		SandboxExists: func(string) bool { return false },
		PortFree:      func(int) bool { return true },
	}
}

func newProject(t *testing.T, name string) string {
	t.Helper()
	return newProjectAt(t, filepath.Join(t.TempDir(), name))
}

func newProjectAt(t *testing.T, dir string) string {
	t.Helper()
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

func writeFile(t *testing.T, path, content string) string {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// runWith runs the Wizard non-interactively with the given answers file content.
func runWith(t *testing.T, host wizard.Host, configDir, answers string) (wizard.Config, error) {
	t.Helper()
	file := writeFile(t, filepath.Join(t.TempDir(), "answers.toml"), answers)
	return wizard.Run(wizard.Options{Host: host, ConfigDir: configDir, AnswersFile: file})
}

func assertConfig(t *testing.T, got, want wizard.Config) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got  %+v\nwant %+v", got, want)
	}
}

func TestEmptyAnswersTakeDefaultsFromTheHost(t *testing.T) {
	dir := newProject(t, "my-app")
	cfg, err := runWith(t, fakeHost(dir), t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	assertConfig(t, cfg, wizard.Config{
		Folder:    dir,
		Name:      "my-app",
		Resources: wizard.Resources{CPUs: 8, MemoryGiB: 16, RootDiskGiB: 32, DockerDiskGiB: 100},
		HostPort:  6768,
		Harnesses: []string{"claude", "codex"},
	})
}

func TestDefaultMemoryIsHalfTheHostButLeavesAtLeast8GiB(t *testing.T) {
	for _, tc := range []struct{ host, want int }{{64, 32}, {16, 8}, {12, 4}} {
		h := fakeHost(newProject(t, "app"))
		h.MemoryGiB = tc.host
		cfg, err := runWith(t, h, t.TempDir(), "")
		if err != nil {
			t.Fatal(err)
		}
		if cfg.Resources.MemoryGiB != tc.want {
			t.Errorf("host %d GiB: got %d GiB, want %d", tc.host, cfg.Resources.MemoryGiB, tc.want)
		}
	}
}

func TestDefaultHostPortIsFirstFreeFrom6768(t *testing.T) {
	h := fakeHost(newProject(t, "app"))
	h.PortFree = func(p int) bool { return p > 6769 }
	cfg, err := runWith(t, h, t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.HostPort != 6770 {
		t.Fatalf("got port %d, want 6770", cfg.HostPort)
	}
}

func TestAnswersFileOverridesDefaults(t *testing.T) {
	cwd := newProject(t, "elsewhere")
	parent := filepath.Dir(cwd)
	newProjectAt(t, filepath.Join(parent, "other"))
	cfg, err := runWith(t, fakeHost(cwd), t.TempDir(), `
folder = "../other"
name = "custom"
host_port = 7000
harnesses = ["opencode"]
[resources]
cpus = 2
memory_gib = 4
`)
	if err != nil {
		t.Fatal(err)
	}
	assertConfig(t, cfg, wizard.Config{
		Folder:    filepath.Join(parent, "other"),
		Name:      "custom",
		Resources: wizard.Resources{CPUs: 2, MemoryGiB: 4, RootDiskGiB: 32, DockerDiskGiB: 100},
		HostPort:  7000,
		Harnesses: []string{"opencode"},
	})
}

func TestNameDefaultsToTheChosenFolder(t *testing.T) {
	cwd := newProject(t, "cwd")
	other := newProjectAt(t, filepath.Join(filepath.Dir(cwd), "picked"))
	cfg, err := runWith(t, fakeHost(cwd), t.TempDir(), `folder = "`+other+`"`)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Name != "picked" {
		t.Fatalf("got name %q, want %q", cfg.Name, "picked")
	}
}

func TestRefusesAnExistingSandboxName(t *testing.T) {
	h := fakeHost(newProject(t, "app"))
	h.SandboxExists = func(name string) bool { return name == "taken" }
	if _, err := runWith(t, h, t.TempDir(), `name = "taken"`); err == nil {
		t.Fatal("want an error for a name collision")
	}
	if _, err := runWith(t, h, t.TempDir(), `name = "free"`); err != nil {
		t.Fatal(err)
	}
}

func TestRefusesInvalidAnswers(t *testing.T) {
	for _, answers := range []string{
		`folder = "/does/not/exist"`,
		`name = "has space"`,
		`harnesses = ["vim"]`,
		`host_port = 80`,
		`[resources]` + "\n" + `cpus = 99`,
		`[resources]` + "\n" + `memory_gib = 64`,
	} {
		if _, err := runWith(t, fakeHost(newProject(t, "app")), t.TempDir(), answers); err == nil {
			t.Errorf("want an error for answers %q", answers)
		}
	}
}

// composeProject is a git Project with a Compose file and gitignored files.
func composeProject(t *testing.T) string {
	t.Helper()
	dir := newProject(t, "shop")
	writeFile(t, filepath.Join(dir, "compose.yaml"), "services:\n  web:\n    image: nginx\n  dev:\n    image: node\n")
	writeFile(t, filepath.Join(dir, "compose.test.yml"), "services:\n  db:\n    image: postgres\n")
	writeFile(t, filepath.Join(dir, ".gitignore"), ".env\n.env.local\nnode_modules/\n")
	writeFile(t, filepath.Join(dir, ".env"), "A=1\n")
	writeFile(t, filepath.Join(dir, ".env.local"), "B=2\n")
	if err := os.Mkdir(filepath.Join(dir, "node_modules"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "node_modules", "x.js"), "")
	git := exec.Command("git", "init", "-q", dir)
	if out, err := git.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	return dir
}

func TestDefaultsComeFromTheProjectFiles(t *testing.T) {
	dir := composeProject(t)
	cfg, err := runWith(t, fakeHost(dir), t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	want := wizard.Project{
		ComposeFiles: []string{"compose.yaml"},
		BringFiles:   []string{".env", ".env.local"},
	}
	if !reflect.DeepEqual(cfg.Project, want) {
		t.Fatalf("got  %+v\nwant %+v", cfg.Project, want)
	}
}

func TestProjectAnswersMustMatchTheComposeFiles(t *testing.T) {
	dir := composeProject(t)
	for _, answers := range []string{
		`[project]` + "\n" + `compose_files = ["missing.yaml"]`,
		`[project]` + "\n" + `workbench = "nope"`,
		`[project]` + "\n" + `workbench = "db"`, // db is only in compose.test.yml, not picked
		`[project]` + "\n" + `web_service = "web"`, // no web_port
		`[project]` + "\n" + `bring_files = ["../outside"]`,
	} {
		if _, err := runWith(t, fakeHost(dir), t.TempDir(), answers); err == nil {
			t.Errorf("want an error for answers %q", answers)
		}
	}
	cfg, err := runWith(t, fakeHost(dir), t.TempDir(), `
[project]
compose_files = ["compose.yaml", "compose.test.yml"]
workbench = "dev"
web_service = "web"
web_port = 3000
bring_files = [".env"]
`)
	if err != nil {
		t.Fatal(err)
	}
	want := wizard.Project{
		ComposeFiles: []string{"compose.yaml", "compose.test.yml"},
		Workbench:    "dev",
		WebService:   "web",
		WebPort:      3000,
		BringFiles:   []string{".env"},
	}
	if !reflect.DeepEqual(cfg.Project, want) {
		t.Fatalf("got  %+v\nwant %+v", cfg.Project, want)
	}
}

func TestRefusesABusyHostPort(t *testing.T) {
	h := fakeHost(newProject(t, "app"))
	h.PortFree = func(p int) bool { return p != 7000 }
	if _, err := runWith(t, h, t.TempDir(), `host_port = 7000`); err == nil {
		t.Fatal("want an error for a busy port")
	}
}
