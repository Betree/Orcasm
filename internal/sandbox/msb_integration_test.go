//go:build msb

package sandbox_test

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/Betree/Orcasm/internal/msb"
	"github.com/Betree/Orcasm/internal/sandbox"
)

// Runs against a real, throwaway, running Sandbox (label orcasm=1) that this
// test REMOVES at the end:
//
//	msb create ghcr.io/superradcompany/ubuntu-systemd:24.04 --init auto --name spike15-b --label orcasm=1
//	ORCASM_TEST_SANDBOX=spike15-b go test -tags msb -run RealMSB ./internal/sandbox/
//
// It installs git in the guest and builds a Main owned by a non-root user.
func TestRemoveChecksUnsavedWorkWithRealMSB(t *testing.T) {
	name := os.Getenv("ORCASM_TEST_SANDBOX")
	if name == "" {
		t.Skip("ORCASM_TEST_SANDBOX not set")
	}
	ctx := context.Background()
	c := msb.CLI{}
	sh := func(script string) string {
		t.Helper()
		out, err := c.Exec(ctx, name, "", "sh", "-c", script)
		if err != nil {
			t.Fatalf("%s: %v", script, err)
		}
		return out
	}
	sh(`command -v git >/dev/null || { apt-get update -qq && apt-get install -y -qq git >/dev/null; }
id dev >/dev/null 2>&1 || useradd -m dev
rm -rf /home/dev/main && mkdir /home/dev/main && cd /home/dev/main &&
git init -q -b main && echo v1 > README && git add . &&
git -c user.name=t -c user.email=t@t commit -q -m first && chown -R dev:dev /home/dev/main`)
	// Provisioning would have set these labels at create time.
	for _, l := range []string{"orcasm.main=/home/dev/main", "orcasm.folder=" + t.TempDir()} {
		if out, err := exec.Command("msb", "modify", name, "--restart", "--label", l).CombinedOutput(); err != nil {
			t.Fatalf("msb modify: %v: %s", err, out)
		}
	}

	ssh := &fakeSSH{}
	m := sandbox.Manager{MSB: c, SSH: ssh}
	var asked []string
	err := m.Remove(ctx, name, sandbox.RemoveOptions{Confirm: func(w []string) bool { asked = w; return false }})
	if err == nil || len(asked) != 1 || !strings.Contains(asked[0], "1 commit") {
		t.Fatalf("err = %v, asked = %q; want a refusal over 1 commit", err, asked)
	}

	sh(`echo edit >> /home/dev/main/README`)
	err = m.Remove(ctx, name, sandbox.RemoveOptions{Confirm: func(w []string) bool { asked = w; return true }})
	if err != nil {
		t.Fatal(err)
	}
	if len(asked) != 2 || !strings.Contains(asked[0], "/home/dev/main: uncommitted") {
		t.Errorf("asked = %q, want uncommitted changes then 1 commit", asked)
	}
	if list, _ := c.List(ctx, msb.Label{Key: "orcasm", Value: "1"}); len(list) > 0 {
		for _, l := range list {
			if l.Name == name {
				t.Error("sandbox still listed after Remove")
			}
		}
	}
	if len(ssh.removed) != 1 {
		t.Errorf("ssh entries removed = %v", ssh.removed)
	}
}
