package version

import "testing"

func TestString(t *testing.T) {
	if got := String(); got != "dev (none, unknown)" {
		t.Fatalf("unexpected version string: %q", got)
	}
}
