package stack_test

import (
	"context"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/Betree/Orcasm/internal/stack"
)

// fakeDocker answers `docker compose ... config --services` with services
// and records every call.
type fakeDocker struct {
	services []string
	calls    [][]string
}

func (f *fakeDocker) run(_ context.Context, args ...string) ([]byte, error) {
	f.calls = append(f.calls, args)
	return []byte(strings.Join(f.services, "\n") + "\n"), nil
}

// spec is a single-Repository Project whose Worktree "feat-login" is
// checked out at /worktrees/shop/feat-login.
func spec(t *testing.T) stack.Spec {
	t.Helper()
	return stack.Spec{
		Worktree: "feat-login",
		Checkouts: []stack.Checkout{{
			Repository: "shop",
			Path:       "/worktrees/shop/feat-login",
			GitDir:     "/home/dev/shop/.git",
		}},
		ComposeFiles: []string{"compose.yml"},
		Workbench:    "app",
		Home:         filepath.Join(t.TempDir(), "home"),
	}
}

type override struct {
	Services map[string]map[string]yaml.Node `yaml:"services"`
}

func generate(t *testing.T, s stack.Spec, services ...string) (stack.Stack, override, *fakeDocker) {
	t.Helper()
	d := &fakeDocker{services: services}
	st, err := stack.Generate(context.Background(), d.run, s, t.TempDir())
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	raw, err := os.ReadFile(st.Override)
	if err != nil {
		t.Fatal(err)
	}
	var o override
	if err := yaml.Unmarshal(raw, &o); err != nil {
		t.Fatalf("override is not YAML: %v\n%s", err, raw)
	}
	return st, o, d
}

func TestEveryServiceLosesItsPublishedPorts(t *testing.T) {
	_, o, _ := generate(t, spec(t), "app", "db", "web")
	for _, name := range []string{"app", "db", "web"} {
		ports, ok := o.Services[name]["ports"]
		if !ok || ports.Tag != "!reset" {
			t.Errorf("service %s: ports = %+v, want !reset []", name, ports)
		}
	}
}

func TestComposeFilesAreReadFromTheCheckoutOfTheirRepository(t *testing.T) {
	s := spec(t)
	s.Checkouts = []stack.Checkout{
		{Repository: "api", ProjectDir: "api", Path: "/worktrees/api/feat-login"},
		{Repository: "web", ProjectDir: "frontend/web", Path: "/worktrees/web/feat-login"},
	}
	s.ComposeFiles = []string{"api/compose.yml", "frontend/web/docker/compose.dev.yml"}
	st, _, d := generate(t, s, "app")
	want := []string{
		"/worktrees/api/feat-login/compose.yml",
		"/worktrees/web/feat-login/docker/compose.dev.yml",
		st.Override,
	}
	if !slices.Equal(st.Files, want) {
		t.Errorf("Files = %q, want %q", st.Files, want)
	}
	if st.Project != "orcasm-feat-login" {
		t.Errorf("Project = %q", st.Project)
	}
	query := strings.Join(d.calls[0], " ")
	if !strings.Contains(query, "-p orcasm-feat-login -f /worktrees/api/feat-login/compose.yml -f /worktrees/web/feat-login/docker/compose.dev.yml") {
		t.Errorf("services queried with %q", query)
	}
}

func TestProjectNameIsAValidComposeNameForAnyWorktreeName(t *testing.T) {
	for name, want := range map[string]string{
		"feat-login": "orcasm-feat-login",
		"a_b":        "orcasm-a_b",
	} {
		if got := stack.ProjectName(name); got != want {
			t.Errorf("ProjectName(%q) = %q, want %q", name, got, want)
		}
	}
}

func list(t *testing.T, o override, service, key string) []string {
	t.Helper()
	var out []string
	n, ok := o.Services[service][key]
	if !ok {
		return nil
	}
	if err := n.Decode(&out); err != nil {
		t.Fatalf("%s.%s: %v", service, key, err)
	}
	return out
}

func env(t *testing.T, o override, service string) map[string]string {
	t.Helper()
	out := map[string]string{}
	if n, ok := o.Services[service]["environment"]; ok {
		if err := n.Decode(&out); err != nil {
			t.Fatalf("%s.environment: %v", service, err)
		}
	}
	return out
}

func TestWorkbenchSeesEachCheckoutAtItsSandboxPathAndUnderWorkspace(t *testing.T) {
	s := spec(t)
	s.Checkouts = []stack.Checkout{
		{Repository: "api", ProjectDir: "api", Path: "/worktrees/api/x", GitDir: "/home/dev/p/api/.git"},
		{Repository: "web", ProjectDir: "web", Path: "/worktrees/web/x", GitDir: "/home/dev/p/web/.git"},
	}
	s.ComposeFiles = []string{"api/compose.yml"}
	st, o, _ := generate(t, s, "app", "db")
	vols := list(t, o, "app", "volumes")
	for _, want := range []string{
		"/worktrees/api/x:/worktrees/api/x",
		"/worktrees/api/x:/workspace/api",
		"/home/dev/p/api/.git:/home/dev/p/api/.git",
		"/worktrees/web/x:/worktrees/web/x",
		"/worktrees/web/x:/workspace/web",
		"/home/dev/p/web/.git:/home/dev/p/web/.git",
	} {
		if !slices.Contains(vols, want) {
			t.Errorf("Workbench volumes %q lack %q", vols, want)
		}
	}
	if list(t, o, "db", "volumes") != nil {
		t.Error("a service other than the Workbench got volumes")
	}
	if st.Workbench != "app" {
		t.Errorf("Workbench = %q", st.Workbench)
	}
}

func TestWorkbenchTrustsTheSandboxCAs(t *testing.T) {
	_, o, _ := generate(t, spec(t), "app")
	if !slices.Contains(list(t, o, "app", "volumes"), "/etc/ssl/certs/ca-certificates.crt:"+stack.CABundle+":ro") {
		t.Error("the Sandbox CA bundle is not mounted")
	}
	e := env(t, o, "app")
	for _, v := range []string{"SSL_CERT_FILE", "NODE_EXTRA_CA_CERTS", "CURL_CA_BUNDLE", "REQUESTS_CA_BUNDLE", "GIT_SSL_CAINFO"} {
		if e[v] != stack.CABundle {
			t.Errorf("%s = %q, want %q", v, e[v], stack.CABundle)
		}
	}
}

func TestWorkbenchGetsSecretPlaceholdersNotValues(t *testing.T) {
	s := spec(t)
	s.Secrets = []string{"GH_TOKEN", "ANTHROPIC_API_KEY"}
	_, o, _ := generate(t, s, "app")
	e := env(t, o, "app")
	// Compose turns $$ into $: the container sees msb's placeholder.
	if e["GH_TOKEN"] != "$$MSB_GH_TOKEN" || e["ANTHROPIC_API_KEY"] != "$$MSB_ANTHROPIC_API_KEY" {
		t.Errorf("environment = %v", e)
	}
}

func TestWorkbenchSharesTheHarnessLayerAndConfigDirsReadWrite(t *testing.T) {
	s := spec(t)
	s.Harnesses = []string{"claude", "codex", "opencode"}
	_, o, _ := generate(t, s, "app")
	vols := list(t, o, "app", "volumes")
	if !slices.Contains(vols, stack.HarnessLayer+":"+stack.HarnessLayer+":ro") {
		t.Errorf("volumes %q lack the harness layer", vols)
	}
	for _, dir := range []string{".claude", ".codex", ".config/opencode", ".local/share/opencode"} {
		p := filepath.Join(s.Home, dir)
		if !slices.Contains(vols, p+":"+p) {
			t.Errorf("volumes %q lack %s read-write", vols, p)
		}
		// Created up front, by the Sandbox user: Docker would create it
		// as root.
		if fi, err := os.Stat(p); err != nil || !fi.IsDir() {
			t.Errorf("%s not created: %v", p, err)
		}
	}
}

func TestOnlyTheChosenHarnessesConfigIsShared(t *testing.T) {
	s := spec(t)
	s.Harnesses = []string{"codex"}
	_, o, _ := generate(t, s, "app")
	for _, v := range list(t, o, "app", "volumes") {
		if strings.Contains(v, ".claude") || strings.Contains(v, "opencode") {
			t.Errorf("unexpected volume %q", v)
		}
	}
}

func TestWithoutAWorkbenchServiceTheGenericOneIsAdded(t *testing.T) {
	s := spec(t)
	s.Workbench = ""
	st, o, _ := generate(t, s, "db")
	if st.Workbench != stack.GenericWorkbench {
		t.Fatalf("Workbench = %q", st.Workbench)
	}
	var wb struct {
		Image      string
		Command    []string
		Init       bool
		WorkingDir string `yaml:"working_dir"`
	}
	n := mappingOf(o.Services[stack.GenericWorkbench])
	if err := n.Decode(&wb); err != nil {
		t.Fatal(err)
	}
	if wb.Image != stack.GenericImage || !slices.Equal(wb.Command, []string{"sleep", "infinity"}) || !wb.Init {
		t.Errorf("generic Workbench = %+v", wb)
	}
	if wb.WorkingDir != "/worktrees/shop/feat-login" {
		t.Errorf("working_dir = %q", wb.WorkingDir)
	}
	if !slices.Contains(list(t, o, stack.GenericWorkbench, "volumes"), "/worktrees/shop/feat-login:/workspace/shop") {
		t.Error("the generic Workbench does not see the Worktree")
	}
}

func TestAFreshStackHasTheWorkbenchLayerThisOrcasmGenerates(t *testing.T) {
	for _, workbench := range []string{"app", ""} {
		s := spec(t)
		s.Workbench = workbench
		st, _, _ := generate(t, s, "app", "db")
		if st.Outdated(s) {
			t.Errorf("Workbench %q: a Stack just generated is outdated", workbench)
		}
	}
}

func TestAStackIsOutdatedOnceThisOrcasmWouldGenerateAnotherOverride(t *testing.T) {
	// E.g. a newer orcasm mounts or sets something else in Workbenches, or
	// the Sandbox's harnesses or secrets changed: no revision to bump.
	s := spec(t)
	st, _, _ := generate(t, s, "app", "db")

	changed := s
	changed.Secrets = []string{"GH_TOKEN"}
	if !st.Outdated(changed) {
		t.Error("a Stack generated without the secret is not outdated")
	}
	changed = s
	changed.Harnesses = []string{"claude"}
	if !st.Outdated(changed) {
		t.Error("a Stack generated without the harness is not outdated")
	}
}

func TestAStackSavedWithoutALayerIsOutdated(t *testing.T) {
	// Saved by an orcasm before Workbench layers were recorded.
	s := spec(t)
	st, _, _ := generate(t, s, "app", "db")
	st.Layer = ""
	if !st.Outdated(s) {
		t.Error("not outdated")
	}
}

func TestAWorkbenchServiceMissingFromTheComposeFilesIsRefused(t *testing.T) {
	_, err := stack.Generate(context.Background(), (&fakeDocker{services: []string{"db"}}).run, spec(t), t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "app") {
		t.Fatalf("err = %v, want one naming the missing Workbench service", err)
	}
}

func TestWebServiceIsKeptForTheProxy(t *testing.T) {
	s := spec(t)
	s.WebService, s.WebPort = "web", 3000
	st, _, _ := generate(t, s, "app", "web")
	if st.WebService != "web" || st.WebPort != 3000 {
		t.Errorf("web = %s:%d", st.WebService, st.WebPort)
	}
}

func TestUpAndDownDriveComposeOnTheWholeStack(t *testing.T) {
	st, _, d := generate(t, spec(t), "app")
	ctx := context.Background()
	if err := st.Up(ctx, d.run); err != nil {
		t.Fatal(err)
	}
	if err := st.Down(ctx, d.run); err != nil {
		t.Fatal(err)
	}
	prefix := "compose -p orcasm-feat-login -f /worktrees/shop/feat-login/compose.yml -f " + st.Override + " "
	got := []string{strings.Join(d.calls[1], " "), strings.Join(d.calls[2], " ")}
	want := []string{prefix + "up --detach --build --remove-orphans", prefix + "down --volumes --remove-orphans"}
	if !slices.Equal(got, want) {
		t.Errorf("calls = %q, want %q", got, want)
	}
}

func labels(t *testing.T, o override, service string) map[string]string {
	t.Helper()
	out := map[string]string{}
	if n, ok := o.Services[service]["labels"]; ok {
		if err := n.Decode(&out); err != nil {
			t.Fatalf("%s.labels: %v", service, err)
		}
	}
	return out
}

func TestTheProxyRoutesTheWorktreeHostnameToTheWebServicePort(t *testing.T) {
	s := spec(t)
	s.Worktree = "fix-login-2"
	s.WebService, s.WebPort = "web", 3000
	_, o, _ := generate(t, s, "app", "web", "db")
	got := labels(t, o, "web")
	want := map[string]string{
		"orcasm.proxy":   "true",
		"traefik.enable": "true",
		"traefik.http.routers.orcasm-fix-login-2.rule":                      "Host(`fix-login-2.localhost`)",
		"traefik.http.services.orcasm-fix-login-2.loadbalancer.server.port": "3000",
	}
	if !maps.Equal(got, want) {
		t.Errorf("web labels = %v, want %v", got, want)
	}
	for _, other := range []string{"app", "db"} {
		if l := labels(t, o, other); len(l) != 0 {
			t.Errorf("%s got proxy labels %v", other, l)
		}
	}
}

func TestHostnameIsTheWorktreeUnderLocalhost(t *testing.T) {
	for name, want := range map[string]string{
		"feat-login": "feat-login.localhost",
		"a-b":        "a-b.localhost",
	} {
		if got := stack.Hostname(name); got != want {
			t.Errorf("Hostname(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestWithoutAWebServiceNothingIsRouted(t *testing.T) {
	_, o, _ := generate(t, spec(t), "app", "db")
	for name := range o.Services {
		if l := labels(t, o, name); len(l) != 0 {
			t.Errorf("%s got proxy labels %v", name, l)
		}
	}
}

func TestAWebServiceMissingFromTheComposeFilesIsRefused(t *testing.T) {
	s := spec(t)
	s.WebService, s.WebPort = "web", 3000
	_, err := stack.Generate(context.Background(), (&fakeDocker{services: []string{"app"}}).run, s, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "web") {
		t.Fatalf("err = %v, want one naming the missing web service", err)
	}
}

// The proxy routes to the web service's port: without one, Traefik would
// get port 0 and the Worktree's URL would never answer.
func TestAWebServiceWithoutAValidPortIsRefused(t *testing.T) {
	for _, port := range []int{0, -1, 65536} {
		s := spec(t)
		s.WebService, s.WebPort = "web", port
		_, err := stack.Generate(context.Background(), (&fakeDocker{services: []string{"app", "web"}}).run, s, t.TempDir())
		if err == nil || !strings.Contains(err.Error(), "port") {
			t.Errorf("port %d: err = %v, want one about the web port", port, err)
		}
	}
}

func mappingOf(m map[string]yaml.Node) *yaml.Node {
	n := &yaml.Node{Kind: yaml.MappingNode}
	for k, v := range m {
		n.Content = append(n.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: k}, &v)
	}
	return n
}

func TestComposeFileOutsideEveryRepositoryIsRefused(t *testing.T) {
	s := spec(t)
	s.Checkouts[0].ProjectDir = "api"
	s.ComposeFiles = []string{"ops/compose.yml"}
	_, err := stack.Generate(context.Background(), (&fakeDocker{}).run, s, t.TempDir())
	if err == nil {
		t.Fatal("Generate accepted a compose file outside every Repository")
	}
}

// home is the Sandbox user's home in s, joined with rel.
func home(s stack.Spec, rel string) string { return filepath.Join(s.Home, rel) }

func TestWorkbenchAgentsReachOrcasHookServer(t *testing.T) {
	s := spec(t)
	s.Harnesses = []string{"claude"}
	_, o, _ := generate(t, s, "app")
	if hosts := list(t, o, "app", "extra_hosts"); !slices.Contains(hosts, "host.docker.internal:host-gateway") {
		t.Errorf("extra_hosts = %q", hosts)
	}
	vols := list(t, o, "app", "volumes")
	for _, want := range []string{
		// Orca's endpoint file and spool, written by both sides.
		home(s, ".config/orca/agent-hooks") + ":" + home(s, ".config/orca/agent-hooks"),
		// The wrapper's rewritten hook scripts, where Orca's settings call them.
		home(s, ".cache/orcasm/agent-hooks") + ":" + home(s, ".orca/agent-hooks") + ":ro",
	} {
		if !slices.Contains(vols, want) {
			t.Errorf("volumes %q lack %q", vols, want)
		}
	}
	if fi, err := os.Stat(home(s, ".cache/orcasm/agent-hooks")); err != nil || !fi.IsDir() {
		t.Errorf("hook copy dir not created: %v", err)
	}
}

func TestWorkbenchGetsOnlyWhatAgentsNeedNotTheWholeHome(t *testing.T) {
	s := spec(t)
	s.Harnesses = []string{"claude", "codex", "opencode"}
	_, o, _ := generate(t, s, "app")
	for _, v := range list(t, o, "app", "volumes") {
		src := strings.SplitN(v, ":", 2)[0]
		if src == s.Home || src == home(s, ".config/orca") || strings.HasSuffix(src, ".claude.json") {
			t.Errorf("volume %q shares more than the agents need", v)
		}
	}
}

func TestClaudeKeepsItsGlobalConfigInItsConfigDir(t *testing.T) {
	// ~/.claude.json is replaced by rename (Orca's trust preflight, Claude
	// itself), so a single-file mount would go stale: Claude reads it from
	// CLAUDE_CONFIG_DIR, a mounted directory, instead.
	s := spec(t)
	s.Harnesses = []string{"claude"}
	_, o, _ := generate(t, s, "app")
	if got := env(t, o, "app")["CLAUDE_CONFIG_DIR"]; got != home(s, ".claude") {
		t.Errorf("CLAUDE_CONFIG_DIR = %q", got)
	}
}

func TestWorkbenchCommitsWithTheSandboxGitIdentity(t *testing.T) {
	s := spec(t)
	_, o, _ := generate(t, s, "app")
	p := home(s, ".config/git")
	if vols := list(t, o, "app", "volumes"); !slices.Contains(vols, p+":"+p) {
		t.Errorf("volumes %q lack %s", vols, p)
	}
}

func TestCodexAndOpenCodeGetOrcasPerLaunchConfig(t *testing.T) {
	s := spec(t)
	s.Harnesses = []string{"codex", "opencode"}
	_, o, _ := generate(t, s, "app")
	vols := list(t, o, "app", "volumes")
	codexHome := home(s, ".config/orca/codex-runtime-home")
	openCode := home(s, ".cache/orcasm/opencode-config")
	for _, want := range []string{codexHome + ":" + codexHome, openCode + ":" + openCode + ":ro"} {
		if !slices.Contains(vols, want) {
			t.Errorf("volumes %q lack %q", vols, want)
		}
	}
}

func TestTheGenericWorkbenchHasWhatOrcasHooksAndAgentsRun(t *testing.T) {
	s := spec(t)
	s.Workbench = ""
	_, o, _ := generate(t, s, "db")
	var wb struct {
		Image string
		Build struct {
			Inline string `yaml:"dockerfile_inline"`
		}
	}
	if err := mappingOf(o.Services[stack.GenericWorkbench]).Decode(&wb); err != nil {
		t.Fatal(err)
	}
	if wb.Image != stack.GenericImage || !strings.Contains(wb.Build.Inline, "FROM ubuntu:24.04") {
		t.Errorf("generic Workbench = %+v", wb)
	}
	for _, tool := range []string{"curl", "git", "ca-certificates"} {
		if !strings.Contains(wb.Build.Inline, tool) {
			t.Errorf("generic image lacks %s:\n%s", tool, wb.Build.Inline)
		}
	}
}

// Names Compose or DNS can't take as they are get a hash of the original,
// so two Worktrees never share a Stack or a hostname.
func TestDistinctWorktreesNeverShareAStackOrAHostname(t *testing.T) {
	names := []string{"fix-login", "Fix-Login", "fix_login", "fix/login", "Fix/Login.v2 now"}
	projects, hosts := map[string]string{}, map[string]string{}
	for _, n := range names {
		p, h := stack.ProjectName(n), stack.Hostname(n)
		if prev, ok := projects[p]; ok {
			t.Errorf("%q and %q share Compose project %s", prev, n, p)
		}
		if prev, ok := hosts[h]; ok {
			t.Errorf("%q and %q share hostname %s", prev, n, h)
		}
		projects[p], hosts[h] = n, n
		if !regexp.MustCompile(`^orcasm-[a-z0-9][a-z0-9_-]*$`).MatchString(p) {
			t.Errorf("ProjectName(%q) = %q is no Compose project name", n, p)
		}
		if !regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?\.localhost$`).MatchString(h) {
			t.Errorf("Hostname(%q) = %q is no hostname", n, h)
		}
	}
	if got := stack.Hostname("Fix/Login.v2 now"); !strings.HasPrefix(got, "fix-login-v2-now-") {
		t.Errorf("Hostname keeps no readable prefix: %s", got)
	}
}
