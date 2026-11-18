//go:build !windows

package agent

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func frame(stream byte, text string) []byte {
	h := make([]byte, 8)
	h[0] = stream
	binary.BigEndian.PutUint32(h[4:], uint32(len(text))) //nolint:gosec // test data is tiny
	return append(h, text...)
}

func TestCopyDockerStreamDemux(t *testing.T) {
	var got bytes.Buffer
	sio := &StreamIO{Out: func(b []byte) error { got.Write(b); return nil }}
	in := append(frame(1, "out line\n"), frame(2, "err line\n")...)
	if err := copyDockerStream(bytes.NewReader(in), false, sio); err != nil {
		t.Fatal(err)
	}
	if got.String() != "out line\nerr line\n" {
		t.Fatalf("%q", got.String())
	}
	got.Reset()
	_ = copyDockerStream(bytes.NewReader([]byte("raw tty output")), true, sio)
	if got.String() != "raw tty output" {
		t.Fatalf("tty streams are passed through: %q", got.String())
	}
}

func TestCPUPercent(t *testing.T) {
	var s dStats
	s.CPUStats.CPUUsage.Total, s.PreCPU.CPUUsage.Total = 2000, 1000
	s.CPUStats.System, s.PreCPU.System = 20000, 10000
	s.CPUStats.Online = 4
	if got := cpuPercent(s); got < 39.9 || got > 40.1 {
		t.Fatalf("got %f want 40", got)
	}
	if cpuPercent(dStats{}) != 0 {
		t.Fatal("no data must give 0")
	}
}

func TestDockerNameValidation(t *testing.T) {
	for _, ok := range []string{"web_1", "abc123def456", "my-app.1"} {
		if !containerRe.MatchString(ok) {
			t.Errorf("%s must be valid", ok)
		}
	}
	for _, bad := range []string{"", "-rm", "a b", "../x", "a;b", "$(x)"} {
		if containerRe.MatchString(bad) {
			t.Errorf("%q must be rejected", bad)
		}
	}
	if !imageRe.MatchString("nginx:1.27") || !imageRe.MatchString("ghcr.io/org/app@sha256:abc") || imageRe.MatchString("a b") {
		t.Error("image name validation")
	}
}
