package sshaccess_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Betree/Orcasm/internal/msb"
	"github.com/Betree/Orcasm/internal/sshaccess"
)

var _ sshaccess.MSB = msb.CLI{}

const hostKey = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIHostKeyForTests"

type fakeMSB struct {
	authorized []string
	hostKeys   map[string]string
}

func (f *fakeMSB) AuthorizeSSHKey(_ context.Context, pubKeyFile string) error {
	b, err := os.ReadFile(pubKeyFile)
	if err != nil {
		return err
	}
	f.authorized = append(f.authorized, strings.TrimSpace(string(b)))
	return nil
}

func (f *fakeMSB) SandboxHostKey(name string) (string, error) {
	if k, ok := f.hostKeys[name]; ok {
		return k, nil
	}
	return hostKey, nil
}

func newAccess(t *testing.T) (sshaccess.Access, *fakeMSB) {
	t.Helper()
	tmp := t.TempDir()
	m := &fakeMSB{hostKeys: map[string]string{}}
	return sshaccess.Access{
		UserSSHDir: filepath.Join(tmp, "home", ".ssh"),
		Dir:        filepath.Join(tmp, "config", "orcasm", "ssh"),
		MSB:        m,
	}, m
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestAddWritesHostEntryForSandbox(t *testing.T) {
	a, _ := newAccess(t)
	if err := a.Add(context.Background(), "demo"); err != nil {
		t.Fatal(err)
	}
	cfg := read(t, filepath.Join(a.Dir, "config"))
	for _, want := range []string{
		"Host orcasm-demo\n",
		"  User dev\n",
		"  ProxyCommand msb ssh serve demo --stdio --no-inactivity-timeout\n",
		"  IdentityFile " + filepath.Join(a.Dir, "id_ed25519") + "\n",
		"  IdentitiesOnly yes\n",
		"  UserKnownHostsFile " + filepath.Join(a.Dir, "known_hosts") + "\n",
		"  StrictHostKeyChecking yes\n",
		"  ServerAliveInterval 30\n",
	} {
		if !strings.Contains(cfg, want) {
			t.Errorf("config missing %q:\n%s", want, cfg)
		}
	}
}

func TestAddGeneratesOrcasmKeyAndAuthorizesItInMSB(t *testing.T) {
	a, m := newAccess(t)
	if err := a.Add(context.Background(), "demo"); err != nil {
		t.Fatal(err)
	}
	pub := strings.TrimSpace(read(t, filepath.Join(a.Dir, "id_ed25519.pub")))
	if !strings.HasPrefix(pub, "ssh-ed25519 ") {
		t.Fatalf("public key = %q", pub)
	}
	info, err := os.Stat(filepath.Join(a.Dir, "id_ed25519"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("private key mode = %v, want 0600", info.Mode().Perm())
	}
	if len(m.authorized) != 1 || m.authorized[0] != pub {
		t.Errorf("authorized = %q, want [%q]", m.authorized, pub)
	}
}

func TestAddPinsSandboxHostKeyAndRefreshesItOnReAdd(t *testing.T) {
	a, m := newAccess(t)
	m.hostKeys["demo"] = "ssh-ed25519 OLDKEY"
	m.hostKeys["other"] = "ssh-ed25519 OTHERKEY"
	if err := a.Add(context.Background(), "demo"); err != nil {
		t.Fatal(err)
	}
	if err := a.Add(context.Background(), "other"); err != nil {
		t.Fatal(err)
	}
	m.hostKeys["demo"] = "ssh-ed25519 NEWKEY"
	if err := a.Add(context.Background(), "demo"); err != nil {
		t.Fatal(err)
	}
	got := read(t, filepath.Join(a.Dir, "known_hosts"))
	want := "orcasm-other ssh-ed25519 OTHERKEY\norcasm-demo ssh-ed25519 NEWKEY\n"
	if got != want {
		t.Errorf("known_hosts =\n%s\nwant\n%s", got, want)
	}
	if n := strings.Count(read(t, filepath.Join(a.Dir, "config")), "Host orcasm-demo\n"); n != 1 {
		t.Errorf("config has %d orcasm-demo entries, want 1", n)
	}
}

func TestRemoveDropsOnlyThatSandboxEntry(t *testing.T) {
	a, m := newAccess(t)
	m.hostKeys["demo"] = "ssh-ed25519 DEMOKEY"
	m.hostKeys["other"] = "ssh-ed25519 OTHERKEY"
	for _, n := range []string{"demo", "other"} {
		if err := a.Add(context.Background(), n); err != nil {
			t.Fatal(err)
		}
	}
	if err := a.Remove("demo"); err != nil {
		t.Fatal(err)
	}
	cfg := read(t, filepath.Join(a.Dir, "config"))
	if strings.Contains(cfg, "orcasm-demo") || strings.Contains(cfg, "serve demo ") {
		t.Errorf("config still mentions demo:\n%s", cfg)
	}
	if !strings.Contains(cfg, "Host orcasm-other\n") {
		t.Errorf("config lost other:\n%s", cfg)
	}
	if got := read(t, filepath.Join(a.Dir, "known_hosts")); got != "orcasm-other ssh-ed25519 OTHERKEY\n" {
		t.Errorf("known_hosts = %q", got)
	}
}

func TestRemoveUnknownSandboxIsNoOp(t *testing.T) {
	a, _ := newAccess(t)
	if err := a.Remove("never-added"); err != nil {
		t.Fatalf("Remove on empty state: %v", err)
	}
	if _, err := os.Stat(a.Dir); !os.IsNotExist(err) {
		t.Errorf("Remove created %s", a.Dir)
	}
}

func TestLinkUserConfigPrependsIncludeAndKeepsUserContent(t *testing.T) {
	a, _ := newAccess(t)
	userCfg := filepath.Join(a.UserSSHDir, "config")
	if err := os.MkdirAll(a.UserSSHDir, 0o700); err != nil {
		t.Fatal(err)
	}
	original := "Host work\n  HostName example.com\n"
	if err := os.WriteFile(userCfg, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	if linked, _ := a.UserConfigLinked(); linked {
		t.Fatal("linked before LinkUserConfig")
	}
	if err := a.LinkUserConfig(); err != nil {
		t.Fatal(err)
	}
	if err := a.LinkUserConfig(); err != nil {
		t.Fatal(err)
	}

	got := read(t, userCfg)
	include := "Include " + filepath.Join(a.Dir, "config") + "\n"
	// Include must precede any Host block, or ssh scopes it to that block.
	if !strings.HasPrefix(got, "# Added by orcasm") || !strings.Contains(got, include) {
		t.Fatalf("user config =\n%s", got)
	}
	if strings.Index(got, include) > strings.Index(got, "Host work") {
		t.Errorf("Include after Host block:\n%s", got)
	}
	if strings.Count(got, include) != 1 {
		t.Errorf("Include added more than once:\n%s", got)
	}
	if !strings.HasSuffix(got, original) {
		t.Errorf("user content altered:\n%s", got)
	}
	if info, _ := os.Stat(userCfg); info.Mode().Perm() != 0o644 {
		t.Errorf("mode changed to %v", info.Mode().Perm())
	}
	if linked, _ := a.UserConfigLinked(); !linked {
		t.Error("not linked after LinkUserConfig")
	}

	if err := a.UnlinkUserConfig(); err != nil {
		t.Fatal(err)
	}
	if got := read(t, userCfg); got != original {
		t.Errorf("after unlink =\n%q\nwant\n%q", got, original)
	}
}

func TestLinkUserConfigCreatesMissingConfig(t *testing.T) {
	a, _ := newAccess(t)
	if err := a.LinkUserConfig(); err != nil {
		t.Fatal(err)
	}
	userCfg := filepath.Join(a.UserSSHDir, "config")
	if !strings.Contains(read(t, userCfg), "Include "+filepath.Join(a.Dir, "config")+"\n") {
		t.Errorf("no Include in new config")
	}
	if info, _ := os.Stat(userCfg); info.Mode().Perm() != 0o600 {
		t.Errorf("new config mode = %v, want 0600", info.Mode().Perm())
	}
	if info, _ := os.Stat(a.UserSSHDir); info.Mode().Perm() != 0o700 {
		t.Errorf("new ~/.ssh mode = %v, want 0700", info.Mode().Perm())
	}
}

func TestCommandReachesTheSandboxWithoutTheUserConfig(t *testing.T) {
	a, _ := newAccess(t)
	if err := a.Add(context.Background(), "demo"); err != nil {
		t.Fatal(err)
	}

	// -G makes ssh print the configuration it would use, without connecting.
	argv := a.Command("demo", "-G")
	out, err := exec.Command(argv[0], argv[1:]...).CombinedOutput()
	if err != nil {
		t.Fatalf("%v: %v\n%s", argv, err, out)
	}

	for _, want := range []string{"user dev\n", "proxycommand msb ssh serve demo"} {
		if !strings.Contains(string(out), want) {
			t.Errorf("ssh -G output lacks %q:\n%s", want, out)
		}
	}
}

func TestAddReusesExistingKey(t *testing.T) {
	a, m := newAccess(t)
	if err := a.Add(context.Background(), "one"); err != nil {
		t.Fatal(err)
	}
	if err := a.Add(context.Background(), "two"); err != nil {
		t.Fatal(err)
	}
	if len(m.authorized) != 2 || m.authorized[0] != m.authorized[1] {
		t.Errorf("expected the same key authorized twice, got %q", m.authorized)
	}
}
