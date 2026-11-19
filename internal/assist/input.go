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
