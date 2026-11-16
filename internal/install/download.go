package install

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"time"
)

const (
	// miseReleases is where the pinned archives are downloaded from; GitHub redirects to its CDN.
	miseReleases = "https://github.com/jdx/mise/releases/download"
	miseMember   = "mise/bin/mise"
	// maxDownload caps the archive and the binary inside it; the real ones are a fraction of this.
	maxDownload     = 300 << 20
	downloadTimeout = 5 * time.Minute
	miseManualHint  = "install mise yourself: https://mise.jdx.dev"
)

// platform names the release asset's OS and architecture for a Go platform.
func platform(goos, goarch string) (string, error) {
	osName := map[string]string{"linux": "linux", "darwin": "macos"}[goos]
	arch := map[string]string{"amd64": "x64", "arm64": "arm64"}[goarch]
	if osName == "" || arch == "" {
		return "", fmt.Errorf("no pinned mise build for %s/%s; %s", goos, goarch, miseManualHint)
	}
	return osName + "-" + arch, nil
}

// installMise downloads the pinned mise for the platform, checks its sum and installs the binary as dir/mise. It
// never replaces an existing file.
func (in *installer) installMise(ctx context.Context, dir string) (string, error) {
	plat, err := platform(in.goos, in.goarch)
	if err != nil {
		return "", err
	}
	target := filepath.Join(dir, "mise")
	if _, err := os.Lstat(target); err == nil {
		return "", fmt.Errorf("%s exists but is not an executable mise; remove it or %s", target, miseManualHint)
	}
	asset := "mise-" + miseVersion + "-" + plat + ".tar.gz"
	archive, err := in.fetch(ctx, asset)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(archive)
	if got, want := hex.EncodeToString(sum[:]), in.sums[plat]; got != want {
		return "", fmt.Errorf("%s: SHA-256 is %s, the pin is %s; nothing was installed", asset, got, want)
	}
	bin, err := extract(archive, miseMember)
	if err != nil {
		return "", fmt.Errorf("%s: %w", asset, err)
	}
	if err := writeNew(target, bin); err != nil {
		return "", err
	}
	return target, nil
}

// fetch downloads one asset of the pinned release, at most maxDownload bytes.
func (in *installer) fetch(ctx context.Context, asset string) ([]byte, error) {
	u, err := url.Parse(in.baseURL)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return nil, fmt.Errorf("mise download URL %q is not an http(s) URL", in.baseURL)
	}
	u = u.JoinPath(miseVersion, asset)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("download %s: %w", asset, err)
	}
	resp, err := in.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download %s: %w", asset, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download %s: %s", u, resp.Status)
	}
	return readCapped(resp.Body, "download "+asset)
}

// readCapped reads r whole, failing beyond maxDownload bytes.
func readCapped(r io.Reader, what string) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, maxDownload+1))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", what, err)
	}
	if len(data) > maxDownload {
		return nil, fmt.Errorf("%s: larger than %d MB", what, maxDownload>>20)
	}
	return data, nil
}

// extract returns the regular file member of the gzipped tar archive.
func extract(archive []byte, member string) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, fmt.Errorf("not a gzip archive: %w", err)
	}
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("no %s in the archive", member)
		}
		if err != nil {
			return nil, fmt.Errorf("read the archive: %w", err)
		}
		if h.Name == member && h.Typeflag == tar.TypeReg {
			return readCapped(tr, "extract "+member)
		}
	}
}

// writeNew writes the executable (0755 before the umask) through a temporary file in the same directory, linked
// into place only when target still does not exist.
func writeNew(target string, data []byte) error {
	dir := filepath.Dir(target)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}
	suffix := make([]byte, 8)
	_, _ = rand.Read(suffix) // crypto/rand.Read never fails
	tmp := filepath.Join(dir, ".mise-"+hex.EncodeToString(suffix))
	if err := os.WriteFile(tmp, data, 0o755); err != nil {
		return fmt.Errorf("install mise: %w", err)
	}
	defer func() { _ = os.Remove(tmp) }()
	// a hard link fails when target exists, which a rename would replace; the rename is for file systems without links
	err := os.Link(tmp, target)
	switch {
	case err == nil:
		return nil
	case errors.Is(err, os.ErrExist):
		return fmt.Errorf("%s appeared during the download; left alone", target)
	}
	if _, err := os.Lstat(target); err == nil {
		return fmt.Errorf("%s appeared during the download; left alone", target)
	}
	if err := os.Rename(tmp, target); err != nil {
		return fmt.Errorf("install mise: %w", err)
	}
	return nil
}
