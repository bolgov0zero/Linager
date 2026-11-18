//go:build !windows

package agent

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"sync"
	"testing"
)

func TestSplitJoinCmd(t *testing.T) {
	cases := map[string][]string{
		`redis-server --save "" --appendonly no`: {"redis-server", "--save", "", "--appendonly", "no"},
		`sh -c 'echo "a b"; exit 0'`:             {"sh", "-c", `echo "a b"; exit 0`},
		`a\ b "c \"d\""`:                         {"a b", `c "d"`},
		``:                                       nil,
	}
	for in, want := range cases {
		got, err := splitCmd(in)
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Errorf("splitCmd(%q) = %q, %v; want %q", in, got, err, want)
		}
		if len(want) > 0 {
			back, err := splitCmd(joinCmd(want))
			if err != nil || !reflect.DeepEqual(back, want) {
				t.Errorf("join then split of %q gives %q, %v", want, back, err)
			}
		}
	}
	if _, err := splitCmd(`echo "open`); err == nil {
		t.Error("an unclosed quote must be refused")
	}
}

func baseSpec() editSpec {
	return normalizeSpec(editSpec{Name: "web", Image: "nginx:1.25", Restart: "unless-stopped", MemMB: 256,
		Ports:  []editPort{{IP: "0.0.0.0", Host: 80, Cont: 80, Proto: "tcp"}},
		Mounts: []editMount{{Type: "bind", Src: "/srv/conf", Dst: "/etc/nginx/conf.d", RO: true}},
		Env:    []editEnv{{K: "TZ", V: "UTC"}}})
}

func TestValidateSpec(t *testing.T) {
	if err := validateSpec(baseSpec()); err != nil {
		t.Fatalf("a good spec is refused: %v", err)
	}
	bad := map[string]func(*editSpec){
		"name":        func(s *editSpec) { s.Name = "-x" },
		"image":       func(s *editSpec) { s.Image = "a b" },
		"restart":     func(s *editSpec) { s.Restart = "sometimes" },
		"memory":      func(s *editSpec) { s.MemMB = 3 },
		"port":        func(s *editSpec) { s.Ports[0].Host = 70000 },
		"port twice":  func(s *editSpec) { s.Ports = append(s.Ports, s.Ports[0]) },
		"address":     func(s *editSpec) { s.Ports[0].IP = "nope" },
		"relative":    func(s *editSpec) { s.Mounts[0].Src = "srv/conf" },
		"colon":       func(s *editSpec) { s.Mounts[0].Src = "/srv:/etc:rw" },
		"dst twice":   func(s *editSpec) { s.Mounts = append(s.Mounts, s.Mounts[0]) },
		"volume name": func(s *editSpec) { s.Mounts = append(s.Mounts, editMount{Type: "volume", Src: "../x", Dst: "/x"}) },
		"env name":    func(s *editSpec) { s.Env[0].K = "1BAD" },
		"env twice":   func(s *editSpec) { s.Env = append(s.Env, s.Env[0]) },
		"cmd quote":   func(s *editSpec) { s.Cmd = `echo "x` },
	}
	for name, mod := range bad {
		s := baseSpec()
		mod(&s)
		if validateSpec(s) == nil {
			t.Errorf("%s: the bad spec is accepted", name)
		}
	}
}

func TestPlanEdit(t *testing.T) {
	cur := baseSpec()
	with := func(f func(*editSpec)) editSpec {
		s := baseSpec()
		f(&s)
		return s
	}
	cases := []struct {
		name     string
		want     editSpec
		recreate bool
		changed  string
	}{
		{"nothing", cur, false, ""},
		{"restart", with(func(s *editSpec) { s.Restart = "always" }), false, "restart"},
		{"raise memory", with(func(s *editSpec) { s.MemMB = 512 }), false, "memory"},
		{"rename", with(func(s *editSpec) { s.Name = "web2" }), false, "name"},
		{"remove the memory limit", with(func(s *editSpec) { s.MemMB = 0 }), true, "memory"},
		{"env", with(func(s *editSpec) { s.Env[0].V = "UTC+3" }), true, "env"},
		{"image", with(func(s *editSpec) { s.Image = "nginx:1.26" }), true, "image"},
		{"command", with(func(s *editSpec) { s.Cmd = "nginx -g 'daemon off;'" }), true, "cmd"},
		{"equal command written differently", with(func(s *editSpec) { s.Cmd = "  " }), false, ""},
	}
	for _, c := range cases {
		p, err := planEdit(cur, c.want, "")
		if err != nil || p.recreate != c.recreate || strings.Join(p.changed, ",") != c.changed {
			t.Errorf("%s: got %v recreate=%v err=%v", c.name, p.changed, p.recreate, err)
		}
	}
	// a compose container: only what is applied in place
	if _, err := planEdit(cur, with(func(s *editSpec) { s.Env[0].V = "x" }), "shop"); err == nil {
		t.Error("compose: a change of a variable must be refused")
	}
	if _, err := planEdit(cur, with(func(s *editSpec) { s.Name = "x" }), "shop"); err == nil {
		t.Error("compose: a rename must be refused")
	}
	if _, err := planEdit(cur, with(func(s *editSpec) { s.Restart = "always"; s.MemMB = 512 }), "shop"); err != nil {
		t.Errorf("compose: the restart policy and the limits are allowed: %v", err)
	}
}

func runEdit(t *testing.T, spec editSpec) (string, error) {
	t.Helper()
	raw, _ := json.Marshal(map[string]any{"container": "web", "spec": spec})
	var mu sync.Mutex
	var out strings.Builder
	sio := &StreamIO{Out: func(b []byte) error {
		mu.Lock()
		out.Write(b)
		mu.Unlock()
		return nil
	}}
	err := dockerEdit(context.Background(), raw, sio)
	mu.Lock()
	defer mu.Unlock()
	return out.String(), err
}

// the container of the fake daemon as the editor sees it
func fakeSpec(t *testing.T, f *fakeDocker) editSpec {
	t.Helper()
	cfg, err := dockerConfig(context.Background(), json.RawMessage(`{"id":"web"}`), false)
	if err != nil {
		t.Fatal(err)
	}
	return normalizeSpec(cfg.(editConfig).editSpec)
}

func TestDockerConfigReadsSettings(t *testing.T) {
	f := &fakeDocker{
		portBindings: map[string]any{
			"80/tcp":   []any{map[string]any{"HostIp": "", "HostPort": "8080"}},
			"9000/tcp": []any{map[string]any{"HostIp": "127.0.0.1", "HostPort": "9000"}},
			"53/udp":   []any{map[string]any{"HostIp": "", "HostPort": ""}}, // a random host port: not shown
		},
		mounts: []map[string]any{
			{"Type": "bind", "Source": "/srv/conf", "Destination": "/etc/conf", "Mode": "ro", "RW": false},
			{"Type": "volume", "Name": "data", "Destination": "/var/data", "Mode": "z", "RW": true},
		}}
	startFakeDocker(t, f)
	s := fakeSpec(t, f)
	wantPorts := []editPort{{"0.0.0.0", 8080, 80, "tcp"}, {"127.0.0.1", 9000, 9000, "tcp"}}
	if !reflect.DeepEqual(s.Ports, wantPorts) {
		t.Errorf("ports: %+v", s.Ports)
	}
	wantMounts := []editMount{{"bind", "/srv/conf", "/etc/conf", true}, {"volume", "data", "/var/data", false}}
	if !reflect.DeepEqual(s.Mounts, wantMounts) {
		t.Errorf("mounts (an anonymous volume is not shown): %+v", s.Mounts)
	}
	if !reflect.DeepEqual(s.Env, []editEnv{{"A", "1"}}) || s.Name != "web" || s.Restart != "no" {
		t.Errorf("spec: %+v", s)
	}
}

func TestDockerEditInPlace(t *testing.T) {
	f := &fakeDocker{}
	startFakeDocker(t, f)
	want := fakeSpec(t, f)
	want.Restart, want.Retries, want.MemMB, want.Name = "on-failure", 5, 128, "web2"
	out, err := runEdit(t, want)
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if f.called("POST /containers/create") || f.called("POST /containers/"+testOldID+"/stop") {
		t.Fatalf("a change in place must not recreate: %v", f.calls)
	}
	rp, _ := f.updateBody["RestartPolicy"].(map[string]any)
	if rp["Name"] != "on-failure" || rp["MaximumRetryCount"] != float64(5) || f.updateBody["Memory"] != float64(128<<20) || f.updateBody["MemorySwap"] != float64(256<<20) {
		t.Errorf("update body: %v", f.updateBody)
	}
	if !f.called("POST /containers/" + testOldID + "/rename?name=web2") {
		t.Errorf("no rename: %v", f.calls)
	}
}

func TestDockerEditRecreates(t *testing.T) {
	f := &fakeDocker{mounts: []map[string]any{{"Type": "bind", "Source": "/srv/conf", "Destination": "/etc/conf", "Mode": "ro,z", "RW": false}}}
	startFakeDocker(t, f)
	want := fakeSpec(t, f)
	want.Env = []editEnv{{"A", "2"}, {"NEW", "x"}}
	want.Ports = []editPort{{"127.0.0.1", 8081, 80, "tcp"}}
	want.Mounts[0].RO = false
	want.Mounts = append(want.Mounts, editMount{"volume", "logs", "/var/log", false})
	out, err := runEdit(t, want)
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if f.called("POST /images/create") {
		t.Error("the image did not change, nothing to pull")
	}
	stop, ren := f.callIndex("POST /containers/"+testOldID+"/stop"), f.callIndex("POST /containers/"+testOldID+"/rename")
	create, del := f.callIndex("POST /containers/create?name=web"), f.callIndex("DELETE /containers/"+testOldID)
	if stop < 0 || stop > ren || ren > create || create > del {
		t.Errorf("wrong order: %v", f.calls)
	}
	body := f.createBody
	if !reflect.DeepEqual(body["Env"], []any{"A=2", "NEW=x"}) {
		t.Errorf("env: %v", body["Env"])
	}
	host, _ := body["HostConfig"].(map[string]any)
	pb, _ := host["PortBindings"].(map[string]any)
	if !reflect.DeepEqual(pb["80/tcp"], []any{map[string]any{"HostIp": "127.0.0.1", "HostPort": "8081"}}) {
		t.Errorf("port bindings: %v", pb)
	}
	if !reflect.DeepEqual(host["Binds"], []any{"/srv/conf:/etc/conf:z", "logs:/var/log"}) {
		t.Errorf("binds (the z option stays, ro is gone): %v", host["Binds"])
	}
	if _, ok := body["ExposedPorts"].(map[string]any)["80/tcp"]; !ok {
		t.Errorf("exposed ports: %v", body["ExposedPorts"])
	}
}

func TestDockerEditPortInUse(t *testing.T) {
	f := &fakeDocker{others: []map[string]any{{"Id": strings.Repeat("d", 64), "Names": []string{"/db"},
		"Ports": []map[string]any{{"IP": "0.0.0.0", "PublicPort": 8081, "Type": "tcp"}}}}}
	startFakeDocker(t, f)
	want := fakeSpec(t, f)
	want.Ports = []editPort{{"127.0.0.1", 8081, 80, "tcp"}}
	out, err := runEdit(t, want)
	if err == nil || !strings.Contains(err.Error(), "db") {
		t.Fatalf("a port of another container must be refused: %v\n%s", err, out)
	}
	if f.called("POST /containers/create") || f.called("POST /containers/"+testOldID+"/stop") {
		t.Errorf("nothing may be touched: %v", f.calls)
	}
}

func TestDockerEditRollsBack(t *testing.T) {
	f := &fakeDocker{failNewStart: true}
	startFakeDocker(t, f)
	want := fakeSpec(t, f)
	want.Env = append(want.Env, editEnv{"B", "2"})
	if _, err := runEdit(t, want); err == nil {
		t.Fatal("a new container that cannot start must fail the edit")
	}
	if !f.called("DELETE /containers/"+testNewID) || !f.called("POST /containers/"+testOldID+"/rename?name=web") {
		t.Errorf("the old container must come back: %v", f.calls)
	}
}

func TestDockerEditComposeAndNoChange(t *testing.T) {
	f := &fakeDocker{labels: map[string]any{composeProject: "shop"}}
	startFakeDocker(t, f)
	want := fakeSpec(t, f)
	if out, err := runEdit(t, want); err != nil || !strings.Contains(out, "Nothing to change") {
		t.Errorf("no change: %v %q", err, out)
	}
	want.Env = append(want.Env, editEnv{"B", "2"})
	if _, err := runEdit(t, want); err == nil {
		t.Error("a compose container must refuse a variable")
	}
	if f.called("POST /containers/create") {
		t.Error("nothing may be created")
	}
}
