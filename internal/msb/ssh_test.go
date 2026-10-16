package msb_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Betree/Orcasm/internal/msb"
)

func keygen(t *testing.T, path string) string {
	t.Helper()
	if out, err := exec.Command("ssh-keygen", "-q", "-t", "ed25519", "-N", "", "-C", "test", "-f", path).CombinedOutput(); err != nil {
		t.Fatalf("ssh-keygen: %v: %s", err, out)
	}
	pub, err := os.ReadFile(path + ".pub")
	if err != nil {
		t.Fatal(err)
	}
	return string(pub)
}

func TestSandboxHostKeyDerivesPublicKeyFromSandboxHostKey(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, "sandboxes", "demo", "ssh")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	pub := keygen(t, filepath.Join(dir, "host_ed25519"))
	want := strings.Join(strings.Fields(pub)[:2], " ")

	got, err := msb.CLI{Home: home}.SandboxHostKey("demo")
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Errorf("SandboxHostKey = %q, want %q", got, want)
	}
}

func TestSandboxHostKeyUnknownSandbox(t *testing.T) {
	if _, err := (msb.CLI{Home: t.TempDir()}).SandboxHostKey("nope"); err == nil {
		t.Fatal("expected error for unknown sandbox")
	}
}

// Runs the real msb against a throwaway MSB_HOME, so the user's global
// authorized_keys is never touched.
func TestAuthorizeSSHKeyWithRealMSB(t *testing.T) {
	if _, err := exec.LookPath("msb"); err != nil {
		t.Skip("msb not on PATH")
	}
	home := t.TempDir()
	key := filepath.Join(t.TempDir(), "id")
	pub := keygen(t, key)
	c := msb.CLI{Home: home}
	for range 2 { // idempotent
		if err := c.AuthorizeSSHKey(key + ".pub"); err != nil {
			t.Fatal(err)
		}
	}
	b, err := os.ReadFile(filepath.Join(home, "ssh", "authorized_keys"))
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(b), strings.Fields(pub)[1]); n != 1 {
		t.Errorf("key appears %d times in authorized_keys:\n%s", n, b)
	}
}
