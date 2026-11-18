package server

import "testing"

func TestValidMount(t *testing.T) {
	for _, ok := range []string{"/", "/var", "C:", "d:", `C:\`, "C:/"} {
		if !validMount(ok) {
			t.Errorf("%q must be valid", ok)
		}
	}
	for _, bad := range []string{"", "var", "C", "1:", `C:\Users`, "CC:", "::"} {
		if validMount(bad) {
			t.Errorf("%q must be invalid", bad)
		}
	}
}
