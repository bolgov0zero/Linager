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
	"sync"
	"testing"
	"time"
)

const (
	testOldID = "aaaaaaaaaaaa1111111111111111111111111111111111111111111111111111"
	testNewID = "bbbbbbbbbbbb2222222222222222222222222222222222222222222222222222"
)

// fakeDocker is a tiny Docker daemon on a unix socket that records the calls it gets.
type fakeDocker struct {
	mu           sync.Mutex
	calls        []string
	createBody   map[string]any
	imageID      string // what the pulled image resolves to
	failNewStart bool
	labels       map[string]any
	configImage  string

	// for the tests of editing a container
	mounts       []map[string]any // more entries of the inspect "Mounts"
	portBindings map[string]any   // HostConfig.PortBindings
	others       []map[string]any // the answer of /containers/json
	updateBody   map[string]any   // the body of the last "update" call
}

func (f *fakeDocker) called(prefix string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.calls {
		if strings.HasPrefix(c, prefix) {
			return true
		}
	}
	return false
}

func (f *fakeDocker) callIndex(prefix string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i, c := range f.calls {
		if strings.HasPrefix(c, prefix) {
			return i
		}
	}
	return -1
}

func startFakeDocker(t *testing.T, f *fakeDocker) {
	t.Helper()
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
	if f.configImage == "" {
		f.configImage = "nginx:latest"
	}
	srv := &http.Server{ReadHeaderTimeout: 10 * time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.calls = append(f.calls, r.Method+" "+r.URL.RequestURI())
		f.mu.Unlock()
		write := func(v any) { _ = json.NewEncoder(w).Encode(v) }
		p := r.URL.Path
		switch {
		case r.Method == "GET" && p == "/containers/web/json":
			hc := map[string]any{"NetworkMode": "mynet", "Binds": []string{"/data:/data"}}
			if f.portBindings != nil {
				hc["PortBindings"] = f.portBindings
			}
			mounts := append([]map[string]any{{"Type": "volume", "Name": strings.Repeat("c", 64)}}, f.mounts...)
			write(map[string]any{"Id": testOldID, "Name": "/web", "Image": "sha256:oldimage",
				"Config":     map[string]any{"Image": f.configImage, "Hostname": testOldID[:12], "Env": []string{"A=1"}, "Labels": f.labels},
				"HostConfig": hc,
				"State":      map[string]any{"Running": true},
				"NetworkSettings": map[string]any{"Networks": map[string]any{
					"mynet": map[string]any{"Aliases": []string{"web", testOldID[:12]}, "IPAMConfig": nil},
					"other": map[string]any{"Aliases": []string{"web"}}}},
				"Mounts": mounts})
		case r.Method == "GET" && p == "/containers/json":
			write(f.others)
		case r.Method == "POST" && p == "/containers/"+testOldID+"/update":
			f.mu.Lock()
			_ = json.NewDecoder(r.Body).Decode(&f.updateBody)
			f.mu.Unlock()
			write(map[string]any{"Warnings": []string{}})
		case r.Method == "GET" && p == "/containers/"+testNewID+"/json":
			write(map[string]any{"Id": testNewID, "State": map[string]any{"Running": true}})
		case r.Method == "POST" && p == "/images/create":
			write(map[string]string{"status": "Pulling from library/nginx"})
			write(map[string]string{"status": "Download complete", "id": "abc"})
		case r.Method == "GET" && strings.HasPrefix(p, "/images/") && strings.HasSuffix(p, "/json"):
			write(map[string]string{"Id": f.imageID})
		case r.Method == "POST" && p == "/containers/create":
			_ = json.NewDecoder(r.Body).Decode(&f.createBody)
			write(map[string]string{"Id": testNewID})
		case r.Method == "POST" && p == "/containers/"+testNewID+"/start" && f.failNewStart:
			w.WriteHeader(500)
			write(map[string]string{"message": "port is already allocated"})
		case r.Method == "GET" && strings.HasPrefix(p, "/containers/") && strings.HasSuffix(p, "/json"):
			w.WriteHeader(404) // a name nobody uses
			write(map[string]string{"message": "No such container"})
		default: // stop, rename, start, connect, delete
			w.WriteHeader(204)
		}
	})}
	go func() { _ = srv.Serve(l) }()
	t.Cleanup(func() { _ = srv.Close() })
	old := dockerSocket
	dockerSocket = sock
	t.Cleanup(func() { dockerSocket = old })
	oldCheck := stayCheck
	stayCheck = 10 * time.Millisecond
	t.Cleanup(func() { stayCheck = oldCheck })
}

func runUpdate(t *testing.T, params string) (string, error) {
	t.Helper()
	var mu sync.Mutex
	var out strings.Builder
	sio := &StreamIO{Out: func(b []byte) error {
		mu.Lock()
		out.Write(b)
		mu.Unlock()
		return nil
	}}
	err := dockerUpdate(context.Background(), json.RawMessage(params), sio)
	mu.Lock()
	defer mu.Unlock()
	return out.String(), err
}

func TestDockerUpdateAlreadyCurrent(t *testing.T) {
	f := &fakeDocker{imageID: "sha256:oldimage"}
	startFakeDocker(t, f)
	out, err := runUpdate(t, `{"container":"web"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "nothing to do") {
		t.Fatalf("output: %q", out)
	}
	if f.called("POST /containers/create") || f.called("POST /containers/"+testOldID+"/stop") {
		t.Fatalf("an up-to-date container must not be touched: %v", f.calls)
	}
}

func TestDockerUpdateRecreates(t *testing.T) {
	f := &fakeDocker{imageID: "sha256:newimage"}
	startFakeDocker(t, f)
	out, err := runUpdate(t, `{"container":"web"}`)
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	order := []string{"POST /images/create", "POST /containers/" + testOldID + "/stop", "POST /containers/" + testOldID + "/rename",
		"POST /containers/create?name=web", "POST /networks/other/connect", "POST /containers/" + testNewID + "/start", "DELETE /containers/" + testOldID}
	last := -1
	for _, c := range order {
		i := f.callIndex(c)
		if i < 0 || i < last {
			t.Fatalf("call %q missing or out of order (%d after %d): %v", c, i, last, f.calls)
		}
		last = i
	}
	b := f.createBody
	if b["Image"] != "nginx:latest" {
		t.Fatalf("image: %v", b["Image"])
	}
	if _, has := b["Hostname"]; has {
		t.Fatal("the hostname that was only the old container id must be dropped")
	}
	host := b["HostConfig"].(map[string]any)
	if binds := host["Binds"].([]any); len(binds) != 1 || binds[0] != "/data:/data" {
		t.Fatalf("binds must be kept: %v", host["Binds"])
	}
	if vf := host["VolumesFrom"].([]any); len(vf) != 1 || vf[0] != testOldID {
		t.Fatalf("anonymous volumes must follow: %v", host["VolumesFrom"])
	}
	ep := b["NetworkingConfig"].(map[string]any)["EndpointsConfig"].(map[string]any)
	al := ep["mynet"].(map[string]any)["Aliases"].([]any)
	if len(ep) != 1 || len(al) != 1 || al[0] != "web" {
		t.Fatalf("the main network keeps its aliases without the old id: %v", ep)
	}
	if !strings.Contains(out, "Done") {
		t.Fatalf("output: %q", out)
	}
}

func TestDockerUpdateRollsBack(t *testing.T) {
	f := &fakeDocker{imageID: "sha256:newimage", failNewStart: true}
	startFakeDocker(t, f)
	out, err := runUpdate(t, `{"container":"web"}`)
	if err == nil || !strings.Contains(err.Error(), "port is already allocated") {
		t.Fatalf("the start error must be reported: %v", err)
	}
	for _, c := range []string{"DELETE /containers/" + testNewID + "?force=1", "POST /containers/" + testOldID + "/start"} {
		if !f.called(c) {
			t.Fatalf("%q expected on rollback: %v", c, f.calls)
		}
	}
	// the old container got its name back
	back := false
	for _, c := range f.calls {
		if c == "POST /containers/"+testOldID+"/rename?name=web" {
			back = true
		}
	}
	if !back || !strings.Contains(out, "restored") {
		t.Fatalf("old container must be renamed back: %v\n%s", f.calls, out)
	}
}

func TestDockerUpdateRejectsImageID(t *testing.T) {
	f := &fakeDocker{imageID: "sha256:newimage", configImage: "3f2a9c1d7e6b"}
	startFakeDocker(t, f)
	if _, err := runUpdate(t, `{"container":"web"}`); err == nil || !strings.Contains(err.Error(), "image id") {
		t.Fatalf("an image id cannot be pulled: %v", err)
	}
	// but an image name entered by the administrator works
	if _, err := runUpdate(t, `{"container":"web","image":"nginx:1.27"}`); err != nil {
		t.Fatal(err)
	}
	if f.createBody["Image"] != "nginx:1.27" {
		t.Fatalf("override: %v", f.createBody["Image"])
	}
}

func TestDockerUpdateValidation(t *testing.T) {
	for _, p := range []string{`{"container":""}`, `{"container":"a b"}`, `{"container":"web","image":"bad image"}`} {
		if _, err := runUpdate(t, p); err == nil {
			t.Fatalf("%s must be refused", p)
		}
	}
}

func TestComposeUpdateRunsComposeCommands(t *testing.T) {
	dir := t.TempDir()
	logFile := filepath.Join(dir, "calls.log")
	script := "#!/bin/sh\necho \"$@\" >> " + logFile + "\necho output\nexit 0\n"
	if err := os.WriteFile(filepath.Join(dir, "docker"), []byte(script), 0o755); err != nil { //nolint:gosec // test helper script
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
	proj := t.TempDir()
	f := &fakeDocker{imageID: "sha256:newimage", labels: map[string]any{
		"com.docker.compose.project": "shop", "com.docker.compose.service": "web",
		"com.docker.compose.project.working_dir": proj, "com.docker.compose.project.config_files": proj + "/a.yml," + proj + "/b.yml"}}
	startFakeDocker(t, f)
	out, err := runUpdate(t, `{"container":"web"}`)
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	b, _ := os.ReadFile(logFile) //nolint:gosec // test path
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	base := "compose --project-name shop --project-directory " + proj + " -f " + proj + "/a.yml -f " + proj + "/b.yml"
	want := []string{"compose version", base + " pull web", base + " up -d --no-deps web"}
	if len(lines) != 3 || lines[0] != want[0] || lines[1] != want[1] || lines[2] != want[2] {
		t.Fatalf("compose calls:\n%s\nwant:\n%s", strings.Join(lines, "\n"), strings.Join(want, "\n"))
	}
	if f.called("POST /containers/create") {
		t.Fatal("a compose container must not be recreated through the API")
	}
}

func TestComposeUpdateFailureIsReported(t *testing.T) {
	dir := t.TempDir()
	script := "#!/bin/sh\nif [ \"$2\" = \"version\" ]; then exit 0; fi\necho 'pull access denied'\nexit 1\n"
	if err := os.WriteFile(filepath.Join(dir, "docker"), []byte(script), 0o755); err != nil { //nolint:gosec // test helper script
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
	f := &fakeDocker{imageID: "sha256:newimage", labels: map[string]any{"com.docker.compose.project": "shop", "com.docker.compose.service": "web"}}
	startFakeDocker(t, f)
	out, err := runUpdate(t, `{"container":"web"}`)
	if err == nil || !strings.Contains(out, "pull access denied") {
		t.Fatalf("a failing compose command must fail the update: %v\n%s", err, out)
	}
}
