package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/bolgov0zero/linager/internal/protocol"
)

func journal() (*slog.Logger, *bytes.Buffer) {
	var buf bytes.Buffer
	return slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})), &buf
}

func TestApplyWritesTheStagesToTheJournal(t *testing.T) {
	f := newApplyFixture(t, "new binary", "new binary")
	log, buf := journal()
	u := &updater{dir: f.dir, version: "0.1.0", log: log}
	if err := u.Apply(context.Background(), "0.2.0", f.key); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{
		"update: started", "update: source", "update: manifest fetched", "update: binary answered",
		"stage=download", "stage=verify", "stage=backup", "stage=replace", "update: installed, the rollback timer is armed",
		"avg_kib_s=", "total_ms=",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the journal has no %q:\n%s", want, out)
		}
	}
}

func TestApplyJournalsAFailedStageWithItsReason(t *testing.T) {
	f := newApplyFixture(t, "new binary", "tampered binary!")
	log, buf := journal()
	u := &updater{dir: f.dir, version: "0.1.0", log: log}
	if err := u.Apply(context.Background(), "0.2.0", f.key); err == nil {
		t.Fatal("a tampered binary must be refused")
	}
	out := buf.String()
	for _, want := range []string{"update: the binary does not match the manifest", "update: stage failed", "stage=verify", "timed_out=false"} {
		if !strings.Contains(out, want) {
			t.Errorf("the journal has no %q:\n%s", want, out)
		}
	}
}

func TestApplyJournalsATimeout(t *testing.T) {
	f := newApplyFixture(t, "new binary", "new binary")
	log, buf := journal()
	u := &updater{dir: f.dir, version: "0.1.0", log: log}
	ctx, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
	defer cancel()
	time.Sleep(time.Millisecond)
	if err := u.Apply(ctx, "0.2.0", f.key); err == nil {
		t.Fatal("an expired context must stop the update")
	}
	if out := buf.String(); !strings.Contains(out, "timed_out=true") || !strings.Contains(out, "stage=download") {
		t.Fatalf("the journal must say that the download timed out:\n%s", out)
	}
}

func TestApplyJournalsARefusal(t *testing.T) {
	f := newApplyFixture(t, "new binary", "new binary")
	log, buf := journal()
	u := &updater{dir: f.dir, version: "0.3.0", log: log}
	if err := u.Apply(context.Background(), "0.2.0", f.key); err == nil {
		t.Fatal("a downgrade must be refused")
	}
	if out := buf.String(); !strings.Contains(out, "update: refused") || !strings.Contains(out, "refusing to downgrade") {
		t.Fatalf("the refusal is not in the journal:\n%s", out)
	}
}

// slowReader gives a byte now and then, so the download journal has time to write its lines.
type slowReader struct{ left int }

func (s *slowReader) Read(b []byte) (int, error) {
	if s.left == 0 {
		return 0, io.EOF
	}
	time.Sleep(15 * time.Millisecond)
	s.left--
	b[0] = 'x'
	return 1, nil
}

func TestDownloadJournalShowsTheProgress(t *testing.T) {
	old := downloadLogEvery
	downloadLogEvery = 10 * time.Millisecond
	defer func() { downloadLogEvery = old }()
	log, buf := journal()
	u := &updater{log: log}
	pr := &progressReader{r: &slowReader{left: 12}, size: 12}
	stop := make(chan struct{})
	started := time.Now()
	done := make(chan struct{})
	go func() { u.watchDownload(pr, started, stop); close(done) }()
	if _, err := io.Copy(io.Discard, pr); err != nil {
		t.Fatal(err)
	}
	close(stop)
	<-done // the journal is read only after the last line was written
	out := buf.String()
	if !strings.Contains(out, "update: downloading") || !strings.Contains(out, "percent=") || !strings.Contains(out, "idle_s=") {
		t.Fatalf("the progress is not in the journal:\n%s", out)
	}
}

func TestUpdateHasALongerLimitThanOtherCommands(t *testing.T) {
	var got = map[string]time.Duration{}
	grab := func(name string) CmdFunc {
		return func(ctx context.Context, _ json.RawMessage, _ bool) (any, error) {
			d, ok := ctx.Deadline()
			if !ok {
				t.Errorf("%s has no limit", name)
			}
			got[name] = time.Until(d)
			return nil, nil
		}
	}
	r := &Registry{cmds: map[string]CmdFunc{"update.apply": grab("update"), "disk.state": grab("other")}}
	r.HandleCmd(context.Background(), protocol.Cmd{ID: "1", Module: "update", Action: "apply"})
	r.HandleCmd(context.Background(), protocol.Cmd{ID: "2", Module: "disk", Action: "state"})
	if got["update"] < 9*time.Minute || got["update"] > protocol.AgentUpdateTimeout {
		t.Errorf("the update limit is %v, want about %v", got["update"], protocol.AgentUpdateTimeout)
	}
	if got["other"] < time.Minute || got["other"] > defaultCmdTimeout {
		t.Errorf("the limit of another command is %v, want about %v", got["other"], defaultCmdTimeout)
	}
}
