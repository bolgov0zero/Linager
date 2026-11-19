package assist

import (
	"bytes"
	"encoding/json"
	"errors"
	"image"
	"image/jpeg"
	"testing"
	"time"
)

func TestFrameRoundTripAndReassembly(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteFrame(&buf, TypeHello, []byte(`{"w":1}`)); err != nil {
		t.Fatal(err)
	}
	if err := WriteFrame(&buf, TypeTiles, bytes.Repeat([]byte{7}, 100000)); err != nil {
		t.Fatal(err)
	}
	all := buf.Bytes()

	f, err := ReadFrame(bytes.NewReader(all))
	if err != nil || f.Type != TypeHello || string(f.Data) != `{"w":1}` {
		t.Fatalf("read: %+v %v", f, err)
	}

	// the same bytes in awkward chunks give the same two frames
	var r Reassembler
	var got []Frame
	for i := 0; i < len(all); i += 7 {
		fs, err := r.Push(all[i:min(i+7, len(all))])
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, fs...)
	}
	if len(got) != 2 || got[0].Type != TypeHello || got[1].Type != TypeTiles || len(got[1].Data) != 100000 {
		t.Fatalf("reassembly: %d frames", len(got))
	}
}

func TestFrameLimits(t *testing.T) {
	if _, err := ReadFrame(bytes.NewReader([]byte{0, 0, 0, 0})); !errors.Is(err, ErrFrameTooBig) {
		t.Fatalf("zero length: %v", err)
	}
	if _, err := ReadFrame(bytes.NewReader([]byte{0xFF, 0xFF, 0xFF, 0xFF})); !errors.Is(err, ErrFrameTooBig) {
		t.Fatalf("huge length: %v", err)
	}
	var r Reassembler
	if _, err := r.Push([]byte{0x7F, 0, 0, 0, 1}); !errors.Is(err, ErrFrameTooBig) {
		t.Fatalf("huge length in a stream: %v", err)
	}
}

func TestTilesPack(t *testing.T) {
	in := []Tile{{X: 0, Y: 0, W: 64, H: 64, Data: []byte("a")}, {X: 1900, Y: 1000, W: 20, H: 80, Data: nil}}
	out, err := UnpackTiles(PackTiles(in))
	if err != nil || len(out) != 2 || out[1].X != 1900 || out[1].H != 80 || string(out[0].Data) != "a" {
		t.Fatalf("%+v %v", out, err)
	}
	b := PackTiles(in)
	if _, err := UnpackTiles(b[:len(b)-1+0]); err == nil && len(in[1].Data) > 0 {
		t.Fatal("a cut tile must be refused")
	}
	if _, err := UnpackTiles(append(PackTiles(in), 0)); err == nil {
		t.Fatal("bytes after the last tile must be refused")
	}
	if _, err := UnpackTiles([]byte{0, 1, 0, 0}); err == nil {
		t.Fatal("a cut header must be refused")
	}
}

func TestSettingsValidate(t *testing.T) {
	if err := DefaultSettings().Validate(); err != nil {
		t.Fatal(err)
	}
	for _, s := range []Settings{
		{Colors: "32", FPS: 15, Mbit: 4}, {Colors: Colors24, FPS: 0, Mbit: 4}, {Colors: Colors24, FPS: 31, Mbit: 4},
		{Colors: Colors24, FPS: 15, Mbit: 0.1}, {Colors: Colors24, FPS: 15, Mbit: 21},
	} {
		if s.Validate() == nil {
			t.Errorf("%+v must be refused", s)
		}
	}
}

func TestParseInput(t *testing.T) {
	good := []string{`{"t":"m","x":10,"y":20}`, `{"t":"b","b":2,"d":true,"x":1,"y":2}`, `{"t":"w","dy":3}`, `{"t":"k","sc":30,"d":true}`, `{"t":"k","sc":72,"ext":true}`, `{"t":"cad"}`}
	for _, g := range good {
		if _, err := ParseInput([]byte(g)); err != nil {
			t.Errorf("%s: %v", g, err)
		}
	}
	bad := []string{`{"t":"m","x":-1}`, `{"t":"m","x":70000}`, `{"t":"b","b":9}`, `{"t":"w","dy":500}`, `{"t":"k","sc":0}`, `{"t":"k","sc":999}`, `{"t":"x"}`, `not json`, `{"t":"m","x":1,"pad":"` + string(bytes.Repeat([]byte("a"), 300)) + `"}`}
	for _, b := range bad {
		if _, err := ParseInput([]byte(b)); err == nil {
			t.Errorf("%.40s must be refused", b)
		}
	}
}

// screen makes a w*h BGRA picture of one colour.
func screen(w, h int, b, g, r byte) []byte {
	p := make([]byte, w*h*4)
	for i := 0; i < len(p); i += 4 {
		p[i], p[i+1], p[i+2], p[i+3] = b, g, r, 255
	}
	return p
}

func TestEncoderSendsOnlyWhatChanged(t *testing.T) {
	const w, h = 200, 150 // 4 x 3 tiles, the last column and row are partial
	e := NewEncoder(Settings{Colors: Colors24, FPS: 15, NoLimit: true, Mbit: 4})
	now := time.Unix(1000, 0)
	p := screen(w, h, 10, 20, 30)

	tiles, err := e.Encode(p, w, h, now)
	if err != nil || len(tiles) != 3 { // one merged rectangle per tile row
		t.Fatalf("first picture: %d tiles, %v", len(tiles), err)
	}
	for _, tl := range tiles {
		if tl.X != 0 || tl.W != w {
			t.Errorf("a row is one rectangle: %+v", tl)
		}
		img, err := jpeg.Decode(bytes.NewReader(tl.Data))
		if err != nil || img.Bounds().Dx() != tl.W || img.Bounds().Dy() != tl.H {
			t.Fatalf("a tile is a JPEG of its size: %v", err)
		}
	}
	if tiles[2].H != 150-128 {
		t.Errorf("the last row is partial: %d", tiles[2].H)
	}

	if tiles, _ := e.Encode(p, w, h, now.Add(time.Second)); len(tiles) != 0 {
		t.Fatalf("an unchanged screen sends nothing: %d", len(tiles))
	}

	// one pixel in the tile (1,1) changes: one tile of 64x64 comes back
	p[(70*w+70)*4] = 200
	tiles, err = e.Encode(p, w, h, now.Add(2*time.Second))
	if err != nil || len(tiles) != 1 || tiles[0].X != 64 || tiles[0].Y != 64 || tiles[0].W != 64 || tiles[0].H != 64 {
		t.Fatalf("one tile: %+v %v", tiles, err)
	}

	// a new size is a whole new picture
	if tiles, _ := e.Encode(screen(100, 100, 1, 2, 3), 100, 100, now.Add(3*time.Second)); len(tiles) != 2 {
		t.Fatalf("a new size: %d", len(tiles))
	}
	e.Reset()
	if tiles, _ := e.Encode(screen(100, 100, 1, 2, 3), 100, 100, now.Add(4*time.Second)); len(tiles) != 2 {
		t.Fatalf("after a reset: %d", len(tiles))
	}
	if _, err := e.Encode(make([]byte, 10), 100, 100, now); err == nil {
		t.Fatal("a short buffer must be refused")
	}
}

func TestEncoderColourModes(t *testing.T) {
	// 24 bit keeps the colour; grey takes it away and a change below the mode is not a change
	e := NewEncoder(Settings{Colors: Colors24, FPS: 15, NoLimit: true, Mbit: 4})
	tiles, _ := e.Encode(screen(64, 64, 0, 0, 255), 64, 64, time.Unix(1, 0)) // red in BGRA
	img, err := jpeg.Decode(bytes.NewReader(tiles[0].Data))
	if err != nil {
		t.Fatal(err)
	}
	r, g, b, _ := img.At(32, 32).RGBA()
	if r>>8 < 200 || g>>8 > 60 || b>>8 > 60 {
		t.Errorf("red must stay red: %d %d %d", r>>8, g>>8, b>>8)
	}

	e.SetSettings(Settings{Colors: ColorsGray, FPS: 15, NoLimit: true, Mbit: 4})
	tiles, _ = e.Encode(screen(64, 64, 0, 0, 255), 64, 64, time.Unix(2, 0))
	if len(tiles) != 1 {
		t.Fatalf("a change of the mode sends the whole picture: %d", len(tiles))
	}
	img, _ = jpeg.Decode(bytes.NewReader(tiles[0].Data))
	if _, ok := img.(*image.Gray); !ok {
		t.Fatalf("a grey tile is a grey JPEG, got %T", img)
	}
	// 16 colours mode: a difference in the low bits is not sent
	e.SetSettings(Settings{Colors: Colors16, FPS: 15, NoLimit: true, Mbit: 4})
	p := screen(64, 64, 100, 100, 100)
	_, _ = e.Encode(p, 64, 64, time.Unix(3, 0)) // the first picture
	p[0] ^= 0x01
	if tiles, _ := e.Encode(p, 64, 64, time.Unix(4, 0)); len(tiles) != 0 {
		t.Fatalf("a change below the colour depth costs nothing: %d", len(tiles))
	}
}

func TestEncoderKeepsToTheBudget(t *testing.T) {
	// a noisy picture is large; with a low limit the encoder skips pictures and lowers the quality, and what was
	// skipped is sent later (the previous picture is not replaced by a picture that was not sent)
	const w, h = 256, 256
	noisy := func(seed byte) []byte {
		p := make([]byte, w*h*4)
		x := uint32(seed) + 1
		for i := range p {
			x = x*1664525 + 1013904223
			p[i] = byte(x >> 24)
		}
		return p
	}
	e := NewEncoder(Settings{Colors: Colors24, FPS: 30, Mbit: 0.5, Auto: true})
	now := time.Unix(10, 0)
	q0 := e.Quality()
	sent, skipped := 0, 0
	for i := 0; i < 30; i++ {
		tiles, err := e.Encode(noisy(byte(i)), w, h, now)
		if err != nil {
			t.Fatal(err)
		}
		if len(tiles) > 0 {
			sent++
		} else {
			skipped++
		}
		now = now.Add(33 * time.Millisecond) // one second of frames
	}
	if skipped == 0 || sent == 0 {
		t.Fatalf("expected both sent and skipped pictures: sent %d, skipped %d", sent, skipped)
	}
	if e.Quality() >= q0 {
		t.Fatalf("the quality must drop under a low limit: %d -> %d", q0, e.Quality())
	}
	// an unlimited encoder never skips
	u := NewEncoder(Settings{Colors: Colors24, FPS: 30, NoLimit: true, Mbit: 4})
	for i := 0; i < 5; i++ {
		if tiles, _ := u.Encode(noisy(byte(i)), w, h, now); len(tiles) == 0 {
			t.Fatal("no limit means no skipped picture")
		}
	}
	// fixed quality when the encoder is not allowed to adapt
	f := NewEncoder(Settings{Colors: Colors24, FPS: 30, Mbit: 0.5})
	if f.Quality() != fixedQuality {
		t.Fatalf("fixed quality: %d", f.Quality())
	}
}

func TestParseClipAndMonitor(t *testing.T) {
	if c, err := ParseClip([]byte(`{"text":"привет\nмир"}`)); err != nil || c.Text != "привет\nмир" {
		t.Fatalf("clip: %+v %v", c, err)
	}
	if _, err := ParseClip([]byte(`{"text":"` + string(bytes.Repeat([]byte("a"), MaxClip+1)) + `"}`)); err == nil {
		t.Fatal("a clipboard over the limit must be refused")
	}
	if _, err := ParseClip([]byte(`nope`)); err == nil {
		t.Fatal("bad JSON must be refused")
	}
	for _, ok := range []string{`{"i":-1}`, `{"i":0}`, `{"i":3}`} {
		if _, err := ParseMonitor([]byte(ok)); err != nil {
			t.Errorf("%s: %v", ok, err)
		}
	}
	for _, bad := range []string{`{"i":-2}`, `{"i":99}`, `x`} {
		if _, err := ParseMonitor([]byte(bad)); err == nil {
			t.Errorf("%s must be refused", bad)
		}
	}
	// the setting that hides the pointer is off when an older record does not know it
	var s Settings
	if err := json.Unmarshal([]byte(`{"colors":"24","fps":15,"mbit":4,"auto":true}`), &s); err != nil || s.HideCursor {
		t.Fatalf("an old record shows the pointer: %+v %v", s, err)
	}
}
