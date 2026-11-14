// Package installer implements the `linager install` wizard for the server and the agent.
package installer

import (
	"context"
	"errors"
)

// Lang selects the interface language.
type Lang string

// Supported languages.
const (
	RU Lang = "ru"
	EN Lang = "en"
)

// ErrAborted is returned when the user cancels (Esc on the first screen, Ctrl+C).
var ErrAborted = errors.New("aborted")

// FieldKind describes how a form field is edited.
type FieldKind int

// Field kinds.
const (
	KindText   FieldKind = iota // free text
	KindSecret                  // hidden input; the user must type it twice when Confirm is set
	KindChoice                  // pick one of Options with arrows
	KindNumber                  // digits only
)

// Option is one entry of a KindChoice field.
type Option struct{ Value, Label string }

// Field is one question of a Form.
type Field struct {
	Key      string
	Label    string
	Hint     string // short grey hint under the input
	Kind     FieldKind
	Default  string
	Options  []Option
	Required bool
	Confirm  bool // KindSecret: ask the value twice
	// Validate returns a user-visible error message; all holds the answers given so far.
	Validate func(value string, all map[string]string) error
	// Visible hides the field depending on earlier answers; nil means always visible.
	Visible func(all map[string]string) bool
}

// Form is a sequence of questions shown one per screen.
type Form struct {
	Title  string
	Fields []Field
}

// EventState is the state of an install step.
type EventState int

// Step states.
const (
	StepStart EventState = iota
	StepDone
	StepFail
	StepSkip
)

// Event reports progress of the install plan.
type Event struct {
	Index int        // 0-based index into the step list declared with Steps
	State EventState //
	Info  string     // optional short detail for the running step ("downloading...")
	Err   error      // set for StepFail
	Tail  []string   // for StepFail: last lines of the log
}

// Waiting is the agent-side pairing screen.
type Waiting struct {
	Key         string // XXXX-XXXX
	Code        string // check code
	Fingerprint string
	Server      string
}

// KV is one line of the final summary.
type KV struct {
	Label string
	Value string
	Mono  bool // render value as code (selectable)
}

// Summary is printed to the normal terminal screen when the wizard ends, so it stays in scrollback.
type Summary struct {
	Title   string
	Lines   []KV
	Notes   []string
	Success bool
}

// Console is what an install flow uses to talk to the user.
type Console interface {
	// Interactive reports whether Ask may be used (false for non-TTY / flag-driven runs).
	Interactive() bool
	Ask(f Form) (map[string]string, error)
	// Steps declares the ordered step titles; call once before Event.
	Steps(titles []string)
	Event(e Event)
	// Wait shows the pairing screen until the next Event or Ask replaces it.
	Wait(w Waiting)
	// Lang is the interface language chosen for this run.
	Lang() Lang
}

// Flow is a complete install procedure.
type Flow func(ctx context.Context, c Console) (*Summary, error)

// Frontend runs a Flow and renders the experience.
type Frontend interface {
	Run(ctx context.Context, lang Lang, flow Flow) (*Summary, error)
}
