//go:build linux

package agent

import (
	"strings"
	"testing"
	"unsafe"
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

func TestKernelStructSizes(t *testing.T) {
	// the sizes are part of the ioctl numbers: a wrong one is a call the kernel refuses
	if got := unsafe.Sizeof(drmModeCardRes{}); got != 64 {
		t.Errorf("drm_mode_card_res is 64 bytes, not %d", got)
	}
	if got := unsafe.Sizeof(drmModeModeInfo{}); got != 68 {
		t.Errorf("drm_mode_modeinfo is 68 bytes, not %d", got)
	}
	if got := unsafe.Sizeof(drmModeCrtc{}); got != 104 {
		t.Errorf("drm_mode_crtc is 104 bytes, not %d", got)
	}
	if got := unsafe.Sizeof(drmModeFBCmd2{}); got != 104 {
		t.Errorf("drm_mode_fb_cmd2 is 104 bytes, not %d", got)
	}
	if got := unsafe.Sizeof(drmPrimeHandle{}); got != 12 {
		t.Errorf("drm_prime_handle is 12 bytes, not %d", got)
	}
	if got := unsafe.Sizeof(drmMapDumb{}); got != 16 {
		t.Errorf("drm_mode_map_dumb is 16 bytes, not %d", got)
	}
	if got := unsafe.Sizeof(uinputSetup{}); got != 92 {
		t.Errorf("uinput_setup is 92 bytes, not %d", got)
	}
	if got := unsafe.Sizeof(uinputAbsSetup{}); got != 28 {
		t.Errorf("uinput_abs_setup is 28 bytes, not %d", got)
	}
	// request numbers known from the headers: DRM_IOCTL_MODE_GETRESOURCES, GETCRTC, GETFB2, UI_DEV_CREATE
	if drmIOWR(0xA0, 64) != 0xC04064A0 || drmIOWR(0xA1, 104) != 0xC06864A1 || drmIOWR(0xCE, 104) != 0xC06864CE || uiIO(1) != 0x5501 {
		t.Errorf("ioctl numbers: %#x %#x %#x %#x", drmIOWR(0xA0, 64), drmIOWR(0xA1, 104), drmIOWR(0xCE, 104), uiIO(1))
	}
}
