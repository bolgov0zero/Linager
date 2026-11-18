//go:build !windows

package agent

import (
	"context"
	"encoding/json"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// The firewall journal: what the rules that are marked for logging matched. A marked rule carries
// `log prefix "lg:<chain>:<id>:<A|D|R> "` (the panel writes it); the kernel logs the packet with that prefix,
// and the agent reads it back from the kernel journal. Nothing else of the kernel log is returned.

const (
	fwLogDays   = 7
	fwLogMax    = 6000 // events sent to the panel, newest first
	fwLogTTL    = 15 * time.Second
	fwLogLines  = 3_000_000
	fwLogWindow = fwLogDays * 24 * time.Hour
)

// FwLogEvent is one packet a marked rule matched.
type FwLogEvent struct {
	TS     int64  `json:"time"`
	Chain  string `json:"chain"`
	Rule   string `json:"rule"`   // id of the rule from its prefix
	Action string `json:"action"` // accept, drop or reject
	Iface  string `json:"iface"`
	Src    string `json:"src"`
	Dst    string `json:"dst"`
	Proto  string `json:"proto"`
	SPort  int    `json:"sport,omitempty"`
	DPort  int    `json:"dport,omitempty"`
	Len    int    `json:"len,omitempty"`
	Flags  string `json:"flags,omitempty"`
}

var (
	fwLogPrefixRe = regexp.MustCompile(`lg:(input|forward|output):([0-9a-f]{4,8}):([ADR]) `)
	fwLogFieldRe  = regexp.MustCompile(`\b(IN|SRC|DST|LEN|PROTO|SPT|DPT)=(\S*)`)
	fwLogFlagsRe  = regexp.MustCompile(`\b(SYN|ACK|FIN|RST|PSH|URG)\b`)
)

// parseFwLogLine reads one line of `journalctl -k -o short-unix`.
func parseFwLogLine(line string) (FwLogEvent, bool) {
	m := fwLogPrefixRe.FindStringSubmatchIndex(line)
	if m == nil {
		return FwLogEvent{}, false
	}
	sp := strings.IndexByte(line, ' ')
	if sp <= 0 {
		return FwLogEvent{}, false
	}
	f, err := strconv.ParseFloat(line[:sp], 64)
	if err != nil {
		return FwLogEvent{}, false
	}
	ev := FwLogEvent{TS: int64(f), Chain: line[m[2]:m[3]], Rule: line[m[4]:m[5]]}
	ev.Action = map[string]string{"A": "accept", "D": "drop", "R": "reject"}[line[m[6]:m[7]]]
	rest := line[m[1]:]
	for _, kv := range fwLogFieldRe.FindAllStringSubmatch(rest, -1) {
		switch kv[1] {
		case "IN":
			ev.Iface = kv[2]
		case "SRC":
			ev.Src = kv[2]
		case "DST":
			ev.Dst = kv[2]
		case "PROTO":
			ev.Proto = strings.ToUpper(kv[2])
		case "LEN":
			ev.Len, _ = strconv.Atoi(kv[2])
		case "SPT":
			ev.SPort, _ = strconv.Atoi(kv[2])
		case "DPT":
			ev.DPort, _ = strconv.Atoi(kv[2])
		}
	}
	if ev.Src == "" || ev.Proto == "" {
		return FwLogEvent{}, false
	}
	if ev.Proto == "ICMPV6" || ev.Proto == "IPV6-ICMP" {
		ev.Proto = "ICMP"
	}
	if ev.Proto == "TCP" {
		if flags := fwLogFlagsRe.FindAllString(rest, -1); len(flags) > 0 {
			ev.Flags = strings.Join(flags, " ")
		}
	}
	return ev, true
}

type fwLogCache struct {
	mu   sync.Mutex
	at   time.Time
	data json.RawMessage
}

var fwLogMemo fwLogCache

// ports and journal access are replaceable in tests
var fwJournal = func(ctx context.Context, onLine func(string) bool) error {
	return streamLines(ctx, "journalctl", []string{"-k", "-o", "short-unix", "-q", "--no-pager", "--since", "7 days ago"}, onLine)
}

var fwJournalAvailable = func() bool {
	_, err := exec.LookPath("journalctl")
	return err == nil
}

// logCmd is "firewall.log": the events of the last seven days, newest first, answered from a short cache.
func (m *fwModule) logCmd(ctx context.Context, _ json.RawMessage, _ bool) (any, error) {
	fwLogMemo.mu.Lock()
	defer fwLogMemo.mu.Unlock()
	if fwLogMemo.data != nil && time.Since(fwLogMemo.at) < fwLogTTL {
		return fwLogMemo.data, nil
	}
	if !fwJournalAvailable() {
		return map[string]any{"available": false, "events": []FwLogEvent{}, "total": 0}, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	cut := time.Now().Add(-fwLogWindow).Unix()
	var events []FwLogEvent
	lines, truncated := 0, false
	if err := fwJournal(ctx, func(line string) bool {
		if lines++; lines > fwLogLines {
			truncated = true
			return false
		}
		if ev, ok := parseFwLogLine(line); ok && ev.TS >= cut {
			events = append(events, ev)
		}
		return true
	}); err != nil {
		return nil, err
	}
	total := len(events)
	// the journal is oldest first: newest first for the panel
	for i, j := 0, len(events)-1; i < j; i, j = i+1, j-1 {
		events[i], events[j] = events[j], events[i]
	}
	if len(events) > fwLogMax {
		events = events[:fwLogMax]
	}
	if events == nil {
		events = []FwLogEvent{}
	}
	b, err := json.Marshal(map[string]any{"available": true, "events": events, "total": total, "truncated": truncated || total > fwLogMax})
	if err != nil {
		return nil, err
	}
	fwLogMemo.at, fwLogMemo.data = time.Now(), b
	return json.RawMessage(b), nil
}
