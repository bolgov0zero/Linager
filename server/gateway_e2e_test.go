package server_test

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/bolgov0zero/linager/internal/protocol"
	"github.com/bolgov0zero/linager/server"
	"github.com/bolgov0zero/linager/server/store"
)

type gwTestState struct {
	Enabled     bool  `json:"enabled"`
	IntervalS   int   `json:"interval_s"`
	AgentOnline bool  `json:"agent_online"`
	LastDataAt  int64 `json:"last_data_at"`
	Nets        []struct {
		CIDR   string `json:"cidr"`
		On     bool   `json:"on"`
		Own    bool   `json:"own"`
		Public bool   `json:"public"`
		Iface  string `json:"iface"`
		VLAN   string `json:"vlan"`
		Via    string `json:"via"`
		Last   *struct {
			Probed   int    `json:"probed"`
			Answered int    `json:"answered"`
			Found    int    `json:"found"`
			Error    string `json:"error"`
		} `json:"last"`
	} `json:"nets"`
	Devices []struct {
		ID          int64    `json:"id"`
		IP          string   `json:"ip"`
		MAC         string   `json:"mac"`
		Vendor      string   `json:"vendor"`
		RandomMAC   bool     `json:"random_mac"`
		Name        string   `json:"name"`
		NameSrc     string   `json:"name_src"`
		OwnName     string   `json:"own_name"`
		Net         string   `json:"net"`
		Online      bool     `json:"online"`
		Excluded    bool     `json:"excluded"`
		IsNew       bool     `json:"is_new"`
		Hints       []string `json:"hints"`
		KnownServer string   `json:"known_server"`
		Type        string   `json:"type"`
		Why         []struct {
			Kind  string `json:"kind"`
			Value string `json:"value"`
		} `json:"why"`
		Svc  []string `json:"svc"`
		UPnP *struct {
			Type  string `json:"type"`
			Model string `json:"model"`
			Maker string `json:"maker"`
		} `json:"upnp"`
		Ports []struct {
			Port   int    `json:"port"`
			Banner string `json:"banner"`
		} `json:"ports"`
	} `json:"devices"`
	Events []struct {
		Kind string `json:"kind"`
		IP   string `json:"ip"`
		Name string `json:"name"`
	} `json:"events"`
	Live *struct{} `json:"live"`
}

func gwInsertServer(t *testing.T, db *store.Store, name, addr string, n int, modules ...string) string {
	t.Helper()
	ctx := context.Background()
	var id string
	if err := db.Pool.QueryRow(ctx, `INSERT INTO servers (name,hostname,address,agent_pubkey,os,agent_version,protocol)
		VALUES ($1,$1,$2,$3,'Debian','0.3.0',1) RETURNING id::text`, name, addr, []byte{byte(n & 0xff)}).Scan(&id); err != nil {
		t.Fatal(err)
	}
	if err := db.SetModules(ctx, id, modules); err != nil {
		t.Fatal(err)
	}
	return id
}

func gwNet(cidr string) map[string]any {
	return map[string]any{"cidr": cidr, "on": true, "ports": true, "names": true}
}

func gwSave(nets ...map[string]any) map[string]any {
	if nets == nil {
		nets = []map[string]any{}
	}
	return map[string]any{"enabled": true, "interval_s": 300, "nets": nets, "confirm": true}
}

// TestGatewayConfigAPI: the configuration is checked by the rules of the module, saved, audited, readable by an
// operator who has the module and by nobody else, and changeable by administrators only.
func TestGatewayConfigAPI(t *testing.T) {
	base, db := startAccountsServer(t)
	ctx := context.Background()
	id := gwInsertServer(t, db, "rpi", "192.168.1.40", 1) // no gateway module yet
	admin := mustLogin(t, base, "admin", "a-long-test-password")
	path := "/api/servers/" + id + "/gateway"

	var st gwTestState
	if code := admin.do("GET", path, nil, &st); code != 200 || st.Enabled || len(st.Nets) != 0 || st.AgentOnline || st.Live != nil || st.Devices == nil || st.Events == nil {
		t.Fatalf("a new gateway is off and empty: %d %+v", code, st)
	}
	if code := admin.do("PUT", path, gwSave(gwNet("192.168.1.0/24")), nil); code != http.StatusConflict {
		t.Fatalf("an agent without the module cannot be configured: %d", code)
	}
	if err := db.SetModules(ctx, id, []string{"gateway"}); err != nil {
		t.Fatal(err)
	}
	noConfirm := gwSave(gwNet("192.168.1.0/24"))
	noConfirm["confirm"] = false
	if code := admin.do("PUT", path, noConfirm, nil); code != http.StatusConflict {
		t.Fatalf("a change needs the confirmation: %d", code)
	}

	bad := map[string]map[string]any{
		"bad_interval":       {"enabled": true, "interval_s": 5, "nets": []any{}, "confirm": true},
		"too_many_nets":      {"enabled": true, "interval_s": 300, "nets": manyNets(17), "confirm": true},
		"bad_net":            gwSave(gwNet("hello")),
		"bad_net/loopback":   gwSave(gwNet("127.0.0.0/24")),
		"too_big":            gwSave(gwNet("10.20.0.0/22")),
		"public_unconfirmed": gwSave(gwNet("203.0.113.0/24")),
		"overlap":            gwSave(gwNet("10.10.0.0/24"), gwNet("10.10.0.0/25")),
	}
	for want, body := range bad {
		var out struct{ Code string }
		if code := admin.do("PUT", path, body, &out); code != http.StatusUnprocessableEntity || out.Code != strings.Split(want, "/")[0] {
			t.Errorf("%s: %d %q", want, code, out.Code)
		}
	}
	if code := admin.do("GET", path, nil, &st); code != 200 || len(st.Nets) != 0 {
		t.Fatalf("a refused configuration saves nothing: %d %+v", code, st)
	}

	// a good one; the host bits of a network are cleared
	if code := admin.do("PUT", path, gwSave(gwNet("192.168.1.77/24"), gwNet("10.10.0.0/24")), &st); code != 200 || !st.Enabled || st.IntervalS != 300 || len(st.Nets) != 2 || st.Nets[0].CIDR != "192.168.1.0/24" || st.Nets[0].Public {
		t.Fatalf("save: %d %+v", code, st)
	}
	// a public network needs its own confirmation, and keeps it afterwards
	pub := gwNet("203.0.113.0/24")
	pub["confirm_public"] = true
	if code := admin.do("PUT", path, gwSave(gwNet("192.168.1.0/24"), pub), &st); code != 200 || len(st.Nets) != 2 || !st.Nets[1].Public {
		t.Fatalf("a confirmed public network: %d %+v", code, st)
	}
	if code := admin.do("PUT", path, gwSave(gwNet("192.168.1.0/24"), gwNet("203.0.113.0/24")), &st); code != 200 || !st.Nets[1].Public {
		t.Fatalf("a network that was confirmed stays confirmed: %d %+v", code, st)
	}

	// the audit says what changed, not more
	var n int
	if err := db.Pool.QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE action='gateway.config' AND target_id=$1 AND result='ok'`, id).Scan(&n); err != nil || n != 3 {
		t.Fatalf("audit entries: %d %v", n, err)
	}
	if err := db.Pool.QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE action='gateway.config' AND details::text LIKE '%confirmed_public%203.0.113.0/24%'`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("the confirmation of a public network is in the audit log once: %d %v", n, err)
	}

	// an operator with the module reads and cannot change; without the module the tab does not exist for them
	for _, c := range []struct {
		login   string
		modules []string
		read    int
	}{{"op-gw", []string{"gateway"}, 200}, {"op-no", []string{"disk"}, 403}} {
		if code := admin.do("POST", "/api/users", map[string]any{"login": c.login, "password": c.login + "-long-password", "role": "operator", "servers": []string{id}, "modules": c.modules}, nil); code != 200 {
			t.Fatalf("create %s: %d", c.login, code)
		}
		op := mustLogin(t, base, c.login, c.login+"-long-password")
		if code := op.do("GET", path, nil, nil); code != c.read {
			t.Errorf("%s reads: %d, want %d", c.login, code, c.read)
		}
		if code := op.do("PUT", path, gwSave(gwNet("192.168.1.0/24")), nil); code != http.StatusForbidden {
			t.Errorf("%s must not change the configuration: %d", c.login, code)
		}
		if code := op.do("POST", path+"/scan", nil, nil); code != http.StatusForbidden {
			t.Errorf("%s must not start a pass: %d", c.login, code)
		}
		if code := op.do("PATCH", path+"/devices/1", map[string]any{"excluded": true}, nil); code != http.StatusForbidden {
			t.Errorf("%s must not change a device: %d", c.login, code)
		}
	}
	// "scan now" reaches an agent that is offline as an error, not as a success
	if code := admin.do("POST", path+"/scan", nil, nil); code != http.StatusServiceUnavailable {
		t.Errorf("scan now with the agent offline: %d", code)
	}
}

func manyNets(n int) []map[string]any {
	out := []map[string]any{}
	for i := 0; i < n; i++ {
		out = append(out, gwNet(fmt.Sprintf("10.%d.0.0/24", i)))
	}
	return out
}

func gwPass(cidr, id string, own bool, devs ...protocol.GatewayDevice) protocol.GatewayPass {
	return protocol.GatewayPass{PassID: id, CIDR: cidr, Own: own, Iface: "eth0", StartedAt: 1, FinishedAt: 2, Probed: 254, Answered: len(devs), Devices: devs, Last: true}
}

func gwDevice(ip, mac, name string, ports ...protocol.GatewayPort) protocol.GatewayDevice {
	d := protocol.GatewayDevice{IP: ip, MAC: mac, Via: []string{"arp"}, Ports: ports}
	if name != "" {
		d.Names = []protocol.GatewayName{{Src: "ptr", Name: name}}
	}
	return d
}

// TestGatewayIngest: what an agent reports becomes devices and events: the first pass is no news, a new device is,
// a device is gone after two passes without it, it keeps its row when its address changes, a replay changes nothing,
// and what the administrator set (a name, the exclusion) survives the passes.
func TestGatewayIngest(t *testing.T) {
	base, db := startAccountsServer(t)
	ctx := context.Background()
	id := gwInsertServer(t, db, "rpi", "192.168.1.40", 1, "gateway")
	other := gwInsertServer(t, db, "nas", "192.168.1.20", 2, "gateway") // an agent that stands in the network of the first
	admin := mustLogin(t, base, "admin", "a-long-test-password")
	path := "/api/servers/" + id + "/gateway"
	var st gwTestState
	if code := admin.do("PUT", path, gwSave(gwNet("192.168.1.0/24"), gwNet("10.10.0.0/24")), &st); code != 200 {
		t.Fatalf("save: %d", code)
	}
	gw := server.NewGateway(db, server.NewHub(), slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn})))
	state := func() gwTestState {
		t.Helper()
		var s gwTestState
		if code := admin.do("GET", path, nil, &s); code != 200 {
			t.Fatalf("state: %d", code)
		}
		return s
	}
	find := func(s gwTestState, ip string) int {
		for i, d := range s.Devices {
			if d.IP == ip {
				return i
			}
		}
		return -1
	}

	const (
		macA = "A4:5E:60:1C:07:2B" // Apple, in capitals: it is stored in lower case
		macB = "bc:24:11:8a:3d:90"
		macC = "da:a1:19:6b:55:0c" // locally administered: a random address
	)
	ssh := protocol.GatewayPort{Port: 22, Banner: "SSH-2.0-OpenSSH_9.2p1 Debian-2+deb12u3"}
	pve := protocol.GatewayPort{Port: 8006, Banner: "Proxmox Virtual Environment"}

	// the first pass: everything is found at once, nothing is news
	gw.Ingest(ctx, id, gwPass("192.168.1.0/24", "p1", true, gwDevice("192.168.1.1", macA, "router.lan", ssh), gwDevice("192.168.1.20", macB, "", pve)))
	s := state()
	if len(s.Devices) != 2 || len(s.Events) != 0 || s.LastDataAt == 0 {
		t.Fatalf("first pass: %+v", s)
	}
	a, b := s.Devices[find(s, "192.168.1.1")], s.Devices[find(s, "192.168.1.20")]
	if a.MAC != strings.ToLower(macA) || !strings.Contains(a.Vendor, "Apple") || a.Name != "router.lan" || a.NameSrc != "ptr" || a.IsNew || !a.Online {
		t.Errorf("device A: %+v", a)
	}
	if !slices.Contains(a.Hints, "debian") || b.Type != "hypervisor" || b.KnownServer != "nas" || slices.Contains(b.Hints, "debian") {
		t.Errorf("hints and the known server: A %v, B %v %q", a.Hints, b.Hints, b.KnownServer)
	}
	if n := s.Nets[0]; n.Last == nil || n.Last.Found != 2 || n.Last.Answered != 2 || n.Last.Probed != 254 || !n.Own || n.Iface != "eth0" {
		t.Errorf("the summary of the network: %+v", n)
	}

	// the second pass: a new device is news, with a random address and no manufacturer
	gw.Ingest(ctx, id, gwPass("192.168.1.0/24", "p2", true, gwDevice("192.168.1.1", macA, "", ssh), gwDevice("192.168.1.20", macB, "", pve), gwDevice("192.168.1.133", macC, "")))
	s = state()
	c := s.Devices[find(s, "192.168.1.133")]
	if len(s.Devices) != 3 || len(s.Events) != 1 || s.Events[0].Kind != "appeared" || s.Events[0].IP != "192.168.1.133" || !c.IsNew || !c.RandomMAC || c.Vendor != "" {
		t.Fatalf("second pass: %+v", s)
	}
	if s.Devices[find(s, "192.168.1.1")].Name != "router.lan" {
		t.Errorf("a pass that found no name keeps the one that was found")
	}

	// the administrator names a device and leaves another off the map
	if code := admin.do("PATCH", path+fmt.Sprintf("/devices/%d", a.ID), map[string]any{"own_name": "  gateway router  "}, nil); code != 200 {
		t.Fatalf("name: %d", code)
	}
	if code := admin.do("PATCH", path+fmt.Sprintf("/devices/%d", c.ID), map[string]any{"excluded": true}, nil); code != 200 {
		t.Fatalf("exclude: %d", code)
	}
	if code := admin.do("PATCH", path+fmt.Sprintf("/devices/%d", a.ID), map[string]any{"own_name": strings.Repeat("я", 101)}, nil); code != http.StatusUnprocessableEntity {
		t.Errorf("a long name: %d", code)
	}
	if code := admin.do("PATCH", path+"/devices/999999", map[string]any{"excluded": true}, nil); code != http.StatusNotFound {
		t.Errorf("an unknown device: %d", code)
	}
	if code := admin.do("PATCH", "/api/servers/"+other+"/gateway/devices/"+fmt.Sprint(a.ID), map[string]any{"excluded": true}, nil); code != http.StatusNotFound {
		t.Errorf("a device of another agent: %d", code)
	}
	if code := admin.do("PATCH", path+fmt.Sprintf("/devices/%d", a.ID), map[string]any{}, nil); code != http.StatusBadRequest {
		t.Errorf("nothing to change: %d", code)
	}

	// one pass without B is a lost packet, the second one is "gone"
	gw.Ingest(ctx, id, gwPass("192.168.1.0/24", "p3", true, gwDevice("192.168.1.1", macA, "", ssh), gwDevice("192.168.1.133", macC, "")))
	s = state()
	if !s.Devices[find(s, "192.168.1.20")].Online || len(s.Events) != 1 {
		t.Fatalf("one missed pass is no alarm: %+v", s.Events)
	}
	gw.Ingest(ctx, id, gwPass("192.168.1.0/24", "p4", true, gwDevice("192.168.1.1", macA, "", ssh), gwDevice("192.168.1.133", macC, "")))
	s = state()
	if s.Devices[find(s, "192.168.1.20")].Online || len(s.Events) != 2 || s.Events[0].Kind != "disappeared" || s.Events[0].IP != "192.168.1.20" {
		t.Fatalf("gone after two passes: %+v", s.Events)
	}
	gw.Ingest(ctx, id, gwPass("192.168.1.0/24", "p5", true, gwDevice("192.168.1.1", macA, "", ssh), gwDevice("192.168.1.20", macB, "", pve), gwDevice("192.168.1.133", macC, "")))
	s = state()
	if !s.Devices[find(s, "192.168.1.20")].Online || len(s.Events) != 3 || s.Events[0].Kind != "appeared" {
		t.Fatalf("back again: %+v", s.Events)
	}

	// a replay of a pass the panel has seen changes nothing
	gw.Ingest(ctx, id, gwPass("192.168.1.0/24", "p5", true, gwDevice("192.168.1.1", macA, "", ssh)))
	if s2 := state(); len(s2.Events) != 3 || !s2.Devices[find(s2, "192.168.1.20")].Online {
		t.Fatalf("a replay: %+v", s2.Events)
	}

	// what the administrator set survives, and the exclusion hides nothing from the API (the page filters)
	s = state()
	a, c = s.Devices[find(s, "192.168.1.1")], s.Devices[find(s, "192.168.1.133")]
	if a.OwnName != "gateway router" || a.Name != "router.lan" || !c.Excluded {
		t.Errorf("name and exclusion: %+v %+v", a, c)
	}

	// a DHCP lease moves a device to another address: one row, new address
	gw.Ingest(ctx, id, gwPass("192.168.1.0/24", "p6", true, gwDevice("192.168.1.1", macA, "", ssh), gwDevice("192.168.1.20", macB, "", pve), gwDevice("192.168.1.150", macC, "")))
	s = state()
	if len(s.Devices) != 3 || find(s, "192.168.1.133") >= 0 || find(s, "192.168.1.150") < 0 || !s.Devices[find(s, "192.168.1.150")].Excluded {
		t.Fatalf("a device that changed its address keeps its row: %+v", s.Devices)
	}

	// behind a router the MAC is the router's: it is dropped, the device is known by its address
	gw.Ingest(ctx, id, gwPass("10.10.0.0/24", "q1", false, gwDevice("10.10.0.5", macA, "far-host")))
	s = state()
	far := s.Devices[find(s, "10.10.0.5")]
	if far.MAC != "" || far.Vendor != "" || far.Net != "10.10.0.0/24" {
		t.Errorf("a device behind a router: %+v", far)
	}

	// addresses outside the network, a network nobody configured, and a failed pass
	gw.Ingest(ctx, id, gwPass("10.10.0.0/24", "q2", false, gwDevice("10.10.0.5", "", ""), gwDevice("172.16.0.9", "", "stranger")))
	gw.Ingest(ctx, id, gwPass("10.99.0.0/24", "z1", false, gwDevice("10.99.0.1", "", "nobody")))
	s = state()
	if find(s, "172.16.0.9") >= 0 || find(s, "10.99.0.1") >= 0 {
		t.Errorf("only the addresses of a configured network are kept: %+v", s.Devices)
	}
	failed := gwPass("10.10.0.0/24", "q3", false)
	failed.Err = "the agent has no route"
	gw.Ingest(ctx, id, failed)
	s = state()
	if s.Nets[1].Last == nil || s.Nets[1].Last.Error == "" || !s.Devices[find(s, "10.10.0.5")].Online {
		t.Errorf("a failed pass shows its error and does not make devices gone: %+v", s.Nets[1])
	}
}

// TestGatewayKinds: what a pass reports about services and UPnP is checked, kept and turned into the kind of the
// device with its reasons when the devices are shown.
func TestGatewayKinds(t *testing.T) {
	base, db := startAccountsServer(t)
	ctx := context.Background()
	id := gwInsertServer(t, db, "rpi", "192.168.1.40", 1, "gateway")
	admin := mustLogin(t, base, "admin", "a-long-test-password")
	path := "/api/servers/" + id + "/gateway"
	if code := admin.do("PUT", path, gwSave(gwNet("192.168.1.0/24")), nil); code != 200 {
		t.Fatalf("save: %d", code)
	}
	gw := server.NewGateway(db, server.NewHub(), slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn})))
	state := func() gwTestState {
		var s gwTestState
		if code := admin.do("GET", path, nil, &s); code != 200 {
			t.Fatalf("state: %d", code)
		}
		return s
	}
	by := func(s gwTestState, ip string) int {
		for i, d := range s.Devices {
			if d.IP == ip {
				return i
			}
		}
		t.Fatalf("no device %s", ip)
		return -1
	}

	router := gwDevice("192.168.1.1", "a4:5e:60:1c:07:2b", "", protocol.GatewayPort{Port: 8291})
	router.Router = true
	printer := gwDevice("192.168.1.60", "3c:2a:f4:77:b2:6e", "", protocol.GatewayPort{Port: 9100})
	printer.Svc = []string{"_IPP._tcp", "_http._tcp", "not a service", "_x._icmp", "_ipp._tcp"}
	tv := gwDevice("192.168.1.110", "8c:79:f5:21:66:d4", "")
	tv.UPnP = &protocol.GatewayUPnP{Type: "MediaRenderer", Model: strings.Repeat("m", 300), Maker: "Samsung Electronics"}
	tv.Names = []protocol.GatewayName{{Src: "ptr", Name: "ptr-name"}, {Src: "ssdp", Name: "[TV] Living room"}, {Src: "evil", Name: "evil-name"}, {Src: "mdns", Name: "tv-mdns"}}
	unknown := gwDevice("192.168.1.99", "00:11:22:33:44:55", "")
	gw.Ingest(ctx, id, gwPass("192.168.1.0/24", "p1", true, router, printer, tv, unknown))
	s := state()
	r, p, v, u := s.Devices[by(s, "192.168.1.1")], s.Devices[by(s, "192.168.1.60")], s.Devices[by(s, "192.168.1.110")], s.Devices[by(s, "192.168.1.99")]
	if r.Type != "router" || len(r.Why) < 2 || r.Why[0].Kind != "gateway" || r.Why[1].Kind != "port" || r.Why[1].Value != "8291" {
		t.Errorf("the router and its reasons: %+v", r)
	}
	if p.Type != "printer" || len(p.Svc) != 2 || p.Svc[0] != "_ipp._tcp" || p.Svc[1] != "_http._tcp" || p.Why[0].Kind != "service" {
		t.Errorf("the printer: the invalid and repeated service types are dropped: %+v", p)
	}
	if v.Type != "tv" || v.UPnP == nil || len(v.UPnP.Model) != 100 || v.UPnP.Maker != "Samsung Electronics" {
		t.Errorf("the tv and its clipped description: %+v", v)
	}
	if v.Name != "tv-mdns" || v.NameSrc != "mdns" {
		t.Errorf("mDNS is the best name, an unknown source is ignored: %q from %q", v.Name, v.NameSrc)
	}
	if u.Type != "" || len(u.Why) != 0 {
		t.Errorf("nothing says what an unknown device is: %+v", u)
	}

	// a pass that hears no services keeps what was learnt, and a device is a router only while the agent says so
	printer2 := gwDevice("192.168.1.60", "3c:2a:f4:77:b2:6e", "")
	router2 := gwDevice("192.168.1.1", "a4:5e:60:1c:07:2b", "")
	gw.Ingest(ctx, id, gwPass("192.168.1.0/24", "p2", true, router2, printer2, tv, unknown))
	s = state()
	r, p = s.Devices[by(s, "192.168.1.1")], s.Devices[by(s, "192.168.1.60")]
	if len(p.Svc) != 2 || p.Type != "printer" {
		t.Errorf("services survive a pass that did not hear them: %+v", p)
	}
	if r.Type != "" {
		t.Errorf("it is no router once the agent stops saying so (no ports, no UPnP): %+v", r)
	}
}
