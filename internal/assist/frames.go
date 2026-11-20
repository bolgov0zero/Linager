// Package assist is the wire format of the Assistant: the view and control of a Windows session (the console or
// another user's remote desktop session) in the panel. The helper process in the session produces frames, the panel
// forwards them to the browser, the browser sends input back. Everything here is plain Go, so it is tested on every
// platform; the capture and the input are in the Windows part of the agent.
//
// On the agent stream and on the pipe to the helper a frame is: 4 bytes of length (big endian, the type byte and the
// payload), 1 byte of type, the payload. On the WebSocket to the browser one message is one frame without the length:
// the type byte and the payload.
package assist

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// Frame types. Frames up to 15 go from the helper to the browser, from 16 on the other way.
const (
	TypeHello    byte = 1  // JSON Hello: the screen size, sent first and after every change of it
	TypeTiles    byte = 2  // packed tiles of changed JPEG pictures, see PackTiles
	TypeStats    byte = 3  // JSON Stats, about once a second
	TypeEnd      byte = 4  // JSON End: the helper stops and says why
	TypeClip     byte = 5  // JSON Clip: the text of the clipboard of the computer, the answer to TypeClipGet
	TypeWaking   byte = 6  // JSON Waking: the screen of the computer is off and is being woken, or it is awake again
	TypeTyped    byte = 7  // JSON Typed: the answer to TypeType (it was typed, or why it was refused)
	TypeInput    byte = 16 // JSON Input: mouse and keys, from the browser
	TypeSettings byte = 17 // JSON Settings, from the panel
	TypeClipSet  byte = 18 // JSON Clip: put this text on the clipboard of the computer
	TypeClipGet  byte = 19 // no payload: send the text of the clipboard of the computer
	TypeMonitor  byte = 20 // JSON MonitorSel: show this monitor
	TypePause    byte = 21 // JSON Pause: stop sending pictures (the tab is hidden) or go on
	TypeType     byte = 22 // JSON Typing: type this text on the sign-in or the UAC screen (Windows only)
)

// MaxFrame is the largest frame accepted: a full 4K screen in poor JPEG is far below it.
const MaxFrame = 16 << 20

// ErrFrameTooBig is returned for a length that is zero or above MaxFrame.
var ErrFrameTooBig = errors.New("assist: bad frame length")

// Frame is one message.
type Frame struct {
	Type byte
	Data []byte
}

// AppendFrame appends the stream form of a frame to b.
func AppendFrame(b []byte, typ byte, data []byte) []byte {
	var h [5]byte
	binary.BigEndian.PutUint32(h[:4], uint32(len(data)+1)) //nolint:gosec // frames are far below 4 GiB
	h[4] = typ
	b = append(b, h[:]...)
	return append(b, data...)
}

// WriteFrame writes one frame to w in a single Write, so frames from one writer never interleave.
func WriteFrame(w io.Writer, typ byte, data []byte) error {
	if len(data)+1 > MaxFrame {
		return ErrFrameTooBig
	}
	_, err := w.Write(AppendFrame(make([]byte, 0, len(data)+5), typ, data))
	return err
}

// WriteJSON writes a frame whose payload is the JSON form of v.
func WriteJSON(w io.Writer, typ byte, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return WriteFrame(w, typ, b)
}

// ReadFrame reads one frame from a byte stream.
func ReadFrame(r io.Reader) (Frame, error) {
	var h [4]byte
	if _, err := io.ReadFull(r, h[:]); err != nil {
		return Frame{}, err
	}
	n := binary.BigEndian.Uint32(h[:])
	if n == 0 || n > MaxFrame {
		return Frame{}, ErrFrameTooBig
	}
	buf := make([]byte, n)
	if _, err := io.ReadFull(r, buf); err != nil {
		return Frame{}, err
	}
	return Frame{Type: buf[0], Data: buf[1:]}, nil
}

// Reassembler cuts frames out of chunks of a stream that do not follow frame boundaries.
type Reassembler struct{ buf []byte }

// Push adds a chunk and returns the frames it completed.
func (r *Reassembler) Push(chunk []byte) ([]Frame, error) {
	r.buf = append(r.buf, chunk...)
	var out []Frame
	for len(r.buf) >= 4 {
		n := binary.BigEndian.Uint32(r.buf[:4])
		if n == 0 || n > MaxFrame {
			return out, ErrFrameTooBig
		}
		if uint64(len(r.buf)) < 4+uint64(n) {
			break
		}
		body := r.buf[4 : 4+n]
		out = append(out, Frame{Type: body[0], Data: append([]byte(nil), body[1:]...)})
		r.buf = r.buf[4+n:]
	}
	if len(r.buf) == 0 {
		r.buf = nil // do not hold the memory of a large frame
	}
	return out, nil
}

// Monitor is one display of the computer, in the pixels of the whole desktop.
type Monitor struct {
	X       int  `json:"x"`
	Y       int  `json:"y"`
	W       int  `json:"w"`
	H       int  `json:"h"`
	Primary bool `json:"primary,omitempty"`
}

// Hello tells the browser what it will see: the size of the picture, the displays and which one is shown.
type Hello struct {
	Width    int       `json:"w"`
	Height   int       `json:"h"`
	Session  int       `json:"session"`
	User     string    `json:"user,omitempty"`
	Monitors []Monitor `json:"monitors,omitempty"` // the primary one first
	Monitor  int       `json:"monitor"`            // the index shown, -1 for all of them as one picture
	Platform string    `json:"platform,omitempty"` // "linux" for a Linux computer (the buttons that are Windows keys are not offered), empty for Windows
}

// Stats is what the helper measured in the last second.
type Stats struct {
	FPS     float64 `json:"fps"`
	Kbps    int     `json:"kbps"`
	Quality int     `json:"q"`
	Desktop string  `json:"desktop,omitempty"` // "default", or "winlogon" while the screen is locked, a sign-in or a UAC prompt is shown
}

// End is the reason the helper stops.
type End struct {
	Reason string `json:"reason"`
	// Reconnect asks the browser to connect again: the helper ended because another way of reading the screen suits the
	// computer now (the desktop opened after the sign-in, the screen was locked, the person signed out).
	Reconnect bool `json:"reconnect,omitempty"`
}

// Waking tells the browser that the screen of the computer is off (it went dark by itself) and the helper is waking it:
// the browser shows a plain "connecting" stub until the picture comes. On=false says the screen is awake.
type Waking struct {
	On bool `json:"on"`
}

// Tile is a rectangle of the screen and its JPEG picture.
type Tile struct {
	X, Y, W, H int
	Data       []byte
}

// PackTiles is the payload of a TypeTiles frame: a 16-bit count, then per tile x, y, w, h (16 bits each), the length of
// the picture (32 bits) and the picture. The numbers are big endian.
func PackTiles(ts []Tile) []byte {
	size := 2
	for _, t := range ts {
		size += 12 + len(t.Data)
	}
	b := make([]byte, 2, size)
	binary.BigEndian.PutUint16(b, uint16(len(ts))) //nolint:gosec // a frame holds far fewer tiles than 65535
	for _, t := range ts {
		var h [12]byte
		binary.BigEndian.PutUint16(h[0:], uint16(t.X)) //nolint:gosec // the screen is below 65536 pixels
		binary.BigEndian.PutUint16(h[2:], uint16(t.Y)) //nolint:gosec // same
		binary.BigEndian.PutUint16(h[4:], uint16(t.W)) //nolint:gosec // same
		binary.BigEndian.PutUint16(h[6:], uint16(t.H)) //nolint:gosec // same
		binary.BigEndian.PutUint32(h[8:], uint32(len(t.Data)))
		b = append(b, h[:]...)
		b = append(b, t.Data...)
	}
	return b
}

// UnpackTiles is the reverse of PackTiles; the pictures are copied.
func UnpackTiles(b []byte) ([]Tile, error) {
	if len(b) < 2 {
		return nil, errors.New("assist: short tiles")
	}
	n := int(binary.BigEndian.Uint16(b))
	b = b[2:]
	out := make([]Tile, 0, n)
	for i := 0; i < n; i++ {
		if len(b) < 12 {
			return nil, fmt.Errorf("assist: tile %d is cut", i)
		}
		t := Tile{X: int(binary.BigEndian.Uint16(b[0:])), Y: int(binary.BigEndian.Uint16(b[2:])),
			W: int(binary.BigEndian.Uint16(b[4:])), H: int(binary.BigEndian.Uint16(b[6:]))}
		l := binary.BigEndian.Uint32(b[8:])
		b = b[12:]
		if uint64(l) > uint64(len(b)) {
			return nil, fmt.Errorf("assist: tile %d picture is cut", i)
		}
		t.Data = append([]byte(nil), b[:l]...)
		b = b[l:]
		out = append(out, t)
	}
	if len(b) != 0 {
		return nil, errors.New("assist: bytes after the last tile")
	}
	return out, nil
}
