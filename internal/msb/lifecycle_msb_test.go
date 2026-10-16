//go:build msb

package msb_test

import (
	"context"
	"os"
	"testing"

	"github.com/Betree/Orcasm/internal/msb"
)

// Runs against a real, throwaway sandbox carrying the label orcasm-test=1:
//
//	msb create ghcr.io/superradcompany/ubuntu-systemd:24.04 --init auto --name spike15-a --label orcasm-test=1
//	ORCASM_TEST_SANDBOX=spike15-a go test -tags msb ./internal/msb/
//
// It stops and starts that sandbox. Never point it at a sandbox you care about.
func testSandbox(t *testing.T) string {
	name := os.Getenv("ORCASM_TEST_SANDBOX")
	if name == "" {
		t.Skip("ORCASM_TEST_SANDBOX not set")
	}
	return name
}

func status(t *testing.T, c msb.CLI, name string) msb.Status {
	t.Helper()
	list, err := c.List(context.Background(), msb.Label{Key: "orcasm-test", Value: "1"})
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range list {
		if l.Name == name {
			return l.Status
		}
	}
	t.Fatalf("%s not listed with label orcasm-test=1: %+v", name, list)
	return ""
}

func TestLifecycleWithRealMSB(t *testing.T) {
	name := testSandbox(t)
	ctx := context.Background()
	c := msb.CLI{}

	labels, err := c.Labels(ctx, name)
	if err != nil {
		t.Fatal(err)
	}
	if labels["orcasm-test"] != "1" {
		t.Errorf("labels = %v, want orcasm-test=1", labels)
	}

	if err := c.Stop(ctx, name, false); err != nil {
		t.Fatal(err)
	}
	if got := status(t, c, name); got != msb.Stopped {
		t.Errorf("after stop: status = %q", got)
	}
	if err := c.Start(ctx, name, nil); err != nil {
		t.Fatal(err)
	}
	if got := status(t, c, name); got != msb.Running {
		t.Errorf("after start: status = %q", got)
	}
	if out, err := c.Exec(ctx, name, "", "echo", "hi"); err != nil || out != "hi\n" {
		t.Errorf("exec = %q, %v", out, err)
	}
	if err := c.Stop(ctx, name, true); err != nil {
		t.Fatal(err)
	}
	if got := status(t, c, name); got != msb.Stopped {
		t.Errorf("after stop --force: status = %q", got)
	}
}

func TestListWithUnmatchedLabelIsEmpty(t *testing.T) {
	list, err := msb.CLI{}.List(context.Background(), msb.Label{Key: "orcasm-test", Value: "no-such-value"})
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 0 {
		t.Errorf("list = %+v, want empty", list)
	}
}

func TestSetLabelsChangesOneLabelOnARunningSandboxAndKeepsTheRest(t *testing.T) {
	name := testSandbox(t)
	ctx := context.Background()
	c := msb.CLI{}
	if status(t, c, name) != msb.Running {
		if err := c.Start(ctx, name, nil); err != nil {
			t.Fatal(err)
		}
	}
	for _, v := range []string{"1", "2"} {
		if err := c.SetLabels(ctx, name, msb.Label{Key: "orcasm-test.schema", Value: v}); err != nil {
			t.Fatal(err)
		}
		labels, err := c.Labels(ctx, name)
		if err != nil {
			t.Fatal(err)
		}
		if labels["orcasm-test.schema"] != v || labels["orcasm-test"] != "1" {
			t.Errorf("labels = %v, want orcasm-test.schema=%s next to orcasm-test=1", labels, v)
		}
	}
	if got := status(t, c, name); got != msb.Running {
		t.Errorf("setting a label left the sandbox %q, want it still running", got)
	}
}
