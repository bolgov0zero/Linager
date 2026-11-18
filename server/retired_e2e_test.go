package server_test

import (
	"context"
	"testing"

	"github.com/bolgov0zero/linager/migrations"
)

// TestRetiredLeftoversAreRemoved: a panel that ran 0.2.4 to 0.2.7 has the tables of the gateway, the layout column of
// the scheme view and a module that exists no more. The migration of 0.2.8 removes exactly those and nothing else.
func TestRetiredLeftoversAreRemoved(t *testing.T) {
	_, db := startAccountsServer(t)
	ctx := context.Background()
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := db.Pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
	}
	count := func(sql string) (n int) {
		t.Helper()
		if err := db.Pool.QueryRow(ctx, sql).Scan(&n); err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
		return n
	}

	// the schema as 0.2.7 left it
	exec(`CREATE TABLE gateways (server_id uuid PRIMARY KEY REFERENCES servers(id) ON DELETE CASCADE, enabled boolean)`)
	exec(`CREATE TABLE gateway_devices (id bigserial PRIMARY KEY, server_id uuid REFERENCES servers(id) ON DELETE CASCADE)`)
	exec(`CREATE TABLE gateway_events (id bigserial PRIMARY KEY, server_id uuid REFERENCES servers(id) ON DELETE CASCADE)`)
	exec(`ALTER TABLE dashboards ADD COLUMN scheme jsonb`)
	var sid string
	if err := db.Pool.QueryRow(ctx, `INSERT INTO servers (name,hostname,address,agent_pubkey,os,agent_version,protocol)
		VALUES ('web','web','10.0.0.1','\x01','Debian','0.2.7',1) RETURNING id::text`).Scan(&sid); err != nil {
		t.Fatal(err)
	}
	exec(`INSERT INTO gateways (server_id, enabled) VALUES ($1, true)`, sid)
	exec(`INSERT INTO gateway_devices (server_id) VALUES ($1)`, sid)
	exec(`INSERT INTO gateway_events (server_id) VALUES ($1)`, sid)
	exec(`UPDATE dashboards SET scheme='{"nodes":{}}'::jsonb`)
	exec(`INSERT INTO user_modules (user_id, module) SELECT id, m FROM users, unnest(ARRAY['gateway','disk']) AS m LIMIT 2`)
	exec(`INSERT INTO audit_log (actor, action, target_type, result) VALUES ('admin','gateway.config','server','ok')`)

	sql, err := migrations.FS.ReadFile("00017_remove_gateway_and_scheme.sql")
	if err != nil {
		t.Fatal(err)
	}
	exec(string(sql))

	if n := count(`SELECT count(*) FROM information_schema.tables WHERE table_name IN ('gateways','gateway_devices','gateway_events')`); n != 0 {
		t.Errorf("%d gateway tables are left", n)
	}
	if n := count(`SELECT count(*) FROM information_schema.columns WHERE table_name='dashboards' AND column_name='scheme'`); n != 0 {
		t.Errorf("the scheme column is left")
	}
	if n := count(`SELECT count(*) FROM user_modules WHERE module='gateway'`); n != 0 {
		t.Errorf("the module gateway is left in the access of users: %d", n)
	}
	// everything else stays: the other module, the server, the dashboards, the audit log
	if n := count(`SELECT count(*) FROM user_modules WHERE module='disk'`); n != 1 {
		t.Errorf("another module of a user must stay: %d", n)
	}
	if n := count(`SELECT count(*) FROM servers`); n != 1 {
		t.Errorf("servers: %d", n)
	}
	if n := count(`SELECT count(*) FROM dashboards`); n < 1 {
		t.Errorf("dashboards: %d", n)
	}
	if n := count(`SELECT count(*) FROM audit_log WHERE action='gateway.config'`); n != 1 {
		t.Errorf("the audit log is history and stays: %d", n)
	}
	// a second run, and a panel that never had them, find nothing to remove
	exec(string(sql))
}
