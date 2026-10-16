package selfupdate

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Archive is the release asset holding orcasm for goos/goarch. The name is
// fixed by .goreleaser.yaml (archives.name_template) and install.sh.
func Archive(goos, goarch string) string {
	return fmt.Sprintf("orcasm_%s_%s.tar.gz", goos, goarch)
}

// Install downloads release tag's build for goos/goarch, verifies it against
// the release's checksums.txt and replaces target with it. A symlinked target
// is followed, so the file it points to is replaced and the link kept. The
// replacement is a rename within target's directory, so target is either the
// old binary or the new one, never half-written, and a failed download or
// checksum leaves it untouched.
func (r Releases) Install(ctx context.Context, tag, goos, goarch, target string) error {
	archive := Archive(goos, goarch)
	sums, err := r.download(ctx, tag, "checksums.txt")
	if err != nil {
		return err
	}
	want, err := checksum(sums, archive)
	if err != nil {
		return fmt.Errorf("orcasm %s: %w", tag, err)
	}
	tgz, err := r.download(ctx, tag, archive)
	if err != nil {
		return err
	}
	if got := sha256.Sum256(tgz); hex.EncodeToString(got[:]) != want {
		return fmt.Errorf("checksum mismatch for %s of orcasm %s; nothing was replaced", archive, tag)
	}
	binary, err := extract(tgz, "orcasm")
	if err != nil {
		return fmt.Errorf("%s of orcasm %s: %w", archive, tag, err)
	}
	return replace(target, binary)
}

// checksum is the sha256 checksums.txt lists for name ("<hex>  <name>"
// lines, as sha256sum prints them).
func checksum(sums []byte, name string) (string, error) {
	sc := bufio.NewScanner(bytes.NewReader(sums))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) == 2 && f[1] == name {
			return strings.ToLower(f[0]), nil
		}
	}
	return "", fmt.Errorf("checksums.txt lists no checksum for %s", name)
}

// extract is the content of the regular file called name in a tar.gz.
func extract(tgz []byte, name string) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(tgz))
	if err != nil {
		return nil, err
	}
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("no %s in the archive", name)
		}
		if err != nil {
			return nil, err
		}
		if h.Typeflag == tar.TypeReg && filepath.Clean(h.Name) == name {
			return io.ReadAll(io.LimitReader(tr, maxDownload))
		}
	}
}

// replace atomically swaps the file target resolves to for content.
func replace(target string, content []byte) error {
	resolved, err := filepath.EvalSymlinks(target)
	if err != nil {
		return err
	}
	dir := filepath.Dir(resolved)
	tmp, err := os.CreateTemp(dir, ".orcasm-update-*")
	if err != nil {
		if errors.Is(err, fs.ErrPermission) {
			return fmt.Errorf("cannot write to %s: rerun with sudo, or reinstall elsewhere with install.sh and ORCASM_INSTALL_DIR", dir)
		}
		return err
	}
	defer os.Remove(tmp.Name()) // no-op once renamed
	if _, err := tmp.Write(content); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), 0o755); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), resolved)
}
