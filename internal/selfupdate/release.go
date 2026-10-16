// Package selfupdate replaces the running orcasm binary with a newer GitHub
// release, the same way install.sh installs one: download
// orcasm_<os>_<arch>.tar.gz and checksums.txt, verify the sha256, and put the
// `orcasm` entry in place.
package selfupdate

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path"
	"strings"
)

// DefaultBaseURL is the orcasm repository on GitHub; release URLs hang off it.
const DefaultBaseURL = "https://github.com/Betree/Orcasm"

// maxDownload bounds what a release download may weigh; the binary is a few
// tens of MB, so this only guards against a runaway response.
const maxDownload = 512 << 20

// Releases reads GitHub releases of one repository through its web URLs
// rather than the REST API, which rate-limits anonymous callers.
type Releases struct {
	BaseURL string       // e.g. DefaultBaseURL; tests point it at a fake
	Client  *http.Client // nil means http.DefaultClient
}

// Latest is the tag of the latest release, read from where
// <base>/releases/latest redirects (…/releases/tag/<tag>).
func (r Releases) Latest(ctx context.Context) (string, error) {
	client := *r.client()
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, r.BaseURL+"/releases/latest", nil)
	if err != nil {
		return "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("look up the latest orcasm release: %w", err)
	}
	resp.Body.Close()
	loc := resp.Header.Get("Location")
	// Without any release, github.com redirects to the (empty) /releases list.
	if resp.StatusCode == http.StatusNotFound || (resp.StatusCode/100 == 3 && strings.HasSuffix(loc, "/releases")) {
		return "", errors.New("no orcasm release is published yet")
	}
	if resp.StatusCode/100 != 3 || !strings.Contains(loc, "/releases/tag/") {
		return "", fmt.Errorf("look up the latest orcasm release: unexpected %s from %s", resp.Status, req.URL)
	}
	return path.Base(loc), nil
}

// download fetches one asset of release tag.
func (r Releases) download(ctx context.Context, tag, asset string) ([]byte, error) {
	url := fmt.Sprintf("%s/releases/download/%s/%s", r.BaseURL, tag, asset)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := r.client().Do(req)
	if err != nil {
		return nil, fmt.Errorf("download %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download %s: %s", url, resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxDownload))
	if err != nil {
		return nil, fmt.Errorf("download %s: %w", url, err)
	}
	return body, nil
}

func (r Releases) client() *http.Client {
	if r.Client != nil {
		return r.Client
	}
	return http.DefaultClient
}
