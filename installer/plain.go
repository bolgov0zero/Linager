package installer

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

// plainFrontend is the non-interactive frontend: line-oriented output, no questions.
type plainFrontend struct {
	out    io.Writer
	titles []string
	lang   Lang
}

// NewPlain returns the line-oriented frontend used when there is no terminal or --no-tui is set.
func NewPlain(out io.Writer) Frontend { return &plainFrontend{out: out} }

func (p *plainFrontend) Run(ctx context.Context, lang Lang, flow Flow) (*Summary, error) {
	p.lang = lang
	sum, err := flow(ctx, p)
	if sum != nil {
		PrintSummary(p.out, sum, false)
	}
	return sum, err
}

func (p *plainFrontend) Interactive() bool { return false }
func (p *plainFrontend) Lang() Lang        { return p.lang }
func (p *plainFrontend) Ask(Form) (map[string]string, error) {
	return nil, errors.New("interactive input is not available")
}
func (p *plainFrontend) Steps(titles []string) { p.titles = titles }
func (p *plainFrontend) Wait(w Waiting) {
	_, _ = fmt.Fprintf(p.out, "      key: %s   check code: %s   fingerprint: %s\n", w.Key, w.Code, w.Fingerprint)
}

func (p *plainFrontend) Event(e Event) {
	if e.Index < 0 || e.Index >= len(p.titles) {
		return
	}
	prefix := fmt.Sprintf("[%d/%d] %s", e.Index+1, len(p.titles), p.titles[e.Index])
	switch e.State {
	case StepStart:
		_, _ = fmt.Fprintf(p.out, "%s ...\n", prefix)
	case StepDone:
		_, _ = fmt.Fprintf(p.out, "%s ok\n", prefix)
	case StepSkip:
		_, _ = fmt.Fprintf(p.out, "%s skipped\n", prefix)
	case StepFail:
		_, _ = fmt.Fprintf(p.out, "%s FAILED: %v\n", prefix, e.Err)
		for _, l := range e.Tail {
			_, _ = fmt.Fprintf(p.out, "    %s\n", l)
		}
	}
}

// PrintSummary writes the final summary. With styled=false it is plain text.
func PrintSummary(w io.Writer, s *Summary, _ bool) {
	_, _ = fmt.Fprintf(w, "\n%s\n\n", s.Title)
	width := 0
	for _, kv := range s.Lines {
		if n := len([]rune(kv.Label)); n > width {
			width = n
		}
	}
	for _, kv := range s.Lines {
		_, _ = fmt.Fprintf(w, "  %s  %s\n", kv.Label+strings.Repeat(" ", width-len([]rune(kv.Label))), kv.Value)
	}
	if len(s.Notes) > 0 {
		_, _ = fmt.Fprintln(w)
		for _, n := range s.Notes {
			_, _ = fmt.Fprintf(w, "  %s\n", n)
		}
	}
	_, _ = fmt.Fprintln(w)
}

func isTerminal(f *os.File) bool {
	st, err := f.Stat()
	return err == nil && st.Mode()&os.ModeCharDevice != 0
}
