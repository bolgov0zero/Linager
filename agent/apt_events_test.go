package agent

import (
	"context"
	"encoding/json"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"
)

// splitAptStream separates event lines from the console text, like the browser does.
func splitAptStream(out string) (events []map[string]any, text string) {
	var tb strings.Builder
	for len(out) > 0 {
		i := strings.Index(out, aptMark)
		if i < 0 {
			tb.WriteString(out)
			break
		}
		tb.WriteString(out[:i])
		out = out[i+1:]
		j := strings.IndexByte(out, '\n')
		if j < 0 {
			break
		}
		var ev map[string]any
		if json.Unmarshal([]byte(out[:j]), &ev) == nil {
			events = append(events, ev)
		}
		out = out[j+1:]
	}
	return events, tb.String()
}

// pkgStates folds the events into the final state of each package, in queue order.
func pkgStates(events []map[string]any) (order []string, state map[string]string) {
	state = map[string]string{}
	for _, ev := range events {
		switch ev["t"] {
		case "queue":
			for _, p := range ev["pkgs"].([]any) {
				name := p.(map[string]any)["name"].(string)
				order = append(order, name)
				state[name] = aptQueued
			}
		case "pkg":
			state[ev["name"].(string)] = ev["state"].(string)
		}
	}
	return order, state
}

func runFakeApt(t *testing.T, script, mode string) (events []map[string]any, text string) {
	t.Helper()
	old := aptCommand
	aptCommand = func(...string) *exec.Cmd {
		return exec.Command("sh", "-c", script) //nolint:gosec // test command
	}
	defer func() { aptCommand = old }()

	var mu sync.Mutex
	var out strings.Builder
	done := make(chan error, 1)
	go func() {
		done <- aptStream(context.Background(), []byte(`{"mode":"`+mode+`"}`), &StreamIO{Out: func(b []byte) error {
			mu.Lock()
			out.Write(b)
			mu.Unlock()
			return nil
		}}, nil)
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the stream did not finish")
	}
	mu.Lock()
	defer mu.Unlock()
	return splitAptStream(out.String())
}

func TestAptTrackerStates(t *testing.T) {
	job := &aptJob{}
	job.cond = sync.NewCond(&job.mu)
	tr := newAptTracker(job)
	tr.add([]AptPackage{{Name: "a"}, {Name: "b"}, {Name: "libc6"}})
	for _, l := range []string{
		"dlstatus:1:40.0:Retrieving file 2 of 5",
		"pmstatus:dpkg-exec:0:Running dpkg",
		"pmstatus:a:10.5:Preparing a (amd64)",
		"pmstatus:a:20:Unpacking a (amd64)",
		"pmstatus:libc6:25:Unpacking libc6:amd64 (amd64)", // architecture suffix
		"pmstatus:newdep:30:Unpacking newdep (amd64)",     // not in the simulation
		"pmstatus:a:90:Installed a (amd64)",
		"pmstatus:a:91:Preparing a (amd64)", // must not move a finished package back
		"pmstatus:old:92:Removing old (amd64)",
		"pmerror:b:95:subprocess returned error exit status 1",
		"garbage",
	} {
		tr.line(l)
	}
	tr.finish(true)
	want := map[string]string{"a": aptDone, "b": aptError, "libc6": aptError, "newdep": aptError}
	order, state := pkgStates(eventsOf(t, job))
	for k, v := range want {
		if state[k] != v {
			t.Errorf("%s: got %q, want %q (order %v)", k, state[k], v, order)
		}
	}
	if _, ok := state["old"]; ok {
		t.Error("a removed package must not be tracked")
	}
	if len(order) != 4 {
		t.Errorf("queue: %v", order)
	}
	// the last pmstatus that counts is "Preparing a" at 91%; pmerror and removals do not move the bar
	if job.prog.Stage != "install" || job.prog.Pct != 91 {
		t.Errorf("progress: %+v", job.prog)
	}
}

func eventsOf(t *testing.T, j *aptJob) []map[string]any {
	t.Helper()
	var ev []map[string]any
	for _, line := range j.events {
		var m map[string]any
		if err := json.Unmarshal(line[1:], &m); err != nil {
			t.Fatal(err)
		}
		ev = append(ev, m)
	}
	return ev
}

func TestAptTrackerFinishWithoutFailure(t *testing.T) {
	job := &aptJob{}
	job.cond = sync.NewCond(&job.mu)
	tr := newAptTracker(job)
	tr.add([]AptPackage{{Name: "a"}, {Name: "b"}})
	tr.line("pmstatus:a:10:Unpacking a (amd64)")
	tr.finish(false)
	_, state := pkgStates(eventsOf(t, job))
	if state["a"] != aptDone || state["b"] != aptDone {
		t.Fatalf("after a successful run everything is done: %v", state)
	}
}

func TestAptStreamSendsEventsAndText(t *testing.T) {
	// fd 3 is the status descriptor the agent reads
	script := `echo "Reading package lists"
echo "pmstatus:nginx:10:Unpacking nginx (1.2)" >&3
echo "Unpacking nginx"
echo "pmstatus:nginx:60:Installed nginx (1.2)" >&3
echo "pmstatus:curl:80:Unpacking curl (8)" >&3
exit 1`
	events, text := runFakeApt(t, script, "update")
	if !strings.Contains(text, "Reading package lists") || !strings.Contains(text, "Unpacking nginx") {
		t.Fatalf("console text: %q", text)
	}
	if strings.Contains(text, aptMark) {
		t.Fatal("event lines must be cut out of the text")
	}
	_, state := pkgStates(events)
	if state["nginx"] != aptDone || state["curl"] != aptError {
		t.Fatalf("states: %v", state)
	}
	var last map[string]any
	for _, ev := range events {
		if ev["t"] == "progress" {
			last = ev
		}
	}
	if last == nil || last["stage"] != "install" {
		t.Fatalf("progress events: %v", events)
	}
}

func TestAptStreamReplaysEventsToLateStream(t *testing.T) {
	// a second stream that attaches while the job runs must see the events sent before it came
	gate := t.TempDir() + "/go"
	old := aptCommand
	aptCommand = func(...string) *exec.Cmd {
		return exec.Command("sh", "-c", `echo "pmstatus:a:5:Unpacking a (1)" >&3; while [ ! -e `+gate+` ]; do sleep 0.05; done; echo "pmstatus:a:90:Installed a (1)" >&3`) //nolint:gosec // test command
	}
	defer func() { aptCommand = old }()

	started := make(chan struct{})
	var once sync.Once
	go func() {
		_ = aptStream(context.Background(), []byte(`{"mode":"update"}`), &StreamIO{Out: func([]byte) error {
			once.Do(func() { close(started) })
			return nil
		}}, nil)
	}()
	<-started
	var mu sync.Mutex
	var out strings.Builder
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = aptStream(context.Background(), []byte(`{"mode":"update"}`), &StreamIO{Out: func(b []byte) error {
			mu.Lock()
			out.Write(b)
			mu.Unlock()
			return nil
		}}, nil)
	}()
	time.Sleep(200 * time.Millisecond)
	if err := exec.Command("touch", gate).Run(); err != nil { //nolint:gosec // test path
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("the attached stream did not finish")
	}
	mu.Lock()
	defer mu.Unlock()
	events, _ := splitAptStream(out.String())
	if _, state := pkgStates(events); state["a"] != aptDone {
		t.Fatalf("late stream must replay the events: %v", events)
	}
}
