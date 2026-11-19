package assist

import (
	"bytes"
	"fmt"
	"image"
	"image/jpeg"
	"time"
)

// TileSize is the side of the squares the screen is compared in.
const TileSize = 64

const (
	startQuality = 75
	minQuality   = 25
	maxQuality   = 85
	fixedQuality = 80
)

// A tile sent at a low quality (the traffic budget was spent) is sent again at the quality in force once the screen is
// still: refineGap is how much lower than the quality now it must have been, refineMax how many tiles one picture
// takes. A flat dark area sent in tiles of different quality shows as rectangles of slightly different shades.
const (
	refineGap = 6
	refineMax = 8
)

// Encoder turns screen shots into the tiles that changed since the last picture that was sent. It keeps the previous
// picture, the colour mode and the traffic budget; it does not touch the screen or the network, so it is tested
// everywhere. It is used by one goroutine.
type Encoder struct {
	set     Settings
	w, h    int
	prev    []byte  // the last picture that was sent, BGRA after the colour reduction
	tileQ   []uint8 // the JPEG quality each tile was last sent at
	cur     []byte
	changed []bool
	quality int
	tokens  float64 // bytes that may still be sent
	last    time.Time
}

// NewEncoder makes an encoder with the given settings (invalid ones are replaced by the defaults).
func NewEncoder(s Settings) *Encoder {
	e := &Encoder{quality: startQuality}
	e.SetSettings(s)
	return e
}

// SetSettings applies new settings; the next picture is a whole one when the colours changed.
func (e *Encoder) SetSettings(s Settings) {
	if s.Validate() != nil {
		s = DefaultSettings()
	}
	if s.Colors != e.set.Colors {
		e.prev = nil
	}
	e.set = s
	if !s.Auto {
		e.quality = fixedQuality
	} else if e.quality == fixedQuality {
		e.quality = startQuality
	}
}

// Settings returns the settings in force.
func (e *Encoder) Settings() Settings { return e.set }

// Quality is the JPEG quality now (the adaptive one moves between 25 and 85).
func (e *Encoder) Quality() int { return e.quality }

// Interval is the time between two pictures at the frame rate limit.
func (e *Encoder) Interval() time.Duration { return time.Second / time.Duration(e.set.FPS) }

// Reset makes the next picture a whole one (a new viewer, a change of the screen size).
func (e *Encoder) Reset() { e.prev, e.tileQ = nil, nil }

// rate is the traffic budget in bytes per second; zero means no limit.
func (e *Encoder) rate() float64 {
	if e.set.NoLimit {
		return 0
	}
	return e.set.Mbit * 125000
}

// Encode compares a screen shot (BGRA, w*4 bytes per row) with the last one that was sent and returns the changed
// tiles as JPEG pictures. It returns nothing when nothing changed, or when the traffic budget is spent: the picture
// is then not counted as sent, and the next call compares against the older one, so no change is lost.
func (e *Encoder) Encode(bgra []byte, w, h int, now time.Time) ([]Tile, error) {
	if w <= 0 || h <= 0 || w > 65535 || h > 65535 || len(bgra) < w*h*4 {
		return nil, fmt.Errorf("assist: bad screen shot %dx%d (%d bytes)", w, h, len(bgra))
	}
	if w != e.w || h != e.h {
		e.w, e.h, e.prev, e.tileQ = w, h, nil, nil
	}
	if rate := e.rate(); rate > 0 {
		if e.last.IsZero() {
			e.tokens = rate
		} else if dt := now.Sub(e.last).Seconds(); dt > 0 {
			e.tokens = min(e.tokens+rate*dt, rate)
		}
		e.last = now
		if e.tokens <= 0 {
			return nil, nil
		}
	}

	n := w * h * 4
	if cap(e.cur) < n {
		e.cur = make([]byte, n)
	}
	e.cur = e.cur[:n]
	reduce(e.cur, bgra[:n], e.set.Colors)

	tx, ty := (w+TileSize-1)/TileSize, (h+TileSize-1)/TileSize
	if cap(e.changed) < tx*ty {
		e.changed = make([]bool, tx*ty)
	}
	e.changed = e.changed[:tx*ty]
	any := false
	for j := 0; j < ty; j++ {
		for i := 0; i < tx; i++ {
			c := e.prev == nil || e.tileDiffers(i, j)
			e.changed[j*tx+i] = c
			any = any || c
		}
	}
	if len(e.tileQ) != tx*ty {
		e.tileQ = make([]uint8, tx*ty)
	}
	if !any {
		// the screen is still: tiles that were sent at a low quality are sent again, at the best quality when the quality
		// moves by the traffic budget
		q := e.quality
		if e.set.Auto && e.rate() > 0 {
			q = maxQuality
		}
		n := 0
		for idx := range e.changed {
			if n < refineMax && int(e.tileQ[idx])+refineGap <= q {
				e.changed[idx] = true
				any = true
				n++
			}
		}
		if !any {
			return nil, nil
		}
		e.quality = q
	}

	var tiles []Tile
	total := 0
	for j := 0; j < ty; j++ {
		for i := 0; i < tx; {
			if !e.changed[j*tx+i] {
				i++
				continue
			}
			k := i
			for k < tx && e.changed[j*tx+k] {
				k++
			}
			x0, y0 := i*TileSize, j*TileSize
			rw, rh := min(k*TileSize, w)-x0, min(y0+TileSize, h)-y0
			pic, err := e.jpeg(x0, y0, rw, rh)
			if err != nil {
				return nil, err
			}
			tiles = append(tiles, Tile{X: x0, Y: y0, W: rw, H: rh, Data: pic})
			for t := i; t < k; t++ {
				e.tileQ[j*tx+t] = uint8(e.quality) //nolint:gosec // 25..85
			}
			total += len(pic)
			i = k
		}
	}
	e.prev, e.cur = e.cur, e.prev
	if rate := e.rate(); rate > 0 {
		e.tokens -= float64(total)
		if e.set.Auto {
			budget := rate / float64(e.set.FPS)
			switch {
			case float64(total) > budget*1.2:
				e.quality = max(minQuality, e.quality-10)
			case float64(total) < budget*0.5:
				e.quality = min(maxQuality, e.quality+3)
			}
		}
	}
	return tiles, nil
}

func (e *Encoder) tileDiffers(i, j int) bool {
	x0, y0 := i*TileSize, j*TileSize
	x1, y1 := min(x0+TileSize, e.w), min(y0+TileSize, e.h)
	for y := y0; y < y1; y++ {
		a := (y*e.w + x0) * 4
		b := (y*e.w + x1) * 4
		if !bytes.Equal(e.cur[a:b], e.prev[a:b]) {
			return true
		}
	}
	return false
}

// jpeg encodes the rectangle of the reduced picture.
func (e *Encoder) jpeg(x0, y0, rw, rh int) ([]byte, error) {
	var img image.Image
	if e.set.Colors == ColorsGray {
		g := image.NewGray(image.Rect(0, 0, rw, rh))
		for y := 0; y < rh; y++ {
			src := e.cur[((y0+y)*e.w+x0)*4:]
			dst := g.Pix[y*g.Stride : y*g.Stride+rw]
			for x := range dst {
				dst[x] = src[x*4] // blue, green and red are equal after the reduction
			}
		}
		img = g
	} else {
		c := image.NewRGBA(image.Rect(0, 0, rw, rh))
		for y := 0; y < rh; y++ {
			src := e.cur[((y0+y)*e.w+x0)*4:]
			dst := c.Pix[y*c.Stride:]
			for x := 0; x < rw; x++ {
				dst[x*4], dst[x*4+1], dst[x*4+2], dst[x*4+3] = src[x*4+2], src[x*4+1], src[x*4], 255
			}
		}
		img = c
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: e.quality}); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// reduce copies src to dst and takes colours away. Reducing before the comparison means that a change below what the
// mode can show (a clock in the corner of a grey picture) does not cost a picture.
func reduce(dst, src []byte, mode string) {
	switch mode {
	case Colors16:
		for i := 0; i+3 < len(src); i += 4 {
			dst[i], dst[i+1], dst[i+2], dst[i+3] = src[i]&0xF8, src[i+1]&0xFC, src[i+2]&0xF8, 255
		}
	case Colors256:
		for i := 0; i+3 < len(src); i += 4 {
			dst[i], dst[i+1], dst[i+2], dst[i+3] = src[i]&0xC0, src[i+1]&0xE0, src[i+2]&0xE0, 255
		}
	case ColorsGray:
		for i := 0; i+3 < len(src); i += 4 {
			y := byte((uint32(src[i])*114 + uint32(src[i+1])*587 + uint32(src[i+2])*299) / 1000) //nolint:gosec // the weights add up to 1000: at most 255
			dst[i], dst[i+1], dst[i+2], dst[i+3] = y, y, y, 255
		}
	default:
		copy(dst, src)
	}
}
