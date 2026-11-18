package server

import (
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func TestStaticServesHomeScreenAssets(t *testing.T) {
	a := &Admin{Static: fstest.MapFS{
		"index.html":           {Data: []byte("<html></html>")},
		"manifest.webmanifest": {Data: []byte(`{"name":"Linager"}`)},
		"apple-touch-icon.png": {Data: []byte("png")},
	}}
	h := a.static()

	cases := []struct{ path, wantType string }{
		{"/manifest.webmanifest", "application/manifest+json"},
		{"/apple-touch-icon.png", "image/png"},
		{"/srv-unknown", "text/html"}, // SPA fallback
	}
	for _, c := range cases {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, c.path, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status %d", c.path, rec.Code)
		}
		if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, c.wantType) {
			t.Errorf("%s: content type %q, want %q", c.path, got, c.wantType)
		}
	}
}

func TestStaticMissingAssetIsNotThePage(t *testing.T) {
	a := &Admin{Static: fstest.MapFS{"index.html": {Data: []byte("<html></html>")}, "assets/new.js": {Data: []byte("export {}")}}}
	h := a.static()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/assets/old-123.js", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("a missing asset: status %d, want 404", rec.Code)
	}
	for _, path := range []string{"/", "/srv-unknown"} {
		rec = httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusOK || rec.Header().Get("Cache-Control") != "no-cache" {
			t.Errorf("%s: status %d, cache %q", path, rec.Code, rec.Header().Get("Cache-Control"))
		}
	}
}

func TestStaticAssetsAreSentCompressed(t *testing.T) {
	body := strings.Repeat("export const x = 1;\n", 500)
	a := &Admin{Static: fstest.MapFS{"index.html": {Data: []byte("<html></html>")}, "assets/app-abc.js": {Data: []byte(body)}, "assets/app-abc.css": {Data: []byte("a{}")}}}
	h := a.static()
	req := httptest.NewRequest(http.MethodGet, "/assets/app-abc.js", nil)
	req.Header.Set("Accept-Encoding", "gzip, br")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Header().Get("Content-Encoding") != "gzip" || rec.Body.Len() >= len(body) {
		t.Fatalf("not compressed: %q, %d bytes", rec.Header().Get("Content-Encoding"), rec.Body.Len())
	}
	zr, err := gzip.NewReader(rec.Body)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(zr)
	if string(got) != body {
		t.Fatal("the compressed answer must hold the same file")
	}
	if !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/javascript") && !strings.HasPrefix(rec.Header().Get("Content-Type"), "application/javascript") {
		t.Fatalf("content type %q", rec.Header().Get("Content-Type"))
	}
	// a browser that does not accept gzip gets the file as it is
	plain := httptest.NewRecorder()
	h.ServeHTTP(plain, httptest.NewRequest(http.MethodGet, "/assets/app-abc.js", nil))
	if plain.Header().Get("Content-Encoding") != "" || plain.Body.String() != body {
		t.Fatal("without Accept-Encoding the file must come unchanged")
	}
}
