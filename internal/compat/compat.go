// Package compat reads the compatibility file: the Orca releases the
// end-to-end suite passed with, published from this repository
// (compatibility.json at its root) so that most Orca bumps need no orcasm
// release (ADR 0006). CI rewrites it when a new Orca release passes (#24).
//
// Its format is JSON, for `jq`:
//
//	{"orca": [{"version": "1.4.227", "min_orcasm": "0.1.0"}, ...]}
//
// Each entry is an Orca release and the oldest orcasm it was tested with:
// orcasm's Orca touchpoints only move forward, so an Orca tested with orcasm
// 0.3.0 is not adopted by an orcasm 0.2.x, which never ran against it. Entry
// order does not matter.
package compat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/Betree/Orcasm/internal/version"
)

const (
	// FileURL is where the compatibility file is published: the main
	// branch of this repository, like install.sh.
	FileURL = "https://raw.githubusercontent.com/Betree/Orcasm/main/compatibility.json"
	// LatestOrcaURL is Orca's latest release on GitHub. GitHub's "latest"
	// skips pre-releases, such as Orca's mobile-android-v* ones.
	LatestOrcaURL = "https://api.github.com/repos/stablyai/orca/releases/latest"
)

// File is a parsed compatibility file.
type File struct {
	Releases []Entry `json:"orca"`
}

// Entry is one tested Orca release.
type Entry struct {
	// Version is the Orca release, without a v ("1.4.227").
	Version string `json:"version"`
	// MinOrcasm is the oldest orcasm release it was tested with.
	MinOrcasm string `json:"min_orcasm"`
}

// Parse reads a compatibility file.
func Parse(raw []byte) (File, error) {
	var f File
	if err := json.Unmarshal(raw, &f); err != nil {
		return File{}, fmt.Errorf("compatibility file: %w", err)
	}
	if len(f.Releases) == 0 {
		return File{}, errors.New("compatibility file lists no Orca release")
	}
	for i, e := range f.Releases {
		if e.Version == "" {
			return File{}, fmt.Errorf("compatibility file: Orca entry %d has no version", i+1)
		}
	}
	return f, nil
}

// MarkTested records that the end-to-end suite passed with Orca e.Version
// and orcasm e.MinOrcasm, for CI's Orca release watcher (#24). It returns the
// rewritten file, or raw unchanged and added false when that Orca is
// already listed: its recorded min_orcasm, the oldest it passed with, stays.
// A corrupt raw is an error rather than replaced, so a bad edit is caught
// before it is published.
func MarkTested(raw []byte, e Entry) (out []byte, added bool, err error) {
	f, err := Parse(raw)
	if err != nil {
		return nil, false, err
	}
	e.Version, e.MinOrcasm = strings.TrimPrefix(e.Version, "v"), strings.TrimPrefix(e.MinOrcasm, "v")
	if !version.IsRelease(e.Version) || !version.IsRelease(e.MinOrcasm) {
		return nil, false, fmt.Errorf("compatibility entry %+v: want release numbers", e)
	}
	if f.Lists(e.Version) {
		return raw, false, nil
	}
	f.Releases = append(f.Releases, e)
	out, err = json.MarshalIndent(f, "", "  ")
	if err != nil {
		return nil, false, err
	}
	return append(out, '\n'), true, nil
}

// Lists reports whether the file lists Orca release orca.
func (f File) Lists(orca string) bool {
	for _, e := range f.Releases {
		if version.Compare(e.Version, orca) == 0 {
			return true
		}
	}
	return false
}

// Newest is the newest Orca release listed, whichever orcasm it needs.
func (f File) Newest() string {
	var newest string
	for _, e := range f.Releases {
		if newest == "" || version.Compare(e.Version, newest) > 0 {
			newest = e.Version
		}
	}
	return newest
}

// Orca is the newest Orca release tested with an orcasm at orcasmVersion or
// older. A development build (a version that is not a release number, like
// "dev") takes the newest of all. ok is false when none qualifies.
func (f File) Orca(orcasmVersion string) (orca string, ok bool) {
	dev := !version.IsRelease(orcasmVersion)
	for _, e := range f.Releases {
		if !dev && version.Compare(e.MinOrcasm, orcasmVersion) > 0 {
			continue
		}
		if !ok || version.Compare(e.Version, orca) > 0 {
			orca, ok = e.Version, true
		}
	}
	return orca, ok
}

// Fetch downloads and parses the compatibility file at url. ctx bounds it:
// callers give it a short deadline, falling back to the Orca release built
// into orcasm when offline.
func Fetch(ctx context.Context, client *http.Client, url string) (File, error) {
	raw, err := get(ctx, client, url)
	if err != nil {
		return File{}, err
	}
	return Parse(raw)
}

// LatestOrca is the version of Orca's latest release, read from GitHub's
// releases API at url (LatestOrcaURL), for `orcasm upgrade --latest`.
func LatestOrca(ctx context.Context, client *http.Client, url string) (string, error) {
	raw, err := get(ctx, client, url)
	if err != nil {
		return "", err
	}
	var rel struct {
		Tag string `json:"tag_name"`
	}
	if err := json.Unmarshal(raw, &rel); err != nil {
		return "", fmt.Errorf("Orca's latest release: %w", err)
	}
	if rel.Tag == "" {
		return "", errors.New("Orca's latest release has no tag")
	}
	return strings.TrimPrefix(rel.Tag, "v"), nil
}

func get(ctx context.Context, client *http.Client, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	// Both documents are small; a cap keeps a misbehaving server harmless.
	return io.ReadAll(io.LimitReader(resp.Body, 1<<20))
}
