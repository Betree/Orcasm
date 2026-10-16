package selfupdate_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Betree/Orcasm/internal/selfupdate"
)

// fakeGitHub serves the URL layout of github.com/<owner>/<repo>/releases:
// /releases/latest redirects to the latest tag's page, and assets are under
// /releases/download/<tag>/<asset>.
type fakeGitHub struct {
	latest string                       // tag; empty means no release yet
	assets map[string]map[string][]byte // tag → asset name → content
}

func (f *fakeGitHub) start(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /releases/latest", func(w http.ResponseWriter, r *http.Request) {
		if f.latest == "" {
			// What github.com does for a repository without releases.
			http.Redirect(w, r, "/releases", http.StatusFound)
			return
		}
		http.Redirect(w, r, "/releases/tag/"+f.latest, http.StatusFound)
	})
	mux.HandleFunc("GET /releases/download/{tag}/{asset}", func(w http.ResponseWriter, r *http.Request) {
		body, ok := f.assets[r.PathValue("tag")][r.PathValue("asset")]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Write(body)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// release publishes tag with an orcasm_<goos>_<goarch>.tar.gz holding binary
// and a matching checksums.txt, the way GoReleaser does.
func (f *fakeGitHub) release(t *testing.T, tag, goos, goarch string, binary []byte) {
	t.Helper()
	archive := fmt.Sprintf("orcasm_%s_%s.tar.gz", goos, goarch)
	tgz := tarGz(t, map[string][]byte{"LICENSE": []byte("license"), "orcasm": binary})
	if f.assets == nil {
		f.assets = map[string]map[string][]byte{}
	}
	f.assets[tag] = map[string][]byte{
		archive:         tgz,
		"checksums.txt": []byte(fmt.Sprintf("%x  orcasm_other_arch.tar.gz\n%x  %s\n", sha256.Sum256(nil), sha256.Sum256(tgz), archive)),
	}
}

func tarGz(t *testing.T, files map[string][]byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, body := range files {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		tw.Write(body)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	gz.Close()
	return buf.Bytes()
}

func TestLatestReadsTheTagGitHubRedirectsTo(t *testing.T) {
	gh := &fakeGitHub{latest: "v0.3.0"}
	srv := gh.start(t)

	tag, err := selfupdate.Releases{BaseURL: srv.URL}.Latest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if tag != "v0.3.0" {
		t.Fatalf("Latest = %q, want v0.3.0", tag)
	}
}

func TestLatestFailsClearlyBeforeTheFirstRelease(t *testing.T) {
	srv := (&fakeGitHub{}).start(t)

	_, err := selfupdate.Releases{BaseURL: srv.URL}.Latest(context.Background())
	if err == nil || !strings.Contains(err.Error(), "no orcasm release") {
		t.Fatalf("want an error saying no release is published, got %v", err)
	}
}
