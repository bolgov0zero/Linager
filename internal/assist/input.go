package assist

import (
	"encoding/json"
	"errors"
)

// Kinds of Input.T.
const (
	InMove  = "m"   // the pointer moved to X, Y (pixels of the screen)
	InBtn   = "b"   // a mouse button: B 0 left, 1 middle, 2 right, 3 back, 4 forward; Down says pressed or released
	InWheel = "w"   // the wheel: DY and DX in notches, positive is down and right
	InKey   = "k"   // a key by its scan code: Sc, Ext for the extended keys; Down says pressed or released
	InCAD   = "cad" // Ctrl+Alt+Del
)

// Input is one event from the browser.
type Input struct {
	T    string `json:"t"`
	X    int    `json:"x,omitempty"`
	Y    int    `json:"y,omitempty"`
	B    int    `json:"b,omitempty"`
	Down bool   `json:"d,omitempty"`
	DX   int    `json:"dx,omitempty"`
	DY   int    `json:"dy,omitempty"`
	Sc   int    `json:"sc,omitempty"`
	Ext  bool   `json:"ext,omitempty"`
}

// MaxInput is the largest JSON of one Input: the panel drops anything longer.
const MaxInput = 256

// ParseInput reads and checks an Input. The numbers are bounded here, so that nothing a browser sends reaches
// SendInput out of range.
func ParseInput(b []byte) (Input, error) {
	var in Input
	if len(b) > MaxInput {
		return in, errors.New("input is too long")
	}
	if err := json.Unmarshal(b, &in); err != nil {
		return in, err
	}
	switch in.T {
	case InMove:
		if in.X < 0 || in.X > 65535 || in.Y < 0 || in.Y > 65535 {
			return in, errors.New("pointer is out of range")
		}
	case InBtn:
		if in.B < 0 || in.B > 4 {
			return in, errors.New("unknown button")
		}
		if in.X < 0 || in.X > 65535 || in.Y < 0 || in.Y > 65535 {
			return in, errors.New("pointer is out of range")
		}
	case InWheel:
		if in.DX < -50 || in.DX > 50 || in.DY < -50 || in.DY > 50 {
			return in, errors.New("wheel is out of range")
		}
	case InKey:
		if in.Sc < 1 || in.Sc > 0xFF {
			return in, errors.New("unknown scan code")
		}
	case InCAD:
	default:
		return in, errors.New("unknown input")
	}
	return in, nil
}

// MaxClip is the longest text of the clipboard that is carried (bytes of UTF-8).
const MaxClip = 1 << 20

// MaxClipFrame is the largest JSON of a clipboard frame: the text with its escapes.
const MaxClipFrame = 2*MaxClip + 64

// Clip is the payload of TypeClip and TypeClipSet.
type Clip struct {
	Text string `json:"text"`
}

// ParseClip reads and checks a clipboard payload.
func ParseClip(b []byte) (Clip, error) {
	var c Clip
	if len(b) > MaxClipFrame {
		return c, errors.New("clipboard is too long")
	}
	if err := json.Unmarshal(b, &c); err != nil {
		return c, err
	}
	if len(c.Text) > MaxClip {
		return c, errors.New("clipboard is too long")
	}
	return c, nil
}

// MonitorSel is the payload of TypeMonitor: an index of the list of displays, or -1 for all of them.
type MonitorSel struct {
	I int `json:"i"`
}

// ParseMonitor reads and checks a choice of a display.
func ParseMonitor(b []byte) (MonitorSel, error) {
	var m MonitorSel
	if len(b) > 64 {
		return m, errors.New("monitor choice is too long")
	}
	if err := json.Unmarshal(b, &m); err != nil {
		return m, err
	}
	if m.I < -1 || m.I > 15 {
		return m, errors.New("unknown monitor")
	}
	return m, nil
}
