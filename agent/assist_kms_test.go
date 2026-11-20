package agent

import (
	"bytes"
	"errors"
	"testing"
	"time"
)

func TestCopyRegionXRGB(t *testing.T) {
	// a 3x2 framebuffer with 16 bytes a row (4 bytes of padding), the pixel (1,1) is blue 10, green 20, red 30
	pitch := 16
	src := make([]byte, 2*pitch)
	copy(src[pitch+4:], []byte{10, 20, 30, 0})
	dst := make([]byte, 2*2*4)
	if err := copyRegion(dst, src, pitch, drmFormatXRGB8888, 1, 0, 2, 2); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(dst[8:12], []byte{10, 20, 30, 255}) {
		t.Errorf("the pixel moved to (0,1) of the region with the alpha set: % x", dst)
	}
}

func TestCopyRegionOtherFormats(t *testing.T) {
	dst := make([]byte, 4)
	// XBGR8888 is R, G, B in memory
	if err := copyRegion(dst, []byte{30, 20, 10, 0}, 4, drmFormatXBGR8888, 0, 0, 1, 1); err != nil || !bytes.Equal(dst, []byte{10, 20, 30, 255}) {
		t.Errorf("XBGR: % x %v", dst, err)
	}
	// RGB565 white and red
	if err := copyRegion(dst, []byte{0xff, 0xff}, 2, drmFormatRGB565, 0, 0, 1, 1); err != nil || !bytes.Equal(dst, []byte{255, 255, 255, 255}) {
		t.Errorf("565 white: % x %v", dst, err)
	}
	if err := copyRegion(dst, []byte{0x00, 0xf8}, 2, drmFormatRGB565, 0, 0, 1, 1); err != nil || !bytes.Equal(dst, []byte{0, 0, 255, 255}) {
		t.Errorf("565 red: % x %v", dst, err)
	}
}

func TestCopyRegionRefusesWhatDoesNotFit(t *testing.T) {
	dst := make([]byte, 16)
	if copyRegion(dst, make([]byte, 8), 8, drmFormatXRGB8888, 0, 0, 2, 2) == nil {
		t.Error("a framebuffer too small for the region")
	}
	if copyRegion(dst, make([]byte, 64), 16, drmFormatXRGB8888, 3, 0, 2, 2) == nil {
		t.Error("a region that sticks out of the row")
	}
	if err := copyRegion(dst, make([]byte, 64), 16, 0x30335258, 0, 0, 2, 2); err == nil {
		t.Error("an unknown format")
	}
}

func TestFourcc(t *testing.T) {
	if got := fourcc(drmFormatXRGB8888); got != "XR24" {
		t.Errorf("got %q", got)
	}
	if got := fourcc(0x00000001); got != "?\x00\x00\x00"[:1]+"???" {
		t.Errorf("non-printing bytes are shown as ?: %q", got)
	}
}

func TestInputEvent(t *testing.T) {
	e := inputEvent(evKey, 30, 1)
	if len(e) != inputEvLen || e[16] != evKey || e[18] != 30 || e[20] != 1 {
		t.Errorf("% x", e)
	}
	if e := inputEvent(evRel, relWheel, -1); e[20] != 0xff || e[23] != 0xff {
		t.Errorf("a negative value keeps its bits: % x", e)
	}
}

func TestScaleAbs(t *testing.T) {
	if scaleAbs(0, 1920) != 0 || scaleAbs(1919, 1920) != absMax || scaleAbs(5000, 1920) != absMax || scaleAbs(-3, 1920) != 0 {
		t.Error("the ends of the screen are the ends of the range, and what is outside is held at them")
	}
	if got := scaleAbs(960, 1920); got < 32000 || got > 33000 {
		t.Errorf("the middle: %d", got)
	}
	if scaleAbs(10, 0) != 0 {
		t.Error("no screen")
	}
}

func TestWaitForScreenWakesAndTellsTheBrowser(t *testing.T) {
	var says []bool
	nudges, looks := 0, 0
	// the screen lights up on the third look
	err := waitForScreen(func() bool { looks++; return looks >= 3 }, func() { nudges++ }, func(on bool) { says = append(says, on) },
		time.Second, time.Hour, time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if nudges != 1 {
		t.Errorf("nudges = %d, want 1 (the repeat is an hour away)", nudges)
	}
	if len(says) != 2 || !says[0] || says[1] {
		t.Errorf("the browser is told: waking, then awake; got %v", says)
	}
}

func TestWaitForScreenGivesUpAndRepeatsTheNudge(t *testing.T) {
	nudges := 0
	err := waitForScreen(func() bool { return false }, func() { nudges++ }, nil, 60*time.Millisecond, 10*time.Millisecond, time.Millisecond)
	if !errors.Is(err, errScreenStaysOff) {
		t.Fatalf("err = %v", err)
	}
	if nudges < 3 {
		t.Errorf("nudges = %d, want the first and some repeats", nudges)
	}
}
