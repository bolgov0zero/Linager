package assist

import (
	"encoding/json"
	"errors"
	"unicode/utf8"
)

// MaxTyping is the largest JSON of one Typing frame: the panel drops anything longer.
const MaxTyping = 4096

// MaxTypingRunes is the longest text of one Typing frame (a password of a key of the vault is at most 512 characters).
const MaxTypingRunes = 512

// MaxTypingLabel is the longest name of the key that goes with the text into the audit log.
const MaxTypingLabel = 80

// Typing asks the helper to type a text on the screen of a sign-in or a UAC prompt (a login or a password of a key of the
// vault). The text is typed as it is, as characters, not as keys, so the keyboard layout of the computer does not matter,
// and the clipboard is not touched. After says what is pressed once the text is typed: "" nothing, "tab" or "enter".
// Label is the name of the key (only for the audit log, never the text).
type Typing struct {
	Text  string `json:"text"`
	After string `json:"after,omitempty"`
	Label string `json:"label,omitempty"`
}

// Typed is the answer of the helper to a Typing: it was typed, or it was refused and why ("desktop": the screen of the
// computer is not a sign-in or a UAC prompt now; "unsupported": this computer cannot do it; "failed").
type Typed struct {
	OK     bool   `json:"ok"`
	Reason string `json:"reason,omitempty"`
}

func noControl(s string) bool {
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}

// ParseTyping reads and checks a Typing: the text is not empty, within the limit and has no control characters (the
// keys Tab and Enter have their own field).
func ParseTyping(b []byte) (Typing, error) {
	var t Typing
	if len(b) > MaxTyping {
		return t, errors.New("typing is too long")
	}
	if err := json.Unmarshal(b, &t); err != nil {
		return t, err
	}
	n := utf8.RuneCountInString(t.Text)
	switch {
	case n == 0 || n > MaxTypingRunes:
		return t, errors.New("typing text length")
	case !utf8.ValidString(t.Text) || !noControl(t.Text):
		return t, errors.New("typing text has bad characters")
	case t.After != "" && t.After != "tab" && t.After != "enter":
		return t, errors.New("typing after")
	case utf8.RuneCountInString(t.Label) > MaxTypingLabel || !utf8.ValidString(t.Label) || !noControl(t.Label):
		return t, errors.New("typing label")
	}
	return t, nil
}
