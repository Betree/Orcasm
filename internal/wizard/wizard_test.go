package wizard_test

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
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
		FirstFreePort: func(start int) (int, error) { return start, nil },
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
	h.FirstFreePort = func(start int) (int, error) { return max(start, 6770), nil }
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
		`[project]` + "\n" + `workbench = "db"`,    // db is only in compose.test.yml, not picked
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

func TestAnswersAreSavedAsDefaultsForTheNextRun(t *testing.T) {
	dir := composeProject(t)
	configDir := t.TempDir()
	first, err := runWith(t, fakeHost(dir), configDir, `
name = "shop-1"
harnesses = ["opencode"]
[resources]
cpus = 4
memory_gib = 12
root_disk_gib = 40
docker_disk_gib = 200
[project]
workbench = "dev"
web_service = "web"
web_port = 3000
bring_files = [".env.local"]
`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "orcasm.toml")); err != nil {
		t.Fatalf("want project answers in orcasm.toml: %v", err)
	}
	if _, err := os.Stat(filepath.Join(configDir, "config.toml")); err != nil {
		t.Fatalf("want machine answers in the config dir: %v", err)
	}

	second, err := runWith(t, fakeHost(dir), configDir, "")
	if err != nil {
		t.Fatal(err)
	}
	want := first
	want.Name = "shop" // the Sandbox name is per Sandbox, not saved
	assertConfig(t, second, want)
}

func TestProjectAnswersComeFromACommittedOrcasmToml(t *testing.T) {
	dir := composeProject(t)
	writeFile(t, filepath.Join(dir, "orcasm.toml"), "workbench = \"dev\"\nbring_files = []\n")
	cfg, err := runWith(t, fakeHost(dir), t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Project.Workbench != "dev" || len(cfg.Project.BringFiles) != 0 {
		t.Fatalf("got %+v, want workbench dev and no files to bring", cfg.Project)
	}
}

func TestInteractiveRunAcceptsDefaultsAndTakesTypedAnswers(t *testing.T) {
	dir := newProject(t, "my-app")
	var out strings.Builder
	// Without a terminal the Wizard asks line by line. Answer: folder (keep),
	// name "typed", then keep every other default.
	in := strings.NewReader("\ntyped\n" + strings.Repeat("\n", 20))
	cfg, err := wizard.Run(wizard.Options{Host: fakeHost(dir), ConfigDir: t.TempDir(), In: in, Out: &out})
	if err != nil {
		t.Fatalf("%v\n%s", err, out.String())
	}
	assertConfig(t, cfg, wizard.Config{
		Folder:    dir,
		Name:      "typed",
		Resources: wizard.Resources{CPUs: 8, MemoryGiB: 16, RootDiskGiB: 32, DockerDiskGiB: 100},
		HostPort:  6768,
		Harnesses: []string{"claude", "codex"},
	})
}

func TestPipedInteractiveRunFailsOnInvalidAnswersInsteadOfAskingAgain(t *testing.T) {
	h := fakeHost(newProject(t, "app"))
	h.SandboxExists = func(string) bool { return true }
	_, err := wizard.Run(wizard.Options{Host: h, ConfigDir: t.TempDir(), In: strings.NewReader(""), Out: io.Discard})
	if err == nil {
		t.Fatal("want an error for a name collision")
	}
}

func TestRefusesABusyHostPort(t *testing.T) {
	h := fakeHost(newProject(t, "app"))
	h.FirstFreePort = func(start int) (int, error) { return start + 1, nil }
	if _, err := runWith(t, h, t.TempDir(), `host_port = 7000`); err == nil {
		t.Fatal("want an error for a busy port")
	}
}

// withEnv makes the fake host's environment hold exactly env.
func withEnv(h wizard.Host, env map[string]string) wizard.Host {
	h.Getenv = func(k string) string { return env[k] }
	return h
}

func TestApprovedAPIKeyFromTheEnvironmentBecomesAHostScopedSecret(t *testing.T) {
	h := withEnv(fakeHost(newProject(t, "app")), map[string]string{"ANTHROPIC_API_KEY": "sk-ant-real"})
	cfg, err := runWith(t, h, t.TempDir(), "[[secrets]]\nname = \"ANTHROPIC_API_KEY\"\n")
	if err != nil {
		t.Fatal(err)
	}
	want := []wizard.Secret{{
		Name:   "ANTHROPIC_API_KEY",
		Hosts:  []string{"api.anthropic.com"},
		Source: wizard.Source{Env: "ANTHROPIC_API_KEY"},
	}}
	if !reflect.DeepEqual(cfg.Secrets, want) {
		t.Fatalf("got  %+v\nwant %+v", cfg.Secrets, want)
	}
	if v, err := h.SecretValue(cfg.Secrets[0]); err != nil || v != "sk-ant-real" {
		t.Fatalf("got value %q (%v), want the host's key", v, err)
	}
}

func TestTokensFoundOnTheHostAreNotSharedWithoutApproval(t *testing.T) {
	h := withEnv(fakeHost(newProject(t, "app")), map[string]string{"ANTHROPIC_API_KEY": "sk-ant-real"})
	cfg, err := runWith(t, h, t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Secrets) != 0 {
		t.Fatalf("got secrets %+v, want none", cfg.Secrets)
	}
}

// withCommands makes the fake host's commands print the given output (keyed
// by the command line); any other command fails.
func withCommands(h wizard.Host, outputs map[string]string) wizard.Host {
	h.Output = func(name string, args ...string) (string, error) {
		line := strings.Join(append([]string{name}, args...), " ")
		if out, ok := outputs[line]; ok {
			return out, nil
		}
		return "", fmt.Errorf("%s: not found", line)
	}
	return h
}

func TestApprovedGitHubCLITokenIsScopedToGitHub(t *testing.T) {
	h := withCommands(fakeHost(newProject(t, "app")), map[string]string{"gh auth token": "gho_real\n"})
	cfg, err := runWith(t, h, t.TempDir(), "[[secrets]]\nname = \"GH_TOKEN\"\n")
	if err != nil {
		t.Fatal(err)
	}
	want := []wizard.Secret{{
		Name:   "GH_TOKEN",
		Hosts:  []string{"github.com", "api.github.com", "uploads.github.com"},
		Source: wizard.Source{Command: []string{"gh", "auth", "token"}},
	}}
	if !reflect.DeepEqual(cfg.Secrets, want) {
		t.Fatalf("got  %+v\nwant %+v", cfg.Secrets, want)
	}
	if v, err := h.SecretValue(cfg.Secrets[0]); err != nil || v != "gho_real" {
		t.Fatalf("got value %q (%v), want the gh token", v, err)
	}
}

func TestApprovingATokenTheHostDoesNotHaveFails(t *testing.T) {
	h := fakeHost(newProject(t, "app"))
	for _, name := range []string{"GH_TOKEN", "ANTHROPIC_API_KEY", "MADE_UP"} {
		if _, err := runWith(t, h, t.TempDir(), "[[secrets]]\nname = \""+name+"\"\n"); err == nil {
			t.Errorf("want an error approving %s", name)
		}
	}
}

// withOpenCodeAuth gives the fake host an OpenCode auth.json.
func withOpenCodeAuth(t *testing.T, h wizard.Host, content string) wizard.Host {
	h.OpenCodeAuthFile = writeFile(t, filepath.Join(t.TempDir(), "auth.json"), content)
	return h
}

const openCodeAuth = `{
  "openrouter": {"type": "api", "key": "sk-or-real"},
  "anthropic": {"type": "api", "key": "sk-ant-from-opencode"},
  "openai": {"type": "oauth", "access": "at", "refresh": "rt", "expires": 1}
}`

func TestAPIKeysStoredByOpenCodeCanBeShared(t *testing.T) {
	h := withOpenCodeAuth(t, fakeHost(newProject(t, "app")), openCodeAuth)
	cfg, err := runWith(t, h, t.TempDir(), "[[secrets]]\nname = \"OPENROUTER_API_KEY\"\n")
	if err != nil {
		t.Fatal(err)
	}
	want := []wizard.Secret{{
		Name:   "OPENROUTER_API_KEY",
		Hosts:  []string{"openrouter.ai"},
		Source: wizard.Source{OpenCodeAuth: "openrouter"},
	}}
	if !reflect.DeepEqual(cfg.Secrets, want) {
		t.Fatalf("got  %+v\nwant %+v", cfg.Secrets, want)
	}
	if v, err := h.SecretValue(cfg.Secrets[0]); err != nil || v != "sk-or-real" {
		t.Fatalf("got value %q (%v), want the OpenCode key", v, err)
	}
}

func TestOpenCodeSubscriptionLoginsAreNeverShared(t *testing.T) {
	h := withOpenCodeAuth(t, fakeHost(newProject(t, "app")), openCodeAuth)
	if _, err := runWith(t, h, t.TempDir(), "[[secrets]]\nname = \"OPENAI_API_KEY\"\n"); err == nil {
		t.Fatal("want an error: OpenCode only holds an OAuth login for openai")
	}
}

func TestEnvironmentKeyIsPreferredOverOpenCodes(t *testing.T) {
	h := withOpenCodeAuth(t, fakeHost(newProject(t, "app")), openCodeAuth)
	h = withEnv(h, map[string]string{"ANTHROPIC_API_KEY": "sk-ant-env"})
	cfg, err := runWith(t, h, t.TempDir(), "[[secrets]]\nname = \"ANTHROPIC_API_KEY\"\n")
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.Secrets[0].Source; !reflect.DeepEqual(got, wizard.Source{Env: "ANTHROPIC_API_KEY"}) {
		t.Fatalf("got source %+v, want the environment", got)
	}
}

func TestApprovalsAreRememberedButTokenValuesAreNeverSaved(t *testing.T) {
	dir := newProject(t, "app")
	h := withEnv(fakeHost(dir), map[string]string{"ANTHROPIC_API_KEY": "sk-ant-real"})
	h = withCommands(h, map[string]string{"gh auth token": "gho_real"})
	configDir := t.TempDir()
	first, err := runWith(t, h, configDir, "[[secrets]]\nname = \"GH_TOKEN\"\n[[secrets]]\nname = \"ANTHROPIC_API_KEY\"\n")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{filepath.Join(dir, "orcasm.toml"), filepath.Join(configDir, "config.toml")} {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), "sk-ant-real") || strings.Contains(string(data), "gho_real") {
			t.Fatalf("%s holds a token value:\n%s", f, data)
		}
	}
	second, err := runWith(t, h, configDir, "")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(second.Secrets, first.Secrets) {
		t.Fatalf("got  %+v\nwant %+v", second.Secrets, first.Secrets)
	}
}

func TestRememberedApprovalForATokenNoLongerOnTheHostIsDropped(t *testing.T) {
	dir := newProject(t, "app")
	configDir := t.TempDir()
	h := withEnv(fakeHost(dir), map[string]string{"ANTHROPIC_API_KEY": "sk-ant-real"})
	if _, err := runWith(t, h, configDir, "[[secrets]]\nname = \"ANTHROPIC_API_KEY\"\n"); err != nil {
		t.Fatal(err)
	}
	cfg, err := runWith(t, fakeHost(dir), configDir, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Secrets) != 0 {
		t.Fatalf("got secrets %+v, want none", cfg.Secrets)
	}
}

func TestGitIdentityIsAnAnswerRememberedForNextTime(t *testing.T) {
	dir := newProject(t, "app")
	configDir := t.TempDir()
	first, err := runWith(t, fakeHost(dir), configDir, "[git]\nname = \"Ada\"\nemail = \"ada@example.com\"\n")
	if err != nil {
		t.Fatal(err)
	}
	if want := (wizard.GitIdentity{Name: "Ada", Email: "ada@example.com"}); first.Git != want {
		t.Fatalf("got %+v, want %+v", first.Git, want)
	}
	second, err := runWith(t, fakeHost(dir), configDir, "")
	if err != nil {
		t.Fatal(err)
	}
	if second.Git != first.Git {
		t.Fatalf("got %+v, want %+v", second.Git, first.Git)
	}
}

func TestInteractiveRunOffersTheHostsTokensAndGitIdentity(t *testing.T) {
	dir := newProject(t, "my-app")
	h := withEnv(fakeHost(dir), map[string]string{"ANTHROPIC_API_KEY": "sk-ant-real"})
	h = withCommands(h, map[string]string{
		"gh auth token":                        "gho_real",
		"git -C " + dir + " config user.name":  "Ada\n",
		"git -C " + dir + " config user.email": "ada@example.com\n",
	})
	var out strings.Builder
	// Keep the folder, name, resources and port; confirm the harnesses (0);
	// then in the sharing question toggle option 2 (ANTHROPIC_API_KEY) and 3
	// (git identity), leaving 1 (GH_TOKEN) out, and confirm.
	in := strings.NewReader(strings.Repeat("\n", 7) + "0\n" + "2\n3\n0\n")
	cfg, err := wizard.Run(wizard.Options{Host: h, ConfigDir: t.TempDir(), In: in, Out: &out})
	if err != nil {
		t.Fatalf("%v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "GH_TOKEN") {
		t.Fatalf("want the gh token offered:\n%s", out.String())
	}
	want := []wizard.Secret{{Name: "ANTHROPIC_API_KEY", Hosts: []string{"api.anthropic.com"}, Source: wizard.Source{Env: "ANTHROPIC_API_KEY"}}}
	if !reflect.DeepEqual(cfg.Secrets, want) {
		t.Fatalf("got  %+v\nwant %+v\n%s", cfg.Secrets, want, out.String())
	}
	if want := (wizard.GitIdentity{Name: "Ada", Email: "ada@example.com"}); cfg.Git != want {
		t.Fatalf("got %+v, want %+v", cfg.Git, want)
	}
}
