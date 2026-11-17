package console

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/bolgov0zero/linager/server/store"
)

// TestServerStatusFromDB runs the status query against a real schema; it needs LINAGER_TEST_DSN like the
// end-to-end tests of the server.
func TestServerStatusFromDB(t *testing.T) {
	dsn := os.Getenv("LINAGER_TEST_DSN")
	if dsn == "" {
		t.Skip("LINAGER_TEST_DSN is not set")
	}
	if !strings.Contains(dsn, "test") {
		t.Fatal("refusing to wipe a database whose DSN does not contain 'test'")
	}
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx, `DROP SCHEMA public CASCADE; CREATE SCHEMA public`); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close(ctx) }()
	db, err := store.Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.CreateAdmin(ctx, "admin", "x"); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx, `INSERT INTO enrollments (agent_pubkey, hostname, nonce, expires_at) VALUES ('\x00', 'h', '\x00', now() + interval '1 hour')`); err != nil {
		t.Fatal(err)
	}

	l := layoutIn(t, true, false)
	cfg := fmt.Sprintf("database:\n  dsn: %q\nadmin:\n  listen: 127.0.0.1:8080\nagents:\n  listen: 0.0.0.0:7443\n", dsn)
	if err := os.WriteFile(l.ServerConfig, []byte(cfg), 0o600); err != nil { //nolint:gosec // a path of the test temp dir
		t.Fatal(err)
	}
	p := NewProbe(l, "0.1.0")
	p.Exec = func(context.Context, string, ...string) (string, error) {
		return "ActiveState=active\nLoadState=loaded\nActiveEnterTimestamp=@1700000000\n", nil
	}
	defer p.Close()

	for i := 0; i < 2; i++ { // the second round reuses the pool
		st := p.Server(ctx)
		if !st.DBOK || st.Users != 1 || st.Clients != 0 || st.Pending != 1 || st.Access != "off" {
			t.Fatalf("round %d: unexpected status %+v", i, st)
		}
		if st.Service.State != "active" || st.Service.Since.IsZero() || st.AgentListen != "0.0.0.0:7443" {
			t.Fatalf("round %d: unexpected service or config: %+v", i, st)
		}
	}
}
