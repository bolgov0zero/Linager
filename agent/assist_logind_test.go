package agent

import "testing"

func TestParseLogindSession(t *testing.T) {
	text := "# This is private data. Do not parse.\nUID=1000\nUSER=bolgov\nACTIVE=1\nSTATE=active\nTYPE=wayland\nCLASS=user\nSEAT=seat0\nREMOTE=0\n"
	s, ok := parseLogindSession("2", text)
	if !ok || s.ID != "2" || s.UID != 1000 || s.User != "bolgov" || !s.Active || s.Type != "wayland" || s.Class != "user" || s.Seat != "seat0" || s.Remote {
		t.Fatalf("got %+v ok=%v", s, ok)
	}
	if !s.supported() {
		t.Error("a Wayland session of a person on seat0 is supported")
	}
	if _, ok := parseLogindSession("3", "STATE=online\n"); ok {
		t.Error("a file without a user is not a session")
	}
}

func TestConsoleSession(t *testing.T) {
	list := []logindSession{
		{ID: "1", UID: 120, User: "gdm", Active: true, Type: "wayland", Class: "greeter", Seat: "seat0"},
		{ID: "4", UID: 1000, User: "a", Active: true, Type: "tty", Class: "user", Seat: "seat0"},
		{ID: "5", UID: 1000, User: "a", Active: true, Type: "wayland", Class: "user", Remote: true},
		{ID: "6", UID: 1001, User: "b", Active: false, Type: "wayland", Class: "user", Seat: "seat0"},
		{ID: "7", UID: 1002, User: "c", Active: true, Type: "x11", Class: "user", Seat: "seat0"},
	}
	s, ok := consoleSession(list)
	if !ok || s.ID != "7" {
		t.Fatalf("the console is the active graphical session of a person on seat0: %+v %v", s, ok)
	}
	if !s.supported() || s.viaCompositor() {
		t.Error("an x11 session is shown below the compositor")
	}
	// nobody is signed in: the sign-in screen is the console
	if g, ok := consoleSession(list[:1]); !ok || g.ID != "1" || !g.supported() {
		t.Errorf("the greeter of GDM is the console while nobody is signed in: %+v %v", g, ok)
	}
	// a tty, a login over the network and a session in the background are not a console
	if _, ok := consoleSession(list[1:4]); ok {
		t.Error("they are not a console")
	}
}

func TestScanToEvdev(t *testing.T) {
	cases := []struct {
		sc   int
		ext  bool
		want int
		ok   bool
	}{
		{0x01, false, 1, true},  // Esc
		{0x1E, false, 30, true}, // A
		{0x39, false, 57, true}, // Space
		{0x58, false, 88, true}, // F12
		{0x48, true, 103, true}, // Up
		{0x1D, true, 97, true},  // right Ctrl
		{0x5B, true, 125, true}, // left Windows key
		{0x53, true, 111, true}, // Delete
		{0x1C, true, 96, true},  // keypad Enter
		{0x00, false, 0, false},
		{0xE1, false, 0, false},
		{0x20, true, 0, false},
	}
	for _, c := range cases {
		got, ok := scanToEvdev(c.sc, c.ext)
		if got != c.want || ok != c.ok {
			t.Errorf("scanToEvdev(%#x, %v) = %d, %v; want %d, %v", c.sc, c.ext, got, ok, c.want, c.ok)
		}
	}
}

func TestParseProcCreds(t *testing.T) {
	status := "Name:\tgnome-shell\nUid:\t60580\t60580\t60580\t60580\nGid:\t60580\t60580\t60580\t60580\nGroups:\t60580 44 \nVmPeak:\t1 kB\n"
	gid, groups, ok := parseProcCreds(status)
	if !ok || gid != 60580 || len(groups) != 2 || groups[0] != 60580 || groups[1] != 44 {
		t.Errorf("got %d %v %v", gid, groups, ok)
	}
	if _, _, ok := parseProcCreds("Name:\tx\n"); ok {
		t.Error("no Gid line, no credentials")
	}
	if _, _, ok := parseProcCreds("Gid:\tabc\n"); ok {
		t.Error("a bad number is no credentials")
	}
}

func TestParseLogindSessionLeader(t *testing.T) {
	s, ok := parseLogindSession("c1", "UID=60580\nUSER=gdm-greeter\nLEADER=2119\nCLASS=greeter\n")
	if !ok || s.Leader != 2119 || s.User != "gdm-greeter" {
		t.Errorf("got %+v %v", s, ok)
	}
}
