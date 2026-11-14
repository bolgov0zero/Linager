package server_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/cookiejar"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/bolgov0zero/linager/agent"
	"github.com/bolgov0zero/linager/internal/config"
	"github.com/bolgov0zero/linager/internal/pki"
	"github.com/bolgov0zero/linager/internal/protocol"
	"github.com/bolgov0zero/linager/server"
	"github.com/bolgov0zero/linager/server/store"
)

type fakeCollector struct{ boot int64 }

func (f fakeCollector) Collect(now time.Time) protocol.Sample {
	return protocol.Sample{TS: now.Unix(), CPU: 42, MemPct: 55, MemUsed: 55, MemTotal: 100, DiskPct: 33, RxBps: 1000, TxBps: 500,
		BootTime: f.boot, Uptime: now.Unix() - f.boot, Updates: 3,
		Disks: []protocol.Disk{{Mount: "/", Device: "/dev/x", FS: "ext4", Total: 100, Used: 33, Pct: 33}}}
}

func (fakeCollector) System(v string) protocol.SystemInfo {
	return protocol.SystemInfo{Hostname: "h", OS: "Test OS", Kernel: "6.0", Arch: "amd64", CPUModel: "cpu", CPUCores: 2, MemTotal: 100, AgentVersion: v}
}

func freeAddr(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = l.Close() }()
	return l.Addr().String()
}

type apiClient struct {
	t    *testing.T
	base string
	hc   *http.Client
	csrf string
}

func (c *apiClient) do(method, path string, body any, out any) int {
	c.t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, c.base+path, rd)
	if c.csrf != "" {
		req.Header.Set("X-CSRF-Token", c.csrf)
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if out != nil {
		_ = json.NewDecoder(resp.Body).Decode(out)
	}
	return resp.StatusCode
}

func waitFor(t *testing.T, what string, f func() bool) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if f() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for: %s", what)
}

// TestEndToEnd needs a throw-away PostgreSQL database: set LINAGER_TEST_DSN (its name must contain "test").
func TestEndToEnd(t *testing.T) {
	dsn := os.Getenv("LINAGER_TEST_DSN")
	if dsn == "" {
		t.Skip("LINAGER_TEST_DSN is not set")
	}
	if !strings.Contains(dsn, "test") {
		t.Fatal("refusing to wipe a database whose DSN does not contain 'test'")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx, `DROP SCHEMA public CASCADE; CREATE SCHEMA public`); err != nil {
		t.Fatal(err)
	}
	_ = conn.Close(ctx)

	dir := t.TempDir()
	cfg := &config.Server{DataDir: dir}
	cfg.Database.DSN = dsn
	cfg.Admin.Listen, cfg.Agents.Listen = freeAddr(t), freeAddr(t)
	cfg.Agents.Hosts = []string{"127.0.0.1"}
	cfg.Admin.TrustedProxies = []string{"0.0.0.0/0"}
	cfg.Admin.CookieSecure = "never"
	if err := server.InitKeys(dir, cfg.Agents.Hosts); err != nil {
		t.Fatal(err)
	}
	keys, err := server.LoadKeys(dir)
	if err != nil {
		t.Fatal(err)
	}
	serverFP := pki.Fingerprint(keys.TLSDER)

	db, err := store.Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := server.CreateAdmin(ctx, db, "Admin", "a-long-test-password"); err != nil {
		t.Fatal(err)
	}
	if err := server.CreateAdmin(ctx, db, "second", "another-long-password"); err == nil {
		t.Fatal("a second administrator must be refused in this version")
	}

	srvDone := make(chan error, 1)
	go func() { srvDone <- server.Run(ctx, cfg, nil, slog.New(slog.NewTextHandler(io.Discard, nil))) }()
	waitFor(t, "server listening", func() bool {
		c, err := net.Dial("tcp", cfg.Admin.Listen)
		if err == nil {
			_ = c.Close()
		}
		return err == nil
	})

	jar, _ := cookiejar.New(nil)
	api := &apiClient{t: t, base: "http://" + cfg.Admin.Listen, hc: &http.Client{Jar: jar}}

	// --- authentication ---
	if code := api.do("GET", "/api/servers", nil, nil); code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated request: %d", code)
	}
	if code := api.do("POST", "/api/login", map[string]string{"login": "admin", "password": "wrong-password-1"}, nil); code != http.StatusUnauthorized {
		t.Fatalf("bad password: %d", code)
	}
	var lr struct{ CSRF string }
	if code := api.do("POST", "/api/login", map[string]string{"login": "ADMIN", "password": "a-long-test-password"}, &lr); code != 200 || lr.CSRF == "" {
		t.Fatalf("login: %d", code)
	}
	api.csrf = ""
	if code := api.do("DELETE", "/api/servers/x", map[string]any{}, nil); code != http.StatusForbidden {
		t.Fatalf("missing CSRF must be refused, got %d", code)
	}
	api.csrf = lr.CSRF

	// --- enrollment with a wrong pinned fingerprint is refused ---
	adata := t.TempDir()
	err = agent.Enroll(ctx, agent.EnrollOptions{Server: cfg.Agents.Listen, Fingerprint: "SHA256:wrong", DataDir: adata, Show: func(string, string, string) {}})
	if err == nil || !strings.Contains(err.Error(), "mismatch") {
		t.Fatalf("fingerprint mismatch must abort, got %v", err)
	}

	// --- full pairing ---
	type shown struct{ key, code string }
	shownCh := make(chan shown, 1)
	enrollErr := make(chan error, 1)
	go func() {
		enrollErr <- agent.Enroll(ctx, agent.EnrollOptions{
			Server: cfg.Agents.Listen, Fingerprint: serverFP, DataDir: adata, AgentVersion: "0.0.1-test", PollEvery: 100 * time.Millisecond,
			Show: func(k, c, _ string) { shownCh <- shown{k, c} },
		})
	}()
	sh := <-shownCh
	type pend struct {
		ID         string `json:"id"`
		VerifyCode string `json:"verify_code"`
		Hostname   string `json:"hostname"`
	}
	var pending []pend
	waitFor(t, "pending enrollment", func() bool {
		api.do("GET", "/api/enrollments", nil, &pending)
		return len(pending) == 1
	})
	if pending[0].VerifyCode != sh.code {
		t.Fatalf("check codes differ: admin %q agent %q", pending[0].VerifyCode, sh.code)
	}
	if code := api.do("POST", "/api/enrollments/"+pending[0].ID+"/approve", map[string]string{"key": "ZZZZ-ZZZZ"}, nil); code != http.StatusUnprocessableEntity {
		t.Fatalf("wrong key: %d", code)
	}
	if code := api.do("POST", "/api/enrollments/"+pending[0].ID+"/approve", map[string]string{"key": strings.ToLower(sh.key)}, nil); code != 200 {
		t.Fatalf("approve: %d", code)
	}
	if err := <-enrollErr; err != nil {
		t.Fatalf("enroll: %v", err)
	}

	// --- signed connection ---
	var uninstalled struct {
		purge bool
		ran   bool
	}
	runErr := make(chan error, 1)
	go func() {
		runErr <- agent.Run(ctx, agent.RunOptions{
			DataDir: adata, AgentVersion: "0.0.1-test", Log: slog.New(slog.NewTextHandler(io.Discard, nil)), PingEvery: 100 * time.Millisecond,
			SampleEvery: 200 * time.Millisecond, Collector: fakeCollector{boot: time.Now().Add(-time.Hour).Unix()},
			Uninstall: func(purge bool, _ string) error { uninstalled.purge, uninstalled.ran = purge, true; return nil },
		})
	}()
	type srvView struct {
		ID           string `json:"id"`
		Name         string `json:"name"`
		AgentVersion string `json:"agent_version"`
		Online       bool   `json:"online"`
	}
	var servers []srvView
	waitFor(t, "agent online", func() bool {
		api.do("GET", "/api/servers", nil, &servers)
		return len(servers) == 1 && servers[0].Online
	})
	id := servers[0].ID
	if servers[0].AgentVersion != "0.0.1-test" {
		t.Fatalf("agent version not recorded: %+v", servers[0])
	}

	// --- metrics, dashboard, history ---
	type dashItem struct {
		ID     string           `json:"id"`
		Online bool             `json:"online"`
		Sample *protocol.Sample `json:"sample"`
		Spark  []float64        `json:"spark"`
	}
	var dash struct {
		Items []dashItem `json:"items"`
	}
	waitFor(t, "live sample on the dashboard", func() bool {
		api.do("GET", "/api/dashboard", nil, &dash)
		return len(dash.Items) == 1 && dash.Items[0].Sample != nil && len(dash.Items[0].Spark) > 0
	})
	if sm := dash.Items[0].Sample; sm.CPU != 42 || sm.Updates != 3 || len(sm.Disks) != 1 {
		t.Fatalf("unexpected sample: %+v", sm)
	}
	var hist struct {
		Tier   string                           `json:"tier"`
		Points []map[string]any                 `json:"points"`
		Stats  map[string]struct{ Avg float64 } `json:"stats"`
		Events []struct{ Kind string }          `json:"events"`
	}
	waitFor(t, "raw history", func() bool {
		api.do("GET", "/api/servers/"+id+"/metrics?period=1h", nil, &hist)
		return len(hist.Points) > 0
	})
	if hist.Tier != "raw" || hist.Stats["cpu"].Avg != 42 {
		t.Fatalf("raw history: tier=%s stats=%+v", hist.Tier, hist.Stats)
	}
	(&server.Scheduler{DB: db, Log: slog.New(slog.NewTextHandler(io.Discard, nil))}).RunOnce(ctx)
	for _, p := range []string{"24h", "7d", "30d", "90d", "180d", "360d"} {
		hist.Points = nil
		api.do("GET", "/api/servers/"+id+"/metrics?period="+p, nil, &hist)
		if len(hist.Points) == 0 || hist.Stats["cpu"].Avg != 42 {
			t.Fatalf("period %s (tier %s): points=%d stats=%+v", p, hist.Tier, len(hist.Points), hist.Stats)
		}
	}
	if code := api.do("GET", "/api/servers/"+id+"/metrics?period=2y", nil, nil); code != http.StatusBadRequest {
		t.Fatalf("unknown period must be refused, got %d", code)
	}
	hasOnline := false
	for _, e := range hist.Events {
		hasOnline = hasOnline || e.Kind == "online"
	}
	if !hasOnline {
		t.Fatal("an 'online' event must be recorded when the agent connects")
	}
	req, _ := http.NewRequest("GET", api.base+"/api/servers/"+id+"/metrics.csv?period=1h", nil)
	if resp, err := api.hc.Do(req); err != nil || resp.StatusCode != 200 || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/csv") {
		t.Fatalf("csv export failed: %v", err)
	} else {
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if !strings.HasPrefix(string(body), "time_utc,cpu_avg") || strings.Count(string(body), "\n") < 2 {
			t.Fatalf("csv content: %q", body)
		}
	}
	// live stream delivers metric events
	sreq, _ := http.NewRequest("GET", api.base+"/api/live", nil)
	sctx, scancel := context.WithTimeout(ctx, 5*time.Second)
	sresp, err := api.hc.Do(sreq.WithContext(sctx))
	if err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 4096)
	got := ""
	for !strings.Contains(got, "event: metrics") {
		n, err := sresp.Body.Read(buf)
		got += string(buf[:n])
		if err != nil {
			scancel()
			t.Fatalf("live stream ended without a metrics event: %q (%v)", got, err)
		}
	}
	scancel()
	_ = sresp.Body.Close()
	// dashboard membership
	var avail []map[string]any
	if code := api.do("DELETE", "/api/dashboard/items/"+id, nil, nil); code != 200 {
		t.Fatalf("remove from dashboard: %d", code)
	}
	api.do("GET", "/api/dashboard/available", nil, &avail)
	api.do("GET", "/api/dashboard", nil, &dash)
	if len(avail) != 1 || len(dash.Items) != 0 {
		t.Fatalf("after remove: available=%d items=%d", len(avail), len(dash.Items))
	}
	if code := api.do("POST", "/api/dashboard/items", map[string]string{"server_id": id}, nil); code != 200 {
		t.Fatalf("add to dashboard: %d", code)
	}
	if code := api.do("PUT", "/api/dashboard/order", map[string]any{"server_ids": []string{id}}, nil); code != 200 {
		t.Fatalf("order: %d", code)
	}

	// --- deleting a client ---
	name := servers[0].Name
	if code := api.do("DELETE", "/api/servers/"+id, map[string]any{"name": "nope", "uninstall": true}, nil); code != http.StatusUnprocessableEntity {
		t.Fatalf("delete with wrong confirmation name: %d", code)
	}
	var del struct {
		Deleted       bool `json:"deleted"`
		UninstallSent bool `json:"uninstall_sent"`
	}
	if code := api.do("DELETE", "/api/servers/"+id, map[string]any{"name": name, "uninstall": true}, &del); code != 200 || !del.Deleted || !del.UninstallSent {
		t.Fatalf("delete: code=%d %+v", code, del)
	}
	select {
	case err := <-runErr:
		if !errors.Is(err, agent.ErrUninstalled) || !uninstalled.ran || !uninstalled.purge {
			t.Fatalf("agent must uninstall itself: err=%v %+v", err, uninstalled)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("agent did not react to the uninstall command")
	}
	api.do("GET", "/api/servers", nil, &servers)
	if len(servers) != 0 {
		t.Fatalf("server must be gone: %+v", servers)
	}

	// --- a deleted agent that still has its certificate is rejected as revoked ---
	err = agent.Run(ctx, agent.RunOptions{DataDir: adata, Log: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if !errors.Is(err, agent.ErrRevoked) {
		t.Fatalf("expected ErrRevoked, got %v", err)
	}

	// --- audit log ---
	var n int
	if err := db.Pool.QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE action IN ('auth.login','enroll.approve','server.delete')`).Scan(&n); err != nil || n < 4 {
		t.Fatalf("audit entries: %d err=%v", n, err)
	}

	// --- brute-forcing the enrollment key rejects the request ---
	bf := t.TempDir()
	shown2 := make(chan shown, 1)
	go func() {
		_ = agent.Enroll(ctx, agent.EnrollOptions{Server: cfg.Agents.Listen, Fingerprint: serverFP, DataDir: bf, PollEvery: 100 * time.Millisecond,
			Show: func(k, c, _ string) { shown2 <- shown{k, c} }})
	}()
	<-shown2
	waitFor(t, "second pending", func() bool { api.do("GET", "/api/enrollments", nil, &pending); return len(pending) == 1 })
	var last int
	for i := 0; i < 5; i++ {
		last = api.do("POST", "/api/enrollments/"+pending[0].ID+"/approve", map[string]string{"key": fmt.Sprintf("AAAA-%04d", i)}, nil)
	}
	if last != http.StatusGone {
		t.Fatalf("5th wrong key must reject the request (410), got %d", last)
	}

	cancel()
	select {
	case err := <-srvDone:
		if err != nil {
			t.Fatalf("server stopped with error: %v", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("server did not shut down")
	}
}
