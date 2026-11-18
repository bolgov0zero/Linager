package server

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// The settings of a container wait for their stream under a one-time token of the same server; the stream
// URL carries the token only (the variables may hold passwords).
func TestDockerEditToken(t *testing.T) {
	a := &Admin{}
	spec := dockerEditSpec{Name: "web", Image: "nginx:1"}
	spec.Env = append(spec.Env, struct {
		K string `json:"k"`
		V string `json:"v"`
	}{"PASSWORD", "s3cret"})
	tok := a.editJobs.put(&dockerEditJob{server: "srv1", container: "web", spec: spec})
	if len(tok) != 32 {
		t.Fatalf("token %q", tok)
	}
	stream := func(server, token string) (string, map[string]any, bool) {
		r := httptest.NewRequest("GET", "/api/servers/"+server+"/stream?kind=docker-edit&token="+token, nil)
		r.SetPathValue("id", server)
		return a.streamParams(r)
	}
	if _, _, ok := stream("srv2", tok); ok {
		t.Fatal("another server must not get the settings")
	}
	mod, p, ok := stream("srv1", tok)
	if !ok || mod != "docker.edit" || p["container"] != "web" {
		t.Fatalf("stream: %s %v %v", mod, p, ok)
	}
	if got, _ := p["spec"].(dockerEditSpec); len(got.Env) != 1 || got.Env[0].V != "s3cret" {
		t.Errorf("the agent must get the settings: %v", p["spec"])
	}
	if _, _, ok := stream("srv1", tok); ok {
		t.Fatal("a token works once")
	}
	if _, _, ok := stream("srv1", strings.Repeat("0", 32)); ok {
		t.Fatal("an unknown token")
	}
	old := a.editJobs.put(&dockerEditJob{server: "srv1", container: "web"})
	a.editJobs.mu.Lock()
	a.editJobs.jobs[old].exp = time.Now().Add(-time.Second)
	a.editJobs.mu.Unlock()
	if a.editJobs.take("srv1", old) != nil {
		t.Fatal("an expired token")
	}
}
