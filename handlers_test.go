package main

import (
	"archive/zip"
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// newTestServer builds a Server rooted at a fresh temp dir.
func newTestServer(t *testing.T) *Server {
	t.Helper()
	root := t.TempDir()
	return &Server{RootDir: root}
}

// doRequest routes a request through the real gin engine + dispatcher so the
// tests also exercise handle()'s method/action routing.
func (s *Server) doRequest(method, target string, body io.Reader) *httptest.ResponseRecorder {
	r := gin.New()
	r.Any("/*reqpath", s.handle)
	req := httptest.NewRequest(method, target, body)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// writeFile creates a file (and parents) under the server root.
func writeFile(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", p, err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", p, err)
	}
}

// zipEntries returns the sorted list of entry names in a zip archive.
func zipEntries(t *testing.T, path string) []string {
	t.Helper()
	zr, err := zip.OpenReader(path)
	if err != nil {
		t.Fatalf("open zip %s: %v", path, err)
	}
	defer zr.Close()
	var names []string
	for _, f := range zr.File {
		names = append(names, f.Name)
	}
	sort.Strings(names)
	return names
}

func TestHandleZipSingleFile(t *testing.T) {
	s := newTestServer(t)
	writeFile(t, s.RootDir, "hello.txt", "hi there")

	w := s.doRequest(http.MethodPost, "/hello.txt?action=zip", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", w.Code, w.Body.String())
	}

	out := filepath.Join(s.RootDir, "hello.txt.zip")
	if _, err := os.Stat(out); err != nil {
		t.Fatalf("expected archive %s: %v", out, err)
	}
	got := zipEntries(t, out)
	want := []string{"hello.txt"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("entries = %v, want %v", got, want)
	}
}

func TestHandleZipDirectory(t *testing.T) {
	s := newTestServer(t)
	writeFile(t, s.RootDir, "proj/a.txt", "a")
	writeFile(t, s.RootDir, "proj/sub/b.txt", "b")

	w := s.doRequest(http.MethodPost, "/proj?action=zip", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", w.Code, w.Body.String())
	}

	out := filepath.Join(s.RootDir, "proj.zip")
	got := zipEntries(t, out)
	want := []string{"proj/", "proj/a.txt", "proj/sub/", "proj/sub/b.txt"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("entries = %v, want %v", got, want)
	}
}

func TestHandleZipConflictSuffix(t *testing.T) {
	s := newTestServer(t)
	writeFile(t, s.RootDir, "data.txt", "x")
	// Pre-create the archive name that zip would otherwise use.
	writeFile(t, s.RootDir, "data.txt.zip", "placeholder")

	w := s.doRequest(http.MethodPost, "/data.txt?action=zip", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", w.Code, w.Body.String())
	}

	// Original archive untouched; a suffixed archive was created.
	if b, _ := os.ReadFile(filepath.Join(s.RootDir, "data.txt.zip")); string(b) != "placeholder" {
		t.Fatalf("existing data.txt.zip was overwritten")
	}
	entries, _ := os.ReadDir(s.RootDir)
	var suffixed int
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "data.txt.zip-") {
			suffixed++
		}
	}
	if suffixed != 1 {
		t.Fatalf("expected exactly one suffixed archive, found %d", suffixed)
	}
}

// makeZip writes a zip archive at root/rel containing the given entries.
func makeZip(t *testing.T, root, rel string, entries map[string]string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", p, err)
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	// Sort for deterministic order.
	var names []string
	for n := range entries {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		w, err := zw.Create(n)
		if err != nil {
			t.Fatalf("zip create %s: %v", n, err)
		}
		if _, err := w.Write([]byte(entries[n])); err != nil {
			t.Fatalf("zip write %s: %v", n, err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}
	if err := os.WriteFile(p, buf.Bytes(), 0o644); err != nil {
		t.Fatalf("write zip %s: %v", p, err)
	}
}

func TestHandleUnzip(t *testing.T) {
	s := newTestServer(t)
	makeZip(t, s.RootDir, "archive.zip", map[string]string{
		"top.txt":     "top-content",
		"sub/deep.txt": "deep-content",
	})

	w := s.doRequest(http.MethodPost, "/archive.zip?action=unzip&dest=out", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", w.Code, w.Body.String())
	}

	cases := map[string]string{
		"out/top.txt":      "top-content",
		"out/sub/deep.txt": "deep-content",
	}
	for rel, want := range cases {
		b, err := os.ReadFile(filepath.Join(s.RootDir, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatalf("read %s: %v", rel, err)
		}
		if string(b) != want {
			t.Fatalf("%s = %q, want %q", rel, string(b), want)
		}
	}
}

func TestHandleUnzipRejectsNonZip(t *testing.T) {
	s := newTestServer(t)
	writeFile(t, s.RootDir, "notes.txt", "plain")

	w := s.doRequest(http.MethodPost, "/notes.txt?action=unzip&dest=out", nil)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body: %s)", w.Code, w.Body.String())
	}
	if _, err := os.Stat(filepath.Join(s.RootDir, "out")); err == nil {
		t.Fatalf("dest folder should not have been created")
	}
}

func TestHandleUnzipZipSlip(t *testing.T) {
	s := newTestServer(t)
	makeZip(t, s.RootDir, "evil.zip", map[string]string{
		"../escaped.txt": "pwned",
	})

	w := s.doRequest(http.MethodPost, "/evil.zip?action=unzip&dest=out", nil)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body: %s)", w.Code, w.Body.String())
	}
	// Nothing should have escaped the destination into the root.
	if _, err := os.Stat(filepath.Join(s.RootDir, "escaped.txt")); err == nil {
		t.Fatalf("zip-slip entry escaped to root")
	}
}

func TestHandleUnzipConflictSuffix(t *testing.T) {
	s := newTestServer(t)
	makeZip(t, s.RootDir, "a.zip", map[string]string{"f.txt": "data"})
	// Pre-create the destination folder name.
	if err := os.MkdirAll(filepath.Join(s.RootDir, "out"), 0o755); err != nil {
		t.Fatalf("mkdir out: %v", err)
	}

	w := s.doRequest(http.MethodPost, "/a.zip?action=unzip&dest=out", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", w.Code, w.Body.String())
	}

	// Original "out" stays empty; a suffixed folder holds the extraction.
	if entries, _ := os.ReadDir(filepath.Join(s.RootDir, "out")); len(entries) != 0 {
		t.Fatalf("existing out/ was written into")
	}
	entries, _ := os.ReadDir(s.RootDir)
	var found bool
	for _, e := range entries {
		if e.IsDir() && strings.HasPrefix(e.Name(), "out-") {
			if _, err := os.Stat(filepath.Join(s.RootDir, e.Name(), "f.txt")); err == nil {
				found = true
			}
		}
	}
	if !found {
		t.Fatalf("expected a suffixed out- folder containing f.txt")
	}
}

func TestPostDispatcherDefaultCreatesDir(t *testing.T) {
	s := newTestServer(t)

	w := s.doRequest(http.MethodPost, "/newdir", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", w.Code, w.Body.String())
	}
	info, err := os.Stat(filepath.Join(s.RootDir, "newdir"))
	if err != nil {
		t.Fatalf("stat newdir: %v", err)
	}
	if !info.IsDir() {
		t.Fatalf("newdir is not a directory")
	}
}

func TestPostDispatcherRoutesActions(t *testing.T) {
	s := newTestServer(t)
	writeFile(t, s.RootDir, "f.txt", "content")
	makeZip(t, s.RootDir, "g.zip", map[string]string{"inner.txt": "z"})

	// action=zip reaches handleZip.
	if w := s.doRequest(http.MethodPost, "/f.txt?action=zip", nil); w.Code != http.StatusOK {
		t.Fatalf("zip status = %d, want 200 (body: %s)", w.Code, w.Body.String())
	}
	if _, err := os.Stat(filepath.Join(s.RootDir, "f.txt.zip")); err != nil {
		t.Fatalf("zip action did not produce archive: %v", err)
	}

	// action=unzip reaches handleUnzip.
	if w := s.doRequest(http.MethodPost, "/g.zip?action=unzip&dest=gout", nil); w.Code != http.StatusOK {
		t.Fatalf("unzip status = %d, want 200 (body: %s)", w.Code, w.Body.String())
	}
	if _, err := os.Stat(filepath.Join(s.RootDir, "gout", "inner.txt")); err != nil {
		t.Fatalf("unzip action did not extract: %v", err)
	}
}
