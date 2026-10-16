package sandbox

// Which Orca release Sandboxes run (ADR 0006): the newest one the
// compatibility file (internal/compat) lists as tested with this orcasm,
// OrcaVersion when it cannot be read, or Orca's latest release on request.

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/Betree/Orcasm/internal/compat"
)

// OrcaSource tells which Orca release Sandboxes should run.
type OrcaSource struct {
	Client *http.Client
	// FileURL is the compatibility file (compat.FileURL) and LatestURL
	// Orca's latest release (compat.LatestOrcaURL).
	FileURL, LatestURL string
	// Orcasm is this orcasm's version (BuildInfo.Version).
	Orcasm string
	// Timeout bounds each request (default 5s): offline, the built-in
	// release is used instead of waiting.
	Timeout time.Duration
}

// NewOrcaSource reads the published compatibility file and GitHub, for an
// orcasm at version orcasm.
func NewOrcaSource(orcasm string) OrcaSource {
	return OrcaSource{Client: http.DefaultClient, FileURL: compat.FileURL, LatestURL: compat.LatestOrcaURL, Orcasm: orcasm}
}

// Desired is the Orca release Sandboxes should run, and where it comes from
// for progress lines. With latest it is Orca's latest release, untested
// with orcasm, and failing to read it is an error; otherwise it falls back
// to OrcaVersion.
func (s OrcaSource) Desired(ctx context.Context, latest bool) (version, from string, err error) {
	timeout := s.Timeout
	if timeout == 0 {
		timeout = 5 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if latest {
		v, err := compat.LatestOrca(ctx, s.Client, s.LatestURL)
		if err != nil {
			return "", "", fmt.Errorf("Orca's latest release: %w", err)
		}
		return v, "latest Orca release, untested with this orcasm", nil
	}
	f, err := compat.Fetch(ctx, s.Client, s.FileURL)
	if err != nil {
		return OrcaVersion, fmt.Sprintf("built in; compatibility file unreadable: %v", err), nil
	}
	v, ok := f.Orca(s.Orcasm)
	if !ok {
		return OrcaVersion, fmt.Sprintf("built in; compatibility file lists none for orcasm %s", s.Orcasm), nil
	}
	return v, "compatibility file", nil
}
