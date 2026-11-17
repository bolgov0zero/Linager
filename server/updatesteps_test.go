package server

import (
	"context"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/bolgov0zero/linager/internal/protocol"
	"github.com/bolgov0zero/linager/server/store"
)

func states(st []store.UpdateStep) string {
	var b strings.Builder
	for _, s := range st {
		b.WriteString(map[string]string{"pending": ".", "running": "~", "done": "#", "failed": "!"}[s.State])
	}
	return b.String()
}

func TestTimelineOfAnOldAgent(t *testing.T) {
	// an agent that predates the stages sends "this step is reached" without a state
	st := newTimeline()
	for i, id := range []string{"download", "verify", "backup", "replace", "restart"} {
		st = applyEvent(st, protocol.UpdateEvent{Step: i + 1, Total: 5, ID: id}, int64(1000*(i+1)))
	}
	if got := states(st); got != "####~..." {
		t.Fatalf("got %s", got)
	}
	if st[0].FinishedMS == 0 || st[0].StartedMS != 1000 {
		t.Fatalf("download must have a start and an end: %+v", st[0])
	}
}

func TestTimelineOfAnAgentWithStates(t *testing.T) {
	st := newTimeline()
	st = applyEvent(st, protocol.UpdateEvent{ID: "download", State: protocol.UpdateRunning}, 1000)
	st = applyEvent(st, protocol.UpdateEvent{ID: "download", State: protocol.UpdateRunning, Bytes: 100, Size: 400}, 2000)
	if st[0].Bytes != 100 || st[0].Size != 400 || states(st) != "~......." {
		t.Fatalf("progress lost: %+v", st[0])
	}
	st = applyEvent(st, protocol.UpdateEvent{ID: "download", State: protocol.UpdateDone, Bytes: 400, Size: 400}, 3000)
	st = applyEvent(st, protocol.UpdateEvent{ID: "verify", State: protocol.UpdateRunning}, 3001)
	st = applyEvent(st, protocol.UpdateEvent{ID: "verify", State: protocol.UpdateDone}, 3002)
	if got := states(st); got != "##......" || st[0].FinishedMS-st[0].StartedMS != 2000 {
		t.Fatalf("got %s %+v", got, st[0])
	}
	// a late progress event must not reopen a finished stage
	st = applyEvent(st, protocol.UpdateEvent{ID: "download", State: protocol.UpdateRunning, Bytes: 5}, 4000)
	if st[0].State != "done" {
		t.Fatalf("a finished stage was reopened: %+v", st[0])
	}
	st = applyEvent(st, protocol.UpdateEvent{ID: "backup", State: protocol.UpdateFailed, Error: "disk full"}, 5000)
	if got := states(st); got != "##!....." || st[2].Detail != "disk full" {
		t.Fatalf("got %s %+v", got, st[2])
	}
}

func TestTimelineAfterTheRestart(t *testing.T) {
	st := newTimeline()
	for i, id := range []string{"download", "verify", "backup", "replace"} {
		st = applyEvent(st, protocol.UpdateEvent{ID: id, State: protocol.UpdateDone}, int64(10000+i))
	}
	if rollbackAt(st) != 10+int64(protocol.AgentRollbackDelay/time.Second) {
		t.Fatalf("rollback time: %d", rollbackAt(st))
	}
	if _, changed := markOffline(st, 11000); changed {
		t.Fatal("a drop before the restart is not a stage")
	}
	st = applyEvent(st, protocol.UpdateEvent{ID: "restart", State: protocol.UpdateRunning}, 11000)
	st, changed := markOffline(st, 12000)
	if !changed || states(st) != "#####~.." || st[4].FinishedMS != 12000 {
		t.Fatalf("offline: %s %+v", states(st), st[4])
	}
	st = markReconnect(st, 20000, "0.2.0")
	if got := states(st); got != "#######~" {
		t.Fatalf("reconnect: %s", got)
	}
	if st[stageOffline].FinishedMS-st[stageOffline].StartedMS != 8000 || st[stageReconnect].FinishedMS != 20000 || st[stageReconnect].Detail != "0.2.0" || st[stageConfirm].State != "running" {
		t.Fatalf("reconnect stage: %+v %+v", st[stageReconnect], st[stageConfirm])
	}
	st = applyEvent(st, protocol.UpdateEvent{ID: "confirm", State: protocol.UpdateDone}, 21000)
	if states(st) != "########" || rollbackAt(st) != 0 {
		t.Fatalf("confirm: %s", states(st))
	}
}

func TestTimelineRollback(t *testing.T) {
	st := newTimeline()
	for _, id := range []string{"download", "verify", "backup", "replace"} {
		st = applyEvent(st, protocol.UpdateEvent{ID: id, State: protocol.UpdateDone}, 1000)
	}
	st = applyEvent(st, protocol.UpdateEvent{ID: "restart", State: protocol.UpdateRunning}, 2000)
	st, _ = markOffline(st, 3000)
	st = markOldVersionBack(st, 80000, "0.1.0")
	if got := states(st); got != "######!." {
		t.Fatalf("old version back: %s", got)
	}
	if st[stageReconnect].State != "failed" || st[stageReconnect].Detail != "0.1.0" {
		t.Fatalf("%+v", st[stageReconnect])
	}
	// the stage that failed first stays the one that is marked
	again := failRunning(st, 90000, "later")
	if states(again) != states(st) {
		t.Fatalf("a second failure must not move the mark: %s", states(again))
	}
	// an agent that went silent: the stage that was going on fails
	silent := newTimeline()
	silent = applyEvent(silent, protocol.UpdateEvent{ID: "restart", State: protocol.UpdateRunning}, 1000)
	silent, _ = markOffline(silent, 2000)
	silent = failRunning(silent, 150000, "no answer")
	if silent[stageOffline].State != "failed" || silent[stageOffline].Detail != "no answer" {
		t.Fatalf("%+v", silent[stageOffline])
	}
}

func TestNormalizeReplacesAForeignTimeline(t *testing.T) {
	if got := normalize([]store.UpdateStep{{ID: "x"}}); len(got) != len(stageIDs) || got[0].ID != "download" {
		t.Fatalf("%+v", got)
	}
	if currentStep(newTimeline()) != 1 {
		t.Fatal("a new timeline starts at the first stage")
	}
}

// TestAgentUpdateLifecycle runs a whole update through the database: events, drop, hello, confirmation, and the
// rollback and the watchdog. It needs LINAGER_TEST_DSN like the other end-to-end tests.
func TestAgentUpdateLifecycle(t *testing.T) {
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
	defer func() { _ = conn.Close(ctx) }()
	if _, err := conn.Exec(ctx, `DROP SCHEMA public CASCADE; CREATE SCHEMA public`); err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var id string
	if err := db.Pool.QueryRow(ctx, `INSERT INTO servers (name,hostname,address,agent_pubkey,os,agent_version,protocol)
		VALUES ('web','web','10.0.0.1','\x00','Debian','0.1.0',1) RETURNING id::text`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	u := &Updates{DB: db, Hub: NewHub(), Log: slog.Default()}
	start := func() {
		if err := db.SetAgentUpdate(ctx, store.AgentUpdate{ServerID: id, Version: "0.2.0", Status: "updating", Step: 1, Steps: newTimeline()}); err != nil {
			t.Fatal(err)
		}
	}
	row := func() store.AgentUpdate {
		r, ok := db.AgentUpdate(ctx, id)
		if !ok {
			t.Fatal("no update row")
		}
		return r
	}
	for _, id := range []string{"download", "verify", "backup", "replace"} {
		u.AgentEvent(ctx, "", protocol.UpdateEvent{ID: id, State: protocol.UpdateDone}) // no row of that server: nothing happens
	}

	// a successful run
	start()
	for _, st := range []string{"download", "verify", "backup", "replace"} {
		u.AgentEvent(ctx, id, protocol.UpdateEvent{ID: st, State: protocol.UpdateDone})
	}
	u.AgentEvent(ctx, id, protocol.UpdateEvent{ID: "restart", State: protocol.UpdateRunning})
	u.AgentOnline(ctx, id, false)
	if got := states(row().Steps); got != "#####~.." {
		t.Fatalf("after the drop: %s", got)
	}
	u.AgentHello(ctx, id, "0.2.0")
	if r := row(); states(r.Steps) != "#######~" || r.Status != "updating" {
		t.Fatalf("after hello: %s %s", states(r.Steps), r.Status)
	}
	u.AgentEvent(ctx, id, protocol.UpdateEvent{ID: "confirm", State: protocol.UpdateDone})
	if _, ok := db.AgentUpdate(ctx, id); ok {
		t.Fatal("a confirmed update must leave no row")
	}

	// the agent comes back with its old version: rolled back, the timeline stays for the panel
	start()
	u.AgentEvent(ctx, id, protocol.UpdateEvent{ID: "restart", State: protocol.UpdateRunning})
	u.AgentOnline(ctx, id, false)
	u.AgentHello(ctx, id, "0.1.0")
	if r := row(); r.Status != "rolled_back" || r.Steps[stageReconnect].State != "failed" {
		t.Fatalf("rollback: %s %+v", r.Status, r.Steps)
	}

	// a new run clears the old timeline; an error event fails the run and keeps the mark
	start()
	if r := row(); states(r.Steps) != "........" {
		t.Fatalf("a new run must start clean: %s", states(r.Steps))
	}
	u.AgentEvent(ctx, id, protocol.UpdateEvent{ID: "verify", State: protocol.UpdateFailed, Error: "binary does not match the signed manifest"})
	if r := row(); r.Status != "failed" || r.Steps[1].State != "failed" || r.Message == "" {
		t.Fatalf("failure: %s %+v", r.Status, r.Steps[1])
	}

	// an agent that reconnected but never confirmed is counted as done after the grace period
	start()
	st := newTimeline()
	st = markReconnect(st, time.Now().Add(-time.Minute).UnixMilli(), "0.2.0")
	if err := db.SetAgentUpdate(ctx, store.AgentUpdate{ServerID: id, Version: "0.2.0", Status: "updating", Step: 8, Steps: st}); err != nil {
		t.Fatal(err)
	}
	u.settle(ctx)
	if _, ok := db.AgentUpdate(ctx, id); ok {
		t.Fatal("the watchdog must finish an update that was not confirmed within the grace period")
	}
}
