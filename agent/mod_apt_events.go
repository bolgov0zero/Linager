//go:build !windows

package agent

import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
)

// aptMark starts an event line in the apt stream: aptMark + JSON + "\n". The browser cuts these lines
// out of the console text and uses them for the per-package statuses and the progress bar. Events are
// kept in the job (not in the size-limited text log), so a reconnecting browser gets all of them.
const aptMark = "\x1e"

// Package states reported to the browser.
const (
	aptQueued  = "queued"
	aptRunning = "running"
	aptDone    = "done"
	aptError   = "error"
	aptSkipped = "skipped" // never reached because apt stopped on an earlier error
)

var aptRank = map[string]int{aptQueued: 0, aptRunning: 1, aptDone: 2, aptError: 2, aptSkipped: 2}

// aptProg is the overall progress of the current phase: "download" or "install".
type aptProg struct {
	T     string `json:"t"`
	Stage string `json:"stage"`
	Pct   int    `json:"pct"`
	Cur   int    `json:"cur,omitempty"`
	Total int    `json:"total,omitempty"`
}

var fileOfRe = regexp.MustCompile(`file (\d+) of (\d+)`)

// aptTracker turns the machine-readable lines of `APT::Status-Fd` into job events. It is used from
// one goroutine at a time: first the starter (queue), then the reader, then the waiter (finish).
type aptTracker struct {
	job   *aptJob
	state map[string]string
	order []string
}

func newAptTracker(job *aptJob) *aptTracker {
	return &aptTracker{job: job, state: map[string]string{}}
}

// add puts packages that are not tracked yet into the queue.
func (t *aptTracker) add(pk []AptPackage) {
	var fresh []AptPackage
	for _, p := range pk {
		if _, ok := t.state[p.Name]; ok {
			continue
		}
		t.state[p.Name] = aptQueued
		t.order = append(t.order, p.Name)
		fresh = append(fresh, p)
	}
	if len(fresh) > 0 {
		t.job.emit(map[string]any{"t": "queue", "pkgs": fresh})
	}
}

// set moves a package forward; a state never goes back (apt reports the same package several times).
func (t *aptTracker) set(name, st, msg string) {
	cur := t.state[name]
	if cur == st || aptRank[cur] == 2 || aptRank[st] < aptRank[cur] {
		return
	}
	t.state[name] = st
	ev := map[string]any{"t": "pkg", "name": name, "state": st}
	if msg != "" {
		ev["msg"] = msg
	}
	t.job.emit(ev)
}

// resolve finds the tracked name for the one apt reports ("libc6:amd64" -> "libc6"). A package the
// simulation did not predict (a new dependency) is added to the queue on the fly.
func (t *aptTracker) resolve(name string) string {
	if _, ok := t.state[name]; ok {
		return name
	}
	if i := strings.IndexByte(name, ':'); i > 0 {
		if _, ok := t.state[name[:i]]; ok {
			return name[:i]
		}
	}
	t.add([]AptPackage{{Name: name}})
	return name
}

func parsePct(s string) int {
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}
	return max(0, min(100, int(f)))
}

// line handles one line of the status descriptor, e.g. "pmstatus:nginx:42.5:Unpacking nginx (1.2)".
func (t *aptTracker) line(s string) {
	f := strings.SplitN(strings.TrimSpace(s), ":", 4)
	if len(f) < 4 {
		return
	}
	switch f[0] {
	case "pmstatus":
		if f[1] == "dpkg-exec" {
			return
		}
		msg := f[3]
		if strings.HasPrefix(msg, "Remov") || strings.HasPrefix(msg, "Preparing for removal") {
			return
		}
		t.job.setProgress(aptProg{T: "progress", Stage: "install", Pct: parsePct(f[2])})
		name := t.resolve(f[1])
		if strings.HasPrefix(msg, "Installed ") {
			t.set(name, aptDone, "")
		} else {
			t.set(name, aptRunning, "")
		}
	case "pmerror":
		t.set(t.resolve(f[1]), aptError, f[3])
	case "dlstatus":
		p := aptProg{T: "progress", Stage: "download", Pct: parsePct(f[2])}
		if m := fileOfRe.FindStringSubmatch(f[3]); m != nil {
			p.Cur, _ = strconv.Atoi(m[1])
			p.Total, _ = strconv.Atoi(m[2])
		}
		t.job.setProgress(p)
	}
}

// finish settles every package that is still open once apt has exited.
func (t *aptTracker) finish(failed bool) {
	for _, name := range t.order {
		switch t.state[name] {
		case aptQueued:
			if failed {
				t.set(name, aptSkipped, "")
			} else {
				t.set(name, aptDone, "")
			}
		case aptRunning:
			if failed {
				t.set(name, aptError, "")
			} else {
				t.set(name, aptDone, "")
			}
		}
	}
}

// emit stores one event line for the streams of this job.
func (j *aptJob) emit(ev any) {
	b, err := json.Marshal(ev)
	if err != nil {
		return
	}
	line := append(append([]byte(aptMark), b...), '\n')
	j.mu.Lock()
	j.events = append(j.events, line)
	j.cond.Broadcast()
	j.mu.Unlock()
}

// setProgress keeps only the latest progress; streams send it when it changed.
func (j *aptJob) setProgress(p aptProg) {
	j.mu.Lock()
	if j.prog != p {
		j.prog = p
		j.progVer++
		j.cond.Broadcast()
	}
	j.mu.Unlock()
}
