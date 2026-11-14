package agent

import (
	"testing"

	"github.com/bolgov0zero/linager/internal/protocol"
)

func TestSampleBufferAckAndLimit(t *testing.T) {
	b := &sampleBuffer{}
	for i := 0; i < maxBuffered+10; i++ {
		b.add(protocol.Sample{TS: int64(i)})
	}
	got := b.snapshot()
	if len(got) != maxBuffered || got[0].TS != 10 {
		t.Fatalf("buffer must keep the newest %d samples, got len=%d first=%d", maxBuffered, len(got), got[0].TS)
	}
	b.ack(5)
	if s := b.snapshot(); len(s) != maxBuffered-5 || s[0].TS != 15 {
		t.Fatal("ack must drop the delivered samples from the front")
	}
	b.ack(100000)
	if len(b.snapshot()) != 0 {
		t.Fatal("over-ack must empty the buffer")
	}
}
