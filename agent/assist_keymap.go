package agent

// Linux key codes (evdev, linux/input-event-codes.h) of the keys the browser sends as scan codes of set 1.
const (
	evKeyKPEnter  = 96
	evKeyRCtrl    = 97
	evKeyKPSlash  = 98
	evKeySysRq    = 99
	evKeyRAlt     = 100
	evKeyHome     = 102
	evKeyUp       = 103
	evKeyPageUp   = 104
	evKeyLeft     = 105
	evKeyRight    = 106
	evKeyEnd      = 107
	evKeyDown     = 108
	evKeyPageDown = 109
	evKeyInsert   = 110
	evKeyDelete   = 111
	evKeyLMeta    = 125
	evKeyRMeta    = 126
	evKeyCompose  = 127
)

// extendedEvdev maps the scan codes after the prefix 0xE0 to evdev key codes.
var extendedEvdev = map[int]int{
	0x1C: evKeyKPEnter, 0x1D: evKeyRCtrl, 0x35: evKeyKPSlash, 0x37: evKeySysRq, 0x38: evKeyRAlt,
	0x47: evKeyHome, 0x48: evKeyUp, 0x49: evKeyPageUp, 0x4B: evKeyLeft, 0x4D: evKeyRight, 0x4F: evKeyEnd,
	0x50: evKeyDown, 0x51: evKeyPageDown, 0x52: evKeyInsert, 0x53: evKeyDelete,
	0x5B: evKeyLMeta, 0x5C: evKeyRMeta, 0x5D: evKeyCompose,
}

// scanToEvdev turns the scan code of a key (set 1, as the Assistant sends it; ext is the prefix 0xE0) into the key code
// of Linux. The plain keys have the same number in both: Esc is 1, the letters and the digits follow the rows of the
// keyboard, F1-F10 are 59-68, NumLock 69, ScrollLock 70, the keypad 71-83, the extra key of the German layout 86, F11
// 87 and F12 88. ok is false for a key that has no code (the Pause key sends a sequence, not a scan code).
func scanToEvdev(sc int, ext bool) (int, bool) {
	if ext {
		c, ok := extendedEvdev[sc]
		return c, ok
	}
	if sc >= 1 && sc <= 88 {
		return sc, true
	}
	return 0, false
}
