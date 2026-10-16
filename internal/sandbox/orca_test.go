package sandbox_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Betree/Orcasm/internal/sandbox"
)

// orcaServer publishes a compatibility file and Orca's latest release.
func orcaServer(t *testing.T, file string) sandbox.OrcaSource {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/compatibility.json", func(w http.ResponseWriter, _ *http.Request) {
		if file == "" {
			http.Error(w, "gone", http.StatusInternalServerError)
			return
		}
		w.Write([]byte(file))
	})
	mux.HandleFunc("/latest", func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`{"tag_name": "v1.4.240"}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return sandbox.OrcaSource{Client: srv.Client(), FileURL: srv.URL + "/compatibility.json", LatestURL: srv.URL + "/latest", Orcasm: "0.2.0"}
}

func TestTheDesiredOrcaIsTheNewestTestedWithThisOrcasm(t *testing.T) {
	src := orcaServer(t, `{"orca": [{"version": "1.4.230", "min_orcasm": "0.2.0"}, {"version": "1.4.235", "min_orcasm": "0.3.0"}]}`)

	got, from, err := src.Desired(context.Background(), false)
	if err != nil || got != "1.4.230" {
		t.Errorf("Desired = %q, %v; want 1.4.230", got, err)
	}
	if !strings.Contains(from, "compatibility file") {
		t.Errorf("from = %q", from)
	}
}

func TestOfflineTheDesiredOrcaIsTheOneBuiltIntoOrcasm(t *testing.T) {
	src := orcaServer(t, "")

	got, from, err := src.Desired(context.Background(), false)
	if err != nil || got != sandbox.OrcaVersion {
		t.Errorf("Desired = %q, %v; want %s", got, err, sandbox.OrcaVersion)
	}
	if !strings.Contains(from, "built in") {
		t.Errorf("from = %q, want it to say the version is built in", from)
	}
}

func TestWithNoOrcaTestedWithThisOrcasmTheBuiltInOneIsDesired(t *testing.T) {
	src := orcaServer(t, `{"orca": [{"version": "1.4.235", "min_orcasm": "0.3.0"}]}`)

	if got, _, err := src.Desired(context.Background(), false); err != nil || got != sandbox.OrcaVersion {
		t.Errorf("Desired = %q, %v; want %s", got, err, sandbox.OrcaVersion)
	}
}

func TestLatestBypassesTheCompatibilityFile(t *testing.T) {
	src := orcaServer(t, `{"orca": [{"version": "1.4.230", "min_orcasm": "0.0.0"}]}`)

	got, from, err := src.Desired(context.Background(), true)
	if err != nil || got != "1.4.240" {
		t.Errorf("Desired = %q, %v; want 1.4.240", got, err)
	}
	if !strings.Contains(from, "latest") {
		t.Errorf("from = %q", from)
	}
}
