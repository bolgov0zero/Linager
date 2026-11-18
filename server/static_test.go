package server

import (
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
