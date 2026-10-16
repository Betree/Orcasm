package sandbox

// Which Orca release Sandboxes run (ADR 0006): the newest one the
// compatibility file (internal/compat) lists as tested with this orcasm,
// OrcaVersion when it cannot be read, or Orca's latest release on request.
// OrcaVersionEnv overrides all of them.

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/Betree/Orcasm/internal/compat"
)

// OrcaVersionEnv names a hidden override of the Orca release Sandboxes run,
// for CI: the end-to-end suite tests an Orca release before the
// compatibility file lists it (#24). It is not a Wizard answer, since the
// Wizard only collects the user's.
const OrcaVersionEnv = "ORCASM_ORCA_VERSION"

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
	// Override, when set, is the Orca release to run, whatever the
	// compatibility file or --latest say (OrcaVersionEnv).
	Override string
}

// NewOrcaSource reads the published compatibility file and GitHub, for an
// orcasm at version orcasm, unless OrcaVersionEnv overrides them.
func NewOrcaSource(orcasm string) OrcaSource {
	return OrcaSource{
		Client:    http.DefaultClient,
		FileURL:   compat.FileURL,
		LatestURL: compat.LatestOrcaURL,
		Orcasm:    orcasm,
		Override:  strings.TrimPrefix(os.Getenv(OrcaVersionEnv), "v"),
	}
}

// OrcaTarget is an Orca release to run, and where that choice comes from,
// for progress lines.
type OrcaTarget struct {
	Version, From string
}

// Desired is the Orca release Sandboxes should run. With latest it is Orca's latest release, untested
// with orcasm, and failing to read it is an error; otherwise it falls back
// to OrcaVersion. An Override is returned as is, without any request.
func (s OrcaSource) Desired(ctx context.Context, latest bool) (OrcaTarget, error) {
	if s.Override != "" {
		return OrcaTarget{s.Override, OrcaVersionEnv + " override, maybe untested with this orcasm"}, nil
	}
	timeout := s.Timeout
	if timeout == 0 {
		timeout = 5 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if latest {
		v, err := compat.LatestOrca(ctx, s.Client, s.LatestURL)
		if err != nil {
			return OrcaTarget{}, fmt.Errorf("Orca's latest release: %w", err)
		}
		return OrcaTarget{v, "latest Orca release, untested with this orcasm"}, nil
	}
	f, err := compat.Fetch(ctx, s.Client, s.FileURL)
	if err != nil {
		return OrcaTarget{OrcaVersion, fmt.Sprintf("built in; compatibility file unreadable: %v", err)}, nil
	}
	v, ok := f.Orca(s.Orcasm)
	if !ok {
		return OrcaTarget{OrcaVersion, fmt.Sprintf("built in; compatibility file lists none for orcasm %s", s.Orcasm)}, nil
	}
	return OrcaTarget{v, "compatibility file"}, nil
}
