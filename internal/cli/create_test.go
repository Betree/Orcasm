package cli_test

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Betree/Orcasm/internal/cli"
	"github.com/Betree/Orcasm/internal/sandbox"
	"github.com/Betree/Orcasm/internal/wizard"
)

// TestMain makes sure no test boots a real Sandbox: tests running
// `orcasm create` must call fakeCreate.
func TestMain(m *testing.M) {
	cli.SetCreate(func(context.Context, wizard.Config, sandbox.SecretValue, io.Writer) error {
		return errors.New("test would create a real Sandbox: call fakeCreate")
	})
	os.Exit(m.Run())
}

// fakeCreate records the Config `orcasm create` provisions.
func fakeCreate(t *testing.T) *[]wizard.Config {
	var got []wizard.Config
	t.Cleanup(cli.SetCreate(func(_ context.Context, cfg wizard.Config, _ sandbox.SecretValue, _ io.Writer) error {
		got = append(got, cfg)
		return nil
	}))
	return &got
}

func TestCreateBuildsTheSandboxFromTheWizardConfig(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	created := fakeCreate(t)
	project := filepath.Join(t.TempDir(), "demo")
	if err := os.Mkdir(project, 0o755); err != nil {
		t.Fatal(err)
	}
	answers := filepath.Join(t.TempDir(), "answers.toml")
	content := "project_path = \"" + project + "\"\nname = \"orcasm-cli-test-sandbox\"\nhost_port = 16999\nharnesses = [\"codex\"]\n"
	if err := os.WriteFile(answers, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := run(t, "create", "--answers", answers)
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if len(*created) != 1 {
		t.Fatalf("want one Sandbox created, got %d", len(*created))
	}
	cfg := (*created)[0]
	if cfg.Name != "orcasm-cli-test-sandbox" || cfg.ProjectPath != project || cfg.HostPort != 16999 || !reflect.DeepEqual(cfg.Harnesses, []string{"codex"}) {
		t.Errorf("created %+v", cfg)
	}
	if !strings.Contains(out, "http://127.0.0.1:16999/") {
		t.Errorf("want the web UI address printed, got:\n%s", out)
	}
}

func TestCreateWithAnswersFileRunsTheWizardWithoutPrompting(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	fakeCreate(t)
	project := filepath.Join(t.TempDir(), "demo")
	if err := os.Mkdir(project, 0o755); err != nil {
		t.Fatal(err)
	}
	answers := filepath.Join(t.TempDir(), "answers.toml")
	content := "project_path = \"" + project + "\"\nname = \"orcasm-cli-test-sandbox\"\nharnesses = []\n"
	if err := os.WriteFile(answers, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := run(t, "create", "--answers", answers)
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if !strings.Contains(out, "Creating Sandbox orcasm-cli-test-sandbox for "+project) {
		t.Fatalf("want a summary of the collected config, got:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(project, "orcasm.toml")); err != nil {
		t.Fatalf("want project answers saved: %v", err)
	}
}

func TestCreateSummarizesApprovedSecretsWithoutTheirValues(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-fake-value")
	fakeCreate(t)
	project := filepath.Join(t.TempDir(), "demo")
	if err := os.Mkdir(project, 0o755); err != nil {
		t.Fatal(err)
	}
	answers := filepath.Join(t.TempDir(), "answers.toml")
	content := "project_path = \"" + project + "\"\nname = \"orcasm-cli-test-sandbox\"\nharnesses = []\n" +
		"[[secrets]]\nname = \"ANTHROPIC_API_KEY\"\n"
	if err := os.WriteFile(answers, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := run(t, "create", "--answers", answers)
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if !strings.Contains(out, "ANTHROPIC_API_KEY (sent only to api.anthropic.com)") {
		t.Fatalf("want the shared secret summarized, got:\n%s", out)
	}
	if strings.Contains(out, "[resources]") {
		t.Fatalf("want a short summary, not the whole Config:\n%s", out)
	}
	if strings.Contains(out, "sk-ant-fake-value") {
		t.Fatalf("the token value was printed:\n%s", out)
	}
}

func TestAnswersFlagIsHidden(t *testing.T) {
	out, err := run(t, "create", "--help")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "--answers") {
		t.Fatalf("--answers should be hidden from help:\n%s", out)
	}
}
