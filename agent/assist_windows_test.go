//go:build windows

package agent

import (
	"reflect"
	"testing"
	"time"
	"unsafe"

	"github.com/bolgov0zero/linager/internal/assist"
)

// The structures that go to Windows must have the sizes Windows expects, or SendInput and the cursor calls fail in
// ways that are hard to see.
func TestAssistStructSizes(t *testing.T) {
	if unsafe.Sizeof(inputRec{}) != 40 {
		t.Errorf("INPUT must be 40 bytes, got %d", unsafe.Sizeof(inputRec{}))
	}
	if unsafe.Sizeof(mouseInput{}) != 32 || unsafe.Sizeof(keybdInput{}) != 24 {
		t.Errorf("MOUSEINPUT 32 and KEYBDINPUT 24, got %d and %d", unsafe.Sizeof(mouseInput{}), unsafe.Sizeof(keybdInput{}))
	}
	if unsafe.Sizeof(cursorInfo{}) != 24 || unsafe.Sizeof(iconInfo{}) != 32 {
		t.Errorf("CURSORINFO 24 and ICONINFO 32, got %d and %d", unsafe.Sizeof(cursorInfo{}), unsafe.Sizeof(iconInfo{}))
	}
	if unsafe.Sizeof(bitmapInfoHeader{}) != 40 {
		t.Errorf("BITMAPINFOHEADER must be 40 bytes, got %d", unsafe.Sizeof(bitmapInfoHeader{}))
	}
}

func TestEnvBlockRoundTrip(t *testing.T) {
	env := []string{"A=1", "Path=C:\\x;D:\\y", "LINAGER_ASSIST_TOKEN=abc", "Ключ=значение"}
	blk := buildEnvBlock(env)
	got := readEnvBlock(&blk[0])
	if !reflect.DeepEqual(got, env) {
		t.Fatalf("%q != %q", got, env)
	}
	if got := readEnvBlock(nil); got != nil {
		t.Fatalf("a nil block: %q", got)
	}
}

func TestAbsoluteMouse(t *testing.T) {
	if absolute(0, 0, 1920) != 0 || absolute(1919, 0, 1920) != 65535 || absolute(-5, 0, 1920) != 0 || absolute(5000, 0, 1920) != 65535 {
		t.Fatal("the ends of the screen must map to the ends of the range")
	}
	if m := absolute(960, 0, 1920); m < 32000 || m > 33500 {
		t.Fatalf("the middle: %d", m)
	}
	if absolute(10, 0, 1) != 0 {
		t.Fatal("a screen of one pixel")
	}
	// a display to the left of the primary one has negative coordinates: the virtual desktop starts at -1920
	if absolute(-1920, -1920, 3840) != 0 || absolute(1919, -1920, 3840) != 65535 {
		t.Fatal("the virtual desktop is counted from its own left edge")
	}
}

// The agent can list sessions and tell a session that does not exist.
func TestAssistSessions(t *testing.T) {
	for _, s := range listAssistSessions() {
		if s.ID == 0 {
			t.Errorf("the services session must not be listed: %+v", s)
		}
	}
	if _, err := resolveAssistSession("4000000"); err == nil {
		t.Error("an unknown session must be refused")
	}
	if _, err := resolveAssistSession("abc"); err == nil {
		t.Error("a bad session must be refused")
	}
}

// Where the test runs in a desktop session (a developer's machine, the Windows runner) the GDI picture is a real one.
func TestWinHost(t *testing.T) {
	h, err := newWinHost()
	if err != nil {
		t.Skipf("no desktop here: %v", err)
	}
	defer h.release()
	pix, w, ht, err := h.Grab()
	if err != nil {
		t.Skipf("the desktop cannot be read here: %v", err)
	}
	if w <= 0 || ht <= 0 || len(pix) != w*ht*4 {
		t.Fatalf("picture %dx%d, %d bytes", w, ht, len(pix))
	}
	mons, sel := h.Monitors()
	if len(mons) == 0 || sel != 0 || !mons[0].Primary {
		t.Fatalf("displays: %+v, shown %d", mons, sel)
	}
	if h.Desktop() == "" {
		t.Error("the desktop has a name")
	}
	enc := assist.NewEncoder(assist.DefaultSettings())
	if _, err := enc.Encode(pix, w, ht, time.Now()); err != nil {
		t.Fatal(err)
	}
	// the clipboard keeps text, with the line ends of Windows turned back into \n
	if err := h.ClipboardSet("a\nb"); err == nil {
		if got, err := h.ClipboardGet(); err != nil || got != "a\nb" {
			t.Fatalf("clipboard round trip: %q %v", got, err)
		}
	}
}
