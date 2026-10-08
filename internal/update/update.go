// Package update replaces the running ctxed binary with the latest published
// release: it reads the GitHub releases API, verifies the download against the
// release's checksums.txt, and swaps the binary atomically.
package update

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Repo is the GitHub repository ctxed releases are published to.
const Repo = "simranjeetc/ctxed"

// Endpoint defaults. They are fields on Client so tests can point elsewhere.
const (
	defaultAPIBase      = "https://api.github.com"
	defaultDownloadBase = "https://github.com"
)

// Client resolves and downloads releases.
type Client struct {
	Repo         string
	APIBase      string
	DownloadBase string
	HTTP         *http.Client
}

func (c *Client) repo() string {
	if c.Repo != "" {
		return c.Repo
	}
	return Repo
}

func (c *Client) api() string {
	if c.APIBase != "" {
		return strings.TrimRight(c.APIBase, "/")
	}
	return defaultAPIBase
}

func (c *Client) download() string {
	if c.DownloadBase != "" {
		return strings.TrimRight(c.DownloadBase, "/")
	}
	return defaultDownloadBase
}

func (c *Client) http() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 60 * time.Second}
}

// Release is the subset of a GitHub release ctxed needs.
type Release struct {
	Tag    string
	Assets map[string]string // asset name -> download URL
}

// Latest returns the latest non-draft, non-prerelease release.
func (c *Client) Latest(ctx context.Context) (Release, error) {
	url := c.api() + "/repos/" + c.repo() + "/releases/latest"
	body, err := c.get(ctx, url)
	if err != nil {
		return Release{}, err
	}
	var parsed struct {
		TagName string `json:"tag_name"`
		Assets  []struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return Release{}, fmt.Errorf("update: decode release: %w", err)
	}
	if parsed.TagName == "" {
		return Release{}, fmt.Errorf("update: %s has no published releases", c.repo())
	}
	rel := Release{Tag: parsed.TagName, Assets: map[string]string{}}
	for _, a := range parsed.Assets {
		rel.Assets[a.Name] = a.URL
	}
	return rel, nil
}

// Result describes what Update did.
type Result struct {
	From    string
	To      string
	Changed bool
}

// Update replaces targetPath with the latest release when it is newer than
// current (or force is set). It verifies the archive against checksums.txt
// before touching the file.
func (c *Client) Update(ctx context.Context, current, goos, goarch, targetPath string, force bool) (Result, error) {
	rel, err := c.Latest(ctx)
	if err != nil {
		return Result{}, err
	}
	res := Result{From: current, To: strings.TrimPrefix(rel.Tag, "v")}
	if !force && Compare(current, rel.Tag) >= 0 {
		return res, nil
	}

	asset := AssetName(rel.Tag, goos, goarch)
	archiveURL, ok := rel.Assets[asset]
	if !ok {
		return res, fmt.Errorf("update: release %s has no asset %s", rel.Tag, asset)
	}
	checksumsURL, ok := rel.Assets["checksums.txt"]
	if !ok {
		return res, fmt.Errorf("update: release %s has no checksums.txt", rel.Tag)
	}

	archive, err := c.get(ctx, archiveURL)
	if err != nil {
		return res, err
	}
	rawSums, err := c.get(ctx, checksumsURL)
	if err != nil {
		return res, err
	}
	sums, err := ParseChecksums(bytes.NewReader(rawSums))
	if err != nil {
		return res, err
	}
	want, ok := sums[asset]
	if !ok {
		return res, fmt.Errorf("update: checksums.txt has no entry for %s", asset)
	}
	if got := Checksum(archive); got != want {
		return res, fmt.Errorf("update: checksum mismatch for %s (want %s, got %s)", asset, want, got)
	}

	bin, err := ExtractBinary(archive)
	if err != nil {
		return res, err
	}
	if err := Replace(targetPath, bin); err != nil {
		return res, err
	}
	res.Changed = true
	return res, nil
}

// get fetches url, retrying transient failures up to three times.
func (c *Client) get(ctx context.Context, url string) ([]byte, error) {
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(time.Duration(attempt) * time.Second):
			}
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", "ctxed-updater")
		resp, err := c.http().Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
		_ = resp.Body.Close()
		if readErr != nil {
			lastErr = readErr
			continue
		}
		if resp.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("update: GET %s: %s", url, resp.Status)
			if resp.StatusCode < 500 {
				break // a 4xx will not fix itself
			}
			continue
		}
		return body, nil
	}
	return nil, lastErr
}

// AssetName is the release archive name for a version and platform, matching
// the goreleaser name_template in .goreleaser.yaml.
func AssetName(version, goos, goarch string) string {
	return fmt.Sprintf("ctxed_%s_%s_%s.tar.gz", strings.TrimPrefix(version, "v"), goos, goarch)
}

// Checksum returns the lowercase hex sha256 of data.
func Checksum(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// ParseChecksums reads the "sha256  filename" lines of a checksums.txt. Lines
// whose first field is not a 64-character hex digest are ignored.
func ParseChecksums(r io.Reader) (map[string]string, error) {
	sums := map[string]string{}
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) != 2 || !isSHA256(fields[0]) {
			continue
		}
		sums[fields[1]] = strings.ToLower(fields[0])
	}
	return sums, sc.Err()
}

func isSHA256(s string) bool {
	if len(s) != 64 {
		return false
	}
	for _, c := range s {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') && (c < 'A' || c > 'F') {
			return false
		}
	}
	return true
}

// ExtractBinary returns the ctxed binary from a gzip-compressed tar archive.
func ExtractBinary(archive []byte) ([]byte, error) {
	zr, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, fmt.Errorf("update: open archive: %w", err)
	}
	defer func() { _ = zr.Close() }()
	tr := tar.NewReader(zr)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("update: read archive: %w", err)
		}
		if h.Typeflag != tar.TypeReg || filepath.Base(h.Name) != "ctxed" {
			continue
		}
		data, err := io.ReadAll(io.LimitReader(tr, 64<<20))
		if err != nil {
			return nil, fmt.Errorf("update: read binary: %w", err)
		}
		return data, nil
	}
	return nil, fmt.Errorf("update: archive has no ctxed binary")
}

// Replace atomically writes data over path, keeping it executable.
func Replace(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".ctxed-update-*")
	if err != nil {
		return fmt.Errorf("update: %w", err)
	}
	name := tmp.Name()
	defer func() { _ = os.Remove(name) }()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o755); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(name, path); err != nil {
		return fmt.Errorf("update: replace %s: %w", path, err)
	}
	return nil
}

type version struct {
	num [3]int
	pre string
}

// Compare returns -1, 0, or 1 comparing two semantic versions, ignoring a
// leading "v" and any build metadata. A "dev" or unparseable version sorts
// lowest.
func Compare(a, b string) int {
	pa, oka := parseVersion(a)
	pb, okb := parseVersion(b)
	switch {
	case !oka && !okb:
		return 0
	case !oka:
		return -1
	case !okb:
		return 1
	}
	for i := range pa.num {
		if pa.num[i] != pb.num[i] {
			if pa.num[i] < pb.num[i] {
				return -1
			}
			return 1
		}
	}
	switch {
	case pa.pre == "" && pb.pre == "":
		return 0
	case pa.pre == "":
		return 1 // a release outranks its pre-releases
	case pb.pre == "":
		return -1
	}
	return strings.Compare(pa.pre, pb.pre)
}

func parseVersion(s string) (version, bool) {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "v")
	if s == "" || s == "dev" || s == "(devel)" {
		return version{}, false
	}
	if i := strings.IndexByte(s, '+'); i >= 0 {
		s = s[:i]
	}
	var v version
	if i := strings.IndexByte(s, '-'); i >= 0 {
		v.pre = s[i+1:]
		s = s[:i]
	}
	parts := strings.Split(s, ".")
	for i := 0; i < len(parts) && i < len(v.num); i++ {
		n, err := strconv.Atoi(parts[i])
		if err != nil {
			return version{}, false
		}
		v.num[i] = n
	}
	if len(parts) == 0 {
		return version{}, false
	}
	return v, true
}
