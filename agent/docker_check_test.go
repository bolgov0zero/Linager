//go:build !windows

package agent

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestClassifyImage(t *testing.T) {
	remote := "sha256:" + strings.Repeat("a", 64)
	other := "sha256:" + strings.Repeat("b", 64)
	cur := classifyImage("sha256:img1", dImageInfo{ID: "sha256:img1", RepoDigests: []string{"nginx@" + remote}}, remote)
	if cur.status != "current" {
		t.Fatalf("same digest must be current: %+v", cur)
	}
	nw := classifyImage("sha256:img1", dImageInfo{ID: "sha256:img1", RepoDigests: []string{"nginx@" + other}}, remote)
	if nw.status != "new" || nw.local != strings.Repeat("b", 12) || nw.remote != strings.Repeat("a", 12) {
		t.Fatalf("another digest must be new: %+v", nw)
	}
	pulled := classifyImage("sha256:old", dImageInfo{ID: "sha256:img2", RepoDigests: []string{"nginx@" + remote}}, remote)
	if pulled.status != "new" {
		t.Fatalf("the tag moved on the host, the container runs the older image: %+v", pulled)
	}
	local := classifyImage("sha256:img1", dImageInfo{ID: "sha256:img1"}, remote)
	if local.status != "unknown" {
		t.Fatalf("a locally built image cannot be checked: %+v", local)
	}
}

func TestDockerCheck(t *testing.T) {
	remote := "sha256:" + strings.Repeat("a", 64)
	dir, err := os.MkdirTemp("", "dk")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	sock := filepath.Join(dir, "d.sock")
	l, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{ReadHeaderTimeout: 10 * time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		write := func(v any) { _ = json.NewEncoder(w).Encode(v) }
		switch p := r.URL.Path; {
		case p == "/containers/json":
			write([]map[string]any{
				{"Id": strings.Repeat("1", 64), "Names": []string{"/web"}, "Image": "nginx:latest", "ImageID": "sha256:img1"},
				{"Id": strings.Repeat("2", 64), "Names": []string{"/db"}, "Image": "postgres:16", "ImageID": "sha256:img2"},
				{"Id": strings.Repeat("3", 64), "Names": []string{"/priv"}, "Image": "registry.local/app:1", "ImageID": "sha256:img3"},
				{"Id": strings.Repeat("4", 64), "Names": []string{"/raw"}, "Image": "sha256:abcdef123456", "ImageID": "sha256:abcdef123456"},
			})
		case strings.HasPrefix(p, "/images/nginx:latest/json"):
			write(map[string]any{"Id": "sha256:img1", "RepoDigests": []string{"nginx@sha256:" + strings.Repeat("c", 64)}})
		case strings.HasPrefix(p, "/images/postgres:16/json"):
			write(map[string]any{"Id": "sha256:img2", "RepoDigests": []string{"postgres@" + remote}})
		case strings.HasPrefix(p, "/images/registry.local"):
			write(map[string]any{"Id": "sha256:img3", "RepoDigests": []string{"registry.local/app@" + remote}})
		case p == "/distribution/registry.local/app:1/json":
			w.WriteHeader(500)
			write(map[string]string{"message": "registry unreachable"})
		case strings.HasPrefix(p, "/distribution/"):
			write(map[string]any{"Descriptor": map[string]any{"digest": remote}})
		default:
			w.WriteHeader(404)
		}
	})}
	go func() { _ = srv.Serve(l) }()
	t.Cleanup(func() { _ = srv.Close() })
	old := dockerSocket
	dockerSocket = sock
	t.Cleanup(func() { dockerSocket = old })

	res, err := dockerCheck(context.Background(), nil, false)
	if err != nil {
		t.Fatal(err)
	}
	m := res.(map[string]any)
	if m["new"] != 1 || m["current"] != 1 || m["unknown"] != 2 {
		t.Fatalf("counts: %v", m)
	}
	if e, ok := checkFor(strings.Repeat("1", 64), "sha256:img1"); !ok || e.status != "new" {
		t.Fatalf("web must have a newer image: %+v %v", e, ok)
	}
	if e, _ := checkFor(strings.Repeat("3", 64), "sha256:img3"); e.status != "unknown" || !strings.Contains(e.err, "registry unreachable") {
		t.Fatalf("a registry error must be kept: %+v", e)
	}
	if _, ok := checkFor(strings.Repeat("1", 64), "sha256:changed"); ok {
		t.Fatal("a result of another image id must not be used")
	}
}

func TestUpdateManyValidates(t *testing.T) {
	sio := &StreamIO{Out: func([]byte) error { return nil }}
	if err := updateMany(context.Background(), nil, sio); err == nil {
		t.Fatal("an empty list must be rejected")
	}
	if err := updateMany(context.Background(), []string{"ok", "bad name"}, sio); err == nil {
		t.Fatal("an invalid name must be rejected before anything runs")
	}
}

func TestCountNewImages(t *testing.T) {
	if n := countNewImages(nil); n != 0 {
		t.Fatalf("no containers: %d", n)
	}
	n := countNewImages(map[string]checkEntry{
		"a": {status: "new"}, "b": {status: "new"}, "c": {status: "current"}, "d": {status: "unknown"},
	})
	if n != 2 {
		t.Fatalf("only the new images are counted: %d", n)
	}
	if imageCheckEvery != 6*time.Hour {
		t.Fatalf("the check interval: %v", imageCheckEvery)
	}
}

func TestImageUpdatesUnknownUntilChecked(t *testing.T) {
	dockerChecks.mu.Lock()
	saved := dockerChecks.at
	dockerChecks.at, dockerChecks.updates = 0, 5
	dockerChecks.mu.Unlock()
	t.Cleanup(func() {
		dockerChecks.mu.Lock()
		dockerChecks.at, dockerChecks.updates = saved, 0
		dockerChecks.mu.Unlock()
	})
	if n := imageUpdates(); n != -1 {
		t.Fatalf("before the first check the value is unknown: %d", n)
	}
	dockerChecks.mu.Lock()
	dockerChecks.at = 100
	dockerChecks.mu.Unlock()
	syncImageUpdates(map[string]checkEntry{"a": {status: "new"}, "b": {status: "current"}})
	if n := imageUpdates(); n != 1 {
		t.Fatalf("after the check: %d", n)
	}
}
