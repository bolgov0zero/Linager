package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestParseFwLogLine(t *testing.T) {
	line := "1731666000.123456 web-01 kernel: lg:input:3f2a:D IN=eth0 OUT= MAC=aa:bb SRC=203.0.113.77 DST=10.20.0.11 LEN=60 TOS=0x00 PREC=0x00 TTL=52 ID=1 DF PROTO=TCP SPT=51234 DPT=22 WINDOW=64240 RES=0x00 SYN URGP=0"
	ev, ok := parseFwLogLine(line)
	if !ok || ev.Chain != "input" || ev.Rule != "3f2a" || ev.Action != "drop" || ev.Src != "203.0.113.77" || ev.Dst != "10.20.0.11" ||
		ev.Proto != "TCP" || ev.DPort != 22 || ev.SPort != 51234 || ev.Iface != "eth0" || ev.Len != 60 || ev.Flags != "SYN" || ev.TS != 1731666000 {
		t.Fatalf("%+v %v", ev, ok)
	}
	v6, ok := parseFwLogLine("1731666001.5 h kernel: lg:forward:00ab:A IN=eth0 OUT=eth1 SRC=2001:db8::5 DST=2001:db8::1 LEN=80 PROTO=ICMPv6 TYPE=128 CODE=0")
	if !ok || v6.Src != "2001:db8::5" || v6.Proto != "ICMP" || v6.Action != "accept" || v6.Chain != "forward" {
		t.Fatalf("%+v %v", v6, ok)
	}
	for _, bad := range []string{
		"1731666000 h kernel: martian source 10.0.0.1 from 10.0.0.2",
		"1731666000 h kernel: lg:other:3f2a:D SRC=1.2.3.4 PROTO=TCP",
		"x h kernel: lg:input:3f2a:D SRC=1.2.3.4 PROTO=TCP",
		"1731666000 h kernel: lg:input:3f2a:D IN=eth0",
	} {
		if _, ok := parseFwLogLine(bad); ok {
			t.Errorf("%q must be ignored", bad)
		}
	}
}

func TestFwLogCmd(t *testing.T) {
	now := time.Now().Unix()
	line := func(ts int64, chain, id, act, src string, port int) string {
		return strings.Join([]string{itoa(ts) + ".0", "h", "kernel:", "lg:" + chain + ":" + id + ":" + act, "IN=eth0", "SRC=" + src, "DST=10.0.0.1", "LEN=40", "PROTO=TCP", "SPT=1000", "DPT=" + itoa(int64(port)), "SYN"}, " ")
	}
	oldJ, oldA := fwJournal, fwJournalAvailable
	t.Cleanup(func() { fwJournal, fwJournalAvailable = oldJ, oldA; fwLogMemo = fwLogCache{} })
	fwLogMemo = fwLogCache{}
	fwJournalAvailable = func() bool { return true }
	fwJournal = func(_ context.Context, on func(string) bool) error {
		for _, l := range []string{
			line(now-3600*24*9, "input", "aaaa", "D", "198.51.100.1", 22), // older than the window
			line(now-600, "input", "aaaa", "D", "198.51.100.2", 22),
			"junk line",
			line(now-60, "forward", "bbbb", "A", "198.51.100.3", 443),
		} {
			if !on(l) {
				break
			}
		}
		return nil
	}
	m := &fwModule{}
	res, err := m.logCmd(context.Background(), nil, false)
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Available bool
		Events    []FwLogEvent
		Total     int
	}
	if err := json.Unmarshal(res.(json.RawMessage), &got); err != nil {
		t.Fatal(err)
	}
	if !got.Available || got.Total != 2 || len(got.Events) != 2 || got.Events[0].Src != "198.51.100.3" || got.Events[1].Src != "198.51.100.2" {
		t.Fatalf("newest first, the old event dropped: %+v", got)
	}
	fwJournalAvailable = func() bool { return false }
	fwLogMemo = fwLogCache{}
	res, _ = m.logCmd(context.Background(), nil, false)
	if res.(map[string]any)["available"] != false {
		t.Fatalf("no journalctl: %v", res)
	}
}
