package assist

import (
	"errors"
	"fmt"
	"math"
)

// Colour modes of the picture.
const (
	Colors24   = "24"   // full colour
	Colors16   = "16"   // 65 thousand colours (5-6-5 bits)
	Colors256  = "256"  // 256 colours (3-3-2 bits)
	ColorsGray = "gray" // shades of grey
)

// Settings are the parameters of the picture; the panel keeps them per server.
type Settings struct {
	Colors  string  `json:"colors"`
	FPS     int     `json:"fps"`     // the upper limit of the frame rate
	Mbit    float64 `json:"mbit"`    // the limit of the traffic, megabits per second
	NoLimit bool    `json:"nolimit"` // no limit of the traffic (a local network)
	Auto    bool    `json:"auto"`    // lower the quality by itself to keep to the limit
	// ShowCursor draws the pointer of the computer into the picture. It is off by default: the browser then shows its
	// own pointer, which moves without delay.
	ShowCursor bool `json:"show_cursor,omitempty"`
}

// Limits of the settings.
const (
	MinFPS, MaxFPS   = 1, 30
	MinMbit, MaxMbit = 0.5, 20.0
)

// DefaultSettings is the "Normal" preset of the panel.
func DefaultSettings() Settings {
	return Settings{Colors: Colors24, FPS: 15, Mbit: 4, Auto: true}
}

// Validate refuses what the panel and the helper would not honour.
func (s Settings) Validate() error {
	switch s.Colors {
	case Colors24, Colors16, Colors256, ColorsGray:
	default:
		return fmt.Errorf("unknown colour mode %q", s.Colors)
	}
	if s.FPS < MinFPS || s.FPS > MaxFPS {
		return fmt.Errorf("frame rate must be %d..%d", MinFPS, MaxFPS)
	}
	if math.IsNaN(s.Mbit) || s.Mbit < MinMbit || s.Mbit > MaxMbit {
		return errors.New("traffic limit must be 0.5..20 Mbit/s")
	}
	return nil
}
