package worktree_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Betree/Orcasm/internal/stack"
)

// After `orcasm upgrade` the Sandbox runs a newer orcasm, but each
// Worktree's saved Stack is still the one an older orcasm generated:
// Workbenches are recreated lazily, at the next agent launch (ADR 0006).

// olderLayer sets up Worktree feat-login with a Stack saved by an orcasm
// with another Workbench layer, its Workbench running as c0ffee with
// processes top (`docker top` output); it records whether the Stack is
// brought up again.
func olderLayer(t *testing.T, top string) (f *fixture, ups *int) {
	t.Helper()
	f = newFixture(t)
	ctx := context.Background()
	if err := f.hooks.Setup(ctx, "/worktrees/shop/feat-login", io.Discard); err != nil {
		t.Fatal(err)
	}
	saved := filepath.Join(f.hooks.StateDir("feat-login"), "stack.json")
	raw, err := os.ReadFile(saved)
	must(t, err)
	var st map[string]any
	must(t, json.Unmarshal(raw, &st))
	delete(st, "Layer") // as saved before Workbench layers were recorded
	raw, err = json.Marshal(st)
	must(t, err)
	must(t, os.WriteFile(saved, raw, 0o644))

	return f, runningAs(f, top)
}

func savedLayer(t *testing.T, f *fixture) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(f.hooks.StateDir("feat-login"), "stack.json"))
	must(t, err)
	var st stack.Stack
	must(t, json.Unmarshal(raw, &st))
	return st.Layer
}

// runningAs makes the Workbench run as c0ffee with processes top, and
// counts the Stack's `up`s.
func runningAs(f *fixture, top string) (ups *int) {
	ups = new(int)
	f.docker.reply = func(args []string) (string, error) {
		switch {
		case slices.Contains(args, "up"):
			*ups++
		case slices.Contains(args, "ps"):
			return "c0ffee\n", nil
		case len(args) > 1 && args[0] == "top":
			return top, nil
		}
		return "", nil
	}
	return ups
}

func TestAnIdleWorkbenchFromAnOlderOrcasmIsRecreatedAtTheNextAgentLaunch(t *testing.T) {
	f, ups := olderLayer(t, "PID COMMAND\n41 sleep infinity\n")
	var progress bytes.Buffer

	id, err := f.hooks.Workbench(context.Background(), "/worktrees/shop/feat-login", &progress)
	if err != nil || id != "c0ffee" {
		t.Fatalf("id %q, err %v", id, err)
	}
	if *ups != 1 {
		t.Errorf("the Stack was brought up %d times, want once", *ups)
	}
	if got := savedLayer(t, f); got == "" {
		t.Error("the regenerated Stack has no layer recorded")
	}
	if !strings.Contains(progress.String(), "Updating") {
		t.Errorf("progress %q does not say the Workbench is updated", progress.String())
	}
}

func TestAWorkbenchWithAnAgentRunningKeepsItsOlderLayer(t *testing.T) {
	// Recreating it would kill that agent mid-task.
	f, ups := olderLayer(t, "PID COMMAND\n41 sleep infinity\n57 "+stack.HarnessLayer+"/lib/node_modules/@anthropic-ai/claude-code/bin/claude\n")

	id, err := f.hooks.Workbench(context.Background(), "/worktrees/shop/feat-login", io.Discard)
	if err != nil || id != "c0ffee" {
		t.Fatalf("id %q, err %v", id, err)
	}
	if *ups != 0 {
		t.Errorf("the Stack was brought up %d times under a running agent", *ups)
	}
	if got := savedLayer(t, f); got != "" {
		t.Errorf("recorded layer %q without recreating the Workbench", got)
	}
}

func TestAWorkbenchIsRecreatedWhenWhatOrcasmPutsInItChanged(t *testing.T) {
	// `orcasm upgrade` rewrote the Sandbox's settings (here a new secret):
	// the override would differ, with no revision bumped by hand.
	f := newFixture(t)
	ctx := context.Background()
	path := "/worktrees/shop/feat-login"
	must(t, f.hooks.Setup(ctx, path, io.Discard))
	before := savedLayer(t, f)
	ups := runningAs(f, "PID COMMAND\n41 sleep infinity\n")
	f.hooks.Config.Secrets = append(f.hooks.Config.Secrets, "NPM_TOKEN")

	if _, err := f.hooks.Workbench(ctx, path, io.Discard); err != nil {
		t.Fatal(err)
	}
	if *ups != 1 {
		t.Errorf("the Stack was brought up %d times, want once", *ups)
	}
	if after := savedLayer(t, f); after == before {
		t.Error("the saved layer did not change")
	}
}

func TestAWorkbenchOfThisOrcasmIsNotRecreated(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	path := "/worktrees/shop/feat-login"
	must(t, f.hooks.Setup(ctx, path, io.Discard))
	f.running(func() string { return "c0ffee" })

	if _, err := f.hooks.Workbench(ctx, path, io.Discard); err != nil {
		t.Fatal(err)
	}
	if up := f.docker.called("up", "--detach", "--build", "--remove-orphans"); up != nil {
		t.Errorf("brought the Stack up again: %v", up)
	}
}
