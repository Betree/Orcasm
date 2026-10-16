package compat_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Betree/Orcasm/internal/compat"
)

const published = `{
  "orca": [
    {"version": "1.4.227", "min_orcasm": "0.1.0"},
    {"version": "1.4.230", "min_orcasm": "0.3.0"},
    {"version": "1.4.229", "min_orcasm": "0.2.0"}
  ]
}`

func parse(t *testing.T, raw string) compat.File {
	t.Helper()
	f, err := compat.Parse([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func TestAnOrcasmTakesTheNewestOrcaTestedWithItsVersionOrOlder(t *testing.T) {
	f := parse(t, published)
	for orcasm, want := range map[string]string{
		"0.1.0":  "1.4.227",
		"0.2.5":  "1.4.229",
		"v0.3.0": "1.4.230",
		"1.0.0":  "1.4.230",
	} {
		got, ok := f.Orca(orcasm)
		if !ok || got != want {
			t.Errorf("Orca(%q) = %q, %v; want %q", orcasm, got, ok, want)
		}
	}
}

func TestADevBuildTakesTheNewestTestedOrca(t *testing.T) {
	if got, ok := parse(t, published).Orca("dev"); !ok || got != "1.4.230" {
		t.Errorf("Orca(dev) = %q, %v; want 1.4.230", got, ok)
	}
}

func TestAnOrcasmOlderThanEveryEntryHasNoTestedOrca(t *testing.T) {
	if got, ok := parse(t, published).Orca("0.0.9"); ok {
		t.Errorf("Orca(0.0.9) = %q, want none", got)
	}
}

func TestOrcaVersionsCompareNumerically(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want int
	}{
		{"1.4.227", "1.4.230", -1},
		{"1.4.30", "1.4.227", -1},
		{"v1.5.0", "1.4.999", 1},
		{"1.4.227", "1.4.227", 0},
		{"1.4", "1.4.0", 0},
	} {
		if got := compat.Compare(c.a, c.b); got != c.want {
			t.Errorf("Compare(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestAFileWithoutVersionsIsRefused(t *testing.T) {
	for _, raw := range []string{`{}`, `{"orca": [{"min_orcasm": "0.1.0"}]}`, `not json`} {
		if _, err := compat.Parse([]byte(raw)); err == nil {
			t.Errorf("Parse(%s) succeeded", raw)
		}
	}
}

func TestTheRepositorysCompatibilityFileIsValid(t *testing.T) {
	// CI (#24) rewrites it: it must stay parseable and list some Orca.
	raw, err := os.ReadFile("../../compatibility.json")
	if err != nil {
		t.Fatal(err)
	}
	f := parse(t, string(raw))
	if _, ok := f.Orca("dev"); !ok {
		t.Error("lists no Orca")
	}
}

func TestMarkingAnOrcaTestedAddsItAsTheNewestForThatOrcasm(t *testing.T) {
	out, added, err := compat.MarkTested([]byte(published), compat.Entry{Version: "v1.4.231", MinOrcasm: "v0.3.1"})
	if err != nil || !added {
		t.Fatalf("MarkTested = %v, %v", added, err)
	}
	f := parse(t, string(out))
	if got, _ := f.Orca("0.3.1"); got != "1.4.231" {
		t.Errorf("Orca(0.3.1) = %q, want 1.4.231", got)
	}
	if got, _ := f.Orca("0.3.0"); got != "1.4.230" {
		t.Errorf("Orca(0.3.0) = %q, want 1.4.230: older orcasms never ran 1.4.231", got)
	}
	if !strings.HasSuffix(string(out), "}\n") {
		t.Errorf("want a trailing newline: %q", out)
	}
}

func TestMarkingAnAlreadyListedOrcaTestedChangesNothing(t *testing.T) {
	// The oldest orcasm it passed with stays the one recorded.
	out, added, err := compat.MarkTested([]byte(published), compat.Entry{Version: "1.4.229", MinOrcasm: "0.3.0"})
	if err != nil || added {
		t.Fatalf("MarkTested = %v, %v; want nothing added", added, err)
	}
	if string(out) != published {
		t.Errorf("file rewritten:\n%s", out)
	}
}

func TestACorruptCompatibilityFileIsNotRewritten(t *testing.T) {
	if _, _, err := compat.MarkTested([]byte(`{"orca": [`), compat.Entry{Version: "1.4.231", MinOrcasm: "0.0.0"}); err == nil {
		t.Error("want an error")
	}
}

func TestFetchReadsThePublishedFile(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(published))
	}))
	defer srv.Close()

	f, err := compat.Fetch(context.Background(), srv.Client(), srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := f.Orca("dev"); got != "1.4.230" {
		t.Errorf("Orca(dev) = %q", got)
	}
}

func TestFetchFailsOnAnHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()

	if _, err := compat.Fetch(context.Background(), srv.Client(), srv.URL); err == nil || !strings.Contains(err.Error(), "404") {
		t.Errorf("err = %v, want a 404", err)
	}
}

func TestFetchGivesUpWhenItsContextEnds(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(5 * time.Second):
		}
	}))
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	if _, err := compat.Fetch(ctx, srv.Client(), srv.URL); err == nil {
		t.Error("want a timeout")
	}
}

func TestLatestOrcaIsTheLatestGitHubReleaseWithoutItsV(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`{"tag_name": "v1.4.231", "name": "Orca 1.4.231"}`))
	}))
	defer srv.Close()

	got, err := compat.LatestOrca(context.Background(), srv.Client(), srv.URL)
	if err != nil || got != "1.4.231" {
		t.Errorf("LatestOrca = %q, %v; want 1.4.231", got, err)
	}
}
