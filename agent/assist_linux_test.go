//go:build linux

package agent

import (
	"strings"
	"testing"
)

func TestTailWriterKeepsTheEnd(t *testing.T) {
	w := &tailWriter{max: 10}
	_, _ = w.Write([]byte("error: a long\nmessage "))
	_, _ = w.Write([]byte("that ends here\n"))
	if got := w.String(); !strings.HasSuffix(got, "ends here") || len(got) > 10 {
		t.Errorf("got %q", got)
	}
	if got := (&tailWriter{max: 10}).String(); got != "" {
		t.Errorf("nothing written: %q", got)
	}
}
