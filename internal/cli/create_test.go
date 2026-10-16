package cli_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCreateWithAnswersFileRunsTheWizardWithoutPrompting(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	project := filepath.Join(t.TempDir(), "demo")
	if err := os.Mkdir(project, 0o755); err != nil {
		t.Fatal(err)
	}
	answers := filepath.Join(t.TempDir(), "answers.toml")
	content := "folder = \"" + project + "\"\nname = \"orcasm-cli-test-sandbox\"\nharnesses = []\n"
	if err := os.WriteFile(answers, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := run(t, "create", "--answers", answers)
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if !strings.Contains(out, `name = "orcasm-cli-test-sandbox"`) {
		t.Fatalf("want the collected config printed, got:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(project, "orcasm.toml")); err != nil {
		t.Fatalf("want project answers saved: %v", err)
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
