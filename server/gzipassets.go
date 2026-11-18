package server

import (
	"bytes"
	"compress/gzip"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strings"
	"sync"
)

// gzipAssets sends the scripts and styles of the web app compressed, when the browser accepts it. The remote desktop
// client is a 6 MB script (2 MB compressed); the files are fixed for the life of the process, so each one is
// compressed once and kept.
type gzipAssets struct {
	fsys  fs.FS
	mu    sync.Mutex
	cache map[string][]byte
}

// serve answers a request for an /assets/ file and reports whether it did.
func (g *gzipAssets) serve(w http.ResponseWriter, r *http.Request) bool {
	p := strings.TrimPrefix(r.URL.Path, "/")
	ext := path.Ext(p)
	if r.Method != http.MethodGet || !strings.HasPrefix(p, "assets/") || (ext != ".js" && ext != ".css") || !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
		return false
	}
	g.mu.Lock()
	b, ok := g.cache[p]
	g.mu.Unlock()
	if !ok {
		raw, err := fs.ReadFile(g.fsys, p)
		if err != nil {
			return false
		}
		var buf bytes.Buffer
		zw, _ := gzip.NewWriterLevel(&buf, gzip.BestCompression)
		_, _ = zw.Write(raw)
		_ = zw.Close()
		b = buf.Bytes()
		g.mu.Lock()
		if g.cache == nil {
			g.cache = map[string][]byte{}
		}
		g.cache[p] = b
		g.mu.Unlock()
	}
	w.Header().Set("Content-Type", mime.TypeByExtension(ext))
	w.Header().Set("Content-Encoding", "gzip")
	w.Header().Set("Vary", "Accept-Encoding")
	// the file names carry a hash of their content: a changed file has a new name
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	_, _ = w.Write(b)
	return true
}
