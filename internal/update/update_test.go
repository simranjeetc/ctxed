package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCompare(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"0.1.0", "0.1.0", 0},
		{"v0.1.0", "0.1.0", 0},
		{"0.1.0", "0.2.0", -1},
		{"0.2.0", "0.1.9", 1},
		{"1.0.0", "0.9.9", 1},
		{"dev", "0.1.0", -1},
		{"0.1.0", "dev", 1},
		{"dev", "dev", 0},
		{"0.1.0-rc1", "0.1.0", -1},
		{"0.1.0", "0.1.0-rc1", 1},
		{"v0.1.0+build", "0.1.0", 0},
	}
	for _, c := range cases {
		if got := Compare(c.a, c.b); got != c.want {
			t.Errorf("Compare(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestAssetName(t *testing.T) {
	cases := []struct {
		version, goos, goarch, want string
	}{
		{"v0.1.0", "darwin", "arm64", "ctxed_0.1.0_darwin_arm64.tar.gz"},
		{"0.1.0", "linux", "amd64", "ctxed_0.1.0_linux_amd64.tar.gz"},
	}
	for _, c := range cases {
		if got := AssetName(c.version, c.goos, c.goarch); got != c.want {
			t.Errorf("AssetName(%q, %q, %q) = %q, want %q", c.version, c.goos, c.goarch, got, c.want)
		}
	}
}

func TestParseChecksums(t *testing.T) {
	a := strings.Repeat("ab", 32) // 64 hex chars
	b := strings.Repeat("cd", 32)
	in := a + "  a.tar.gz\n\n" + b + "  b.tar.gz\nmalformed line\n" + strings.Repeat("x", 64) + "  c.tar.gz\n"
	sums, err := ParseChecksums(bytes.NewReader([]byte(in)))
	if err != nil {
		t.Fatal(err)
	}
	if sums["a.tar.gz"] != a || sums["b.tar.gz"] != b {
		t.Fatalf("unexpected sums: %v", sums)
	}
	if len(sums) != 2 {
		t.Fatalf("want 2 entries, got %d", len(sums))
	}
}

func TestExtractFile(t *testing.T) {
	archive := tarGz(t, map[string]string{
		"LICENSE": "mit",
		"ctxed":   "BINARY",
	})
	got, err := ExtractFile(archive, "ctxed")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "BINARY" {
		t.Fatalf("got %q, want BINARY", got)
	}
}

func TestExtractFileMissing(t *testing.T) {
	if _, err := ExtractFile(tarGz(t, map[string]string{"README.md": "x"}), "ctxed"); err == nil {
		t.Fatal("want error when the archive has no matching file")
	}
}

func TestReplace(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "ctxed")
	if err := os.WriteFile(target, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Replace(target, []byte("new")); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "new" {
		t.Fatalf("got %q, want new", got)
	}
	info, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o755 {
		t.Fatalf("mode = %o, want 755", info.Mode().Perm())
	}
}

func TestUpdateReplacesBinary(t *testing.T) {
	archive := tarGz(t, map[string]string{
		"ctxed":                          "NEW-BINARY",
		"skills/ctxed-overview/SKILL.md": "SKILL-BODY",
	})
	asset := "ctxed_9.9.9_linux_amd64.tar.gz"

	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/simranjeetc/ctxed/releases/latest":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"tag_name": "v9.9.9",
				"assets": []map[string]string{
					{"name": asset, "browser_download_url": srv.URL + "/dl/archive"},
					{"name": "checksums.txt", "browser_download_url": srv.URL + "/dl/checksums"},
				},
			})
		case "/dl/archive":
			_, _ = w.Write(archive)
		case "/dl/checksums":
			_, _ = w.Write([]byte(Checksum(archive) + "  " + asset + "\n"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	target := filepath.Join(t.TempDir(), "ctxed")
	if err := os.WriteFile(target, []byte("OLD"), 0o755); err != nil {
		t.Fatal(err)
	}

	c := &Client{APIBase: srv.URL}
	res, err := c.Update(context.Background(), "0.1.0", "linux", "amd64", target, false)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.To != "9.9.9" {
		t.Fatalf("unexpected result: %+v", res)
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "NEW-BINARY" {
		t.Fatalf("got %q, want NEW-BINARY", got)
	}
	if string(res.Skill) != "SKILL-BODY" {
		t.Fatalf("skill = %q, want SKILL-BODY", res.Skill)
	}
}

func TestUpdateAlreadyLatest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/repos/simranjeetc/ctxed/releases/latest" {
			_ = json.NewEncoder(w).Encode(map[string]any{"tag_name": "v0.1.0", "assets": []any{}})
			return
		}
		http.NotFound(w, r) // any download attempt is a test failure
	}))
	defer srv.Close()

	target := filepath.Join(t.TempDir(), "ctxed")
	if err := os.WriteFile(target, []byte("KEEP"), 0o755); err != nil {
		t.Fatal(err)
	}
	c := &Client{APIBase: srv.URL}
	res, err := c.Update(context.Background(), "0.1.0", "linux", "amd64", target, false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatalf("want no change, got %+v", res)
	}
	if got, _ := os.ReadFile(target); string(got) != "KEEP" {
		t.Fatalf("binary was modified: %q", got)
	}
}

func TestUpdateChecksumMismatch(t *testing.T) {
	archive := tarGz(t, map[string]string{"ctxed": "NEW"})
	asset := "ctxed_9.9.9_linux_amd64.tar.gz"

	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/simranjeetc/ctxed/releases/latest":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"tag_name": "v9.9.9",
				"assets": []map[string]string{
					{"name": asset, "browser_download_url": srv.URL + "/dl/archive"},
					{"name": "checksums.txt", "browser_download_url": srv.URL + "/dl/checksums"},
				},
			})
		case "/dl/archive":
			_, _ = w.Write(archive)
		case "/dl/checksums":
			_, _ = w.Write([]byte("0000000000000000000000000000000000000000000000000000000000000000  " + asset + "\n"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	target := filepath.Join(t.TempDir(), "ctxed")
	if err := os.WriteFile(target, []byte("OLD"), 0o755); err != nil {
		t.Fatal(err)
	}
	c := &Client{APIBase: srv.URL}
	if _, err := c.Update(context.Background(), "0.1.0", "linux", "amd64", target, false); err == nil {
		t.Fatal("want checksum mismatch error")
	}
	if got, _ := os.ReadFile(target); string(got) != "OLD" {
		t.Fatalf("binary was modified despite mismatch: %q", got)
	}
}

func tarGz(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(zw)
	for name, content := range files {
		hdr := &tar.Header{Name: name, Mode: 0o755, Size: int64(len(content)), Typeflag: tar.TypeReg}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}
