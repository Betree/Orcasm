package sandbox_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/Betree/Orcasm/internal/sandbox"
	"github.com/Betree/Orcasm/internal/wizard"
)

// `msb start` needs every secret's value again, so the Sandbox's Config
// (secret specs, never values) is kept on the host.
func TestSavedConfigLoadsBackWithItsSecretSpecs(t *testing.T) {
	dir := t.TempDir()
	cfg := demoConfig()
	cfg.Secrets = []wizard.Secret{{
		Name: "GH_TOKEN", Hosts: []string{"github.com"},
		Source: wizard.Source{Command: []string{"gh", "auth", "token"}},
	}}

	if err := sandbox.SaveConfig(dir, cfg); err != nil {
		t.Fatal(err)
	}
	got, err := sandbox.LoadConfig(dir, "demo")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, cfg) {
		t.Errorf("loaded %+v\nwant %+v", got, cfg)
	}
}

func TestLoadConfigOfAnUnknownSandboxFails(t *testing.T) {
	_, err := sandbox.LoadConfig(t.TempDir(), "nope")
	if err == nil || !strings.Contains(err.Error(), "nope") {
		t.Fatalf("want an error naming the Sandbox, got %v", err)
	}
}

func TestRemoveConfigForgetsTheSandbox(t *testing.T) {
	dir := t.TempDir()
	if err := sandbox.SaveConfig(dir, demoConfig()); err != nil {
		t.Fatal(err)
	}
	if err := sandbox.RemoveConfig(dir, "demo"); err != nil {
		t.Fatal(err)
	}
	if _, err := sandbox.LoadConfig(dir, "demo"); err == nil {
		t.Error("still loadable after RemoveConfig")
	}
	if err := sandbox.RemoveConfig(dir, "demo"); err != nil {
		t.Errorf("removing twice: %v", err)
	}
}
