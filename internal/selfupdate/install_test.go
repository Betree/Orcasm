package selfupdate_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Betree/Orcasm/internal/selfupdate"
)

// oldBinary writes a stand-in for the installed orcasm and returns its path.
func oldBinary(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "orcasm")
	if err := os.WriteFile(p, []byte("old orcasm"), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func assertContent(t *testing.T, p, want string) {
	t.Helper()
	got, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Fatalf("%s holds %q, want %q", p, got, want)
	}
}

func TestInstallReplacesTheBinaryWithTheReleasedOne(t *testing.T) {
	gh := &fakeGitHub{}
	gh.release(t, "v0.3.0", "linux", "amd64", []byte("new orcasm"))
	srv := gh.start(t)
	target := oldBinary(t)

	if err := (selfupdate.Releases{BaseURL: srv.URL}).Install(context.Background(), "v0.3.0", "linux", "amd64", target); err != nil {
		t.Fatal(err)
	}

	assertContent(t, target, "new orcasm")
	info, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o755 {
		t.Fatalf("mode = %v, want 0755", info.Mode().Perm())
	}
	leftovers, _ := filepath.Glob(filepath.Join(filepath.Dir(target), "*"))
	if len(leftovers) != 1 {
		t.Fatalf("want only the binary left in its directory, got %v", leftovers)
	}
}

func TestInstallReplacesWhatASymlinkPointsTo(t *testing.T) {
	gh := &fakeGitHub{}
	gh.release(t, "v0.3.0", "linux", "arm64", []byte("new orcasm"))
	srv := gh.start(t)
	real := oldBinary(t)
	link := filepath.Join(t.TempDir(), "orcasm")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}

	if err := (selfupdate.Releases{BaseURL: srv.URL}).Install(context.Background(), "v0.3.0", "linux", "arm64", link); err != nil {
		t.Fatal(err)
	}

	assertContent(t, real, "new orcasm")
	if fi, err := os.Lstat(link); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("the symlink should stay a symlink (%v)", err)
	}
}

func TestInstallLeavesTheBinaryAloneWhenTheChecksumIsWrong(t *testing.T) {
	gh := &fakeGitHub{}
	gh.release(t, "v0.3.0", "linux", "amd64", []byte("new orcasm"))
	gh.assets["v0.3.0"]["orcasm_linux_amd64.tar.gz"] = tarGz(t, map[string][]byte{"orcasm": []byte("tampered")})
	srv := gh.start(t)
	target := oldBinary(t)

	err := (selfupdate.Releases{BaseURL: srv.URL}).Install(context.Background(), "v0.3.0", "linux", "amd64", target)
	if err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("want a checksum error, got %v", err)
	}
	assertContent(t, target, "old orcasm")
}

func TestInstallRefusesAnArchiveNotListedInTheChecksums(t *testing.T) {
	gh := &fakeGitHub{}
	gh.release(t, "v0.3.0", "linux", "amd64", []byte("new orcasm"))
	gh.assets["v0.3.0"]["checksums.txt"] = []byte("")
	srv := gh.start(t)
	target := oldBinary(t)

	if err := (selfupdate.Releases{BaseURL: srv.URL}).Install(context.Background(), "v0.3.0", "linux", "amd64", target); err == nil {
		t.Fatal("want an error when checksums.txt does not list the archive")
	}
	assertContent(t, target, "old orcasm")
}

func TestInstallFailsWhenTheReleaseHasNoBuildForThisHost(t *testing.T) {
	gh := &fakeGitHub{}
	gh.release(t, "v0.3.0", "linux", "amd64", []byte("new orcasm"))
	srv := gh.start(t)
	target := oldBinary(t)

	if err := (selfupdate.Releases{BaseURL: srv.URL}).Install(context.Background(), "v0.3.0", "freebsd", "amd64", target); err == nil {
		t.Fatal("want an error for a missing archive")
	}
	assertContent(t, target, "old orcasm")
}
