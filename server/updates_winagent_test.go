package server

import "testing"

func TestWindowsByHand(t *testing.T) {
	cases := []struct {
		os, ver string
		has     bool
		want    bool
	}{
		{"Windows Server 2022 Standard (21H2, build 20348)", "v0.3.0-beta.3", true, true}, // before self-update: a new MSI
		{"Windows 11 Pro", "v0.3.0-beta.4", true, false},                                  // updates from the panel
		{"Windows 11 Pro", "v0.3.1", true, false},                                         // newer than the first one
		{"Windows 11 Pro", "v0.3.0-beta.4", false, true},                                  // the release has no Windows file
		{"Debian GNU/Linux 12 (bookworm)", "v0.2.9", true, false},                         // Linux is never by hand
		{"Debian GNU/Linux 12 (bookworm)", "v0.3.0-beta.1", false, false},
	}
	for _, c := range cases {
		if got := windowsByHand(c.os, c.ver, c.has); got != c.want {
			t.Errorf("%q %s has=%v: got %v, want %v", c.os, c.ver, c.has, got, c.want)
		}
	}
}
