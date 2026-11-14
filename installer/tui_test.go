package installer

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func tuiTypeRunes(m formModel, s string) formModel {
	for _, r := range s {
		m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	return m
}

func tuiPress(m formModel, k tea.KeyType) formModel {
	m, _ = m.Update(tea.KeyMsg{Type: k})
	return m
}

func TestNumberFieldIgnoresLetters(t *testing.T) {
	m := newFormModel(EN, Form{Fields: []Field{{Key: "port", Label: "Port", Kind: KindNumber}}}, 80)
	m = tuiTypeRunes(m, "a1b-2z")
	if got := m.input.Value(); got != "12" {
		t.Fatalf("number input = %q, want %q", got, "12")
	}
}

func TestRequiredEmptyShowsErrorAndDoesNotAdvance(t *testing.T) {
	form := Form{Fields: []Field{
		{Key: "host", Label: "Host", Kind: KindText, Required: true},
		{Key: "name", Label: "Name", Kind: KindText},
	}}
	m := newFormModel(EN, form, 80)
	m = tuiPress(m, tea.KeyEnter)

	if m.idx != 0 {
		t.Fatalf("advanced to field %d on empty required input", m.idx)
	}
	if m.errMsg != tuiStringsFor(EN).required {
		t.Fatalf("errMsg = %q, want required message", m.errMsg)
	}
	if m.done {
		t.Fatal("form finished on empty required input")
	}
}

func TestSecretConfirmMismatchRejectedMatchAdvances(t *testing.T) {
	form := Form{Fields: []Field{
		{Key: "pw", Label: "Password", Kind: KindSecret, Confirm: true},
		{Key: "name", Label: "Name", Kind: KindText},
	}}
	m := newFormModel(EN, form, 80)

	m = tuiTypeRunes(m, "abc")
	m = tuiPress(m, tea.KeyEnter)
	if !m.confirming {
		t.Fatal("expected confirmation stage after first entry")
	}

	m = tuiTypeRunes(m, "abd")
	m = tuiPress(m, tea.KeyEnter)
	if m.idx != 0 {
		t.Fatalf("mismatch advanced to field %d", m.idx)
	}
	if m.errMsg != tuiStringsFor(EN).mismatch {
		t.Fatalf("errMsg = %q, want mismatch message", m.errMsg)
	}
	if m.confirming {
		t.Fatal("mismatch should restart from the first entry")
	}

	m = tuiTypeRunes(m, "abc")
	m = tuiPress(m, tea.KeyEnter)
	m = tuiTypeRunes(m, "abc")
	m = tuiPress(m, tea.KeyEnter)
	if m.idx != 1 {
		t.Fatalf("matching confirmation did not advance, idx = %d", m.idx)
	}
	if got := m.answers["pw"]; got != "abc" {
		t.Fatalf("stored secret = %q, want %q", got, "abc")
	}
}

func TestInvisibleFieldsAreSkipped(t *testing.T) {
	form := Form{Fields: []Field{
		{Key: "mode", Label: "Mode", Kind: KindChoice, Default: "a", Options: []Option{
			{Value: "a", Label: "A"},
			{Value: "b", Label: "B"},
		}},
		{Key: "extra", Label: "Extra", Kind: KindText,
			Visible: func(all map[string]string) bool { return all["mode"] == "b" }},
		{Key: "last", Label: "Last", Kind: KindText},
	}}

	m := newFormModel(EN, form, 80)
	m = tuiPress(m, tea.KeyEnter) // accept mode=a
	if m.idx != 2 {
		t.Fatalf("with mode=a, expected to land on field 2, got %d", m.idx)
	}

	m = newFormModel(EN, form, 80)
	m = tuiPress(m, tea.KeyDown) // choose mode=b
	m = tuiPress(m, tea.KeyEnter)
	if m.idx != 1 {
		t.Fatalf("with mode=b, expected to land on field 1, got %d", m.idx)
	}
}

func TestEscOnFirstFieldAborts(t *testing.T) {
	m := newFormModel(EN, Form{Fields: []Field{{Key: "a", Label: "A", Kind: KindText}}}, 80)
	m = tuiPress(m, tea.KeyEsc)
	if !m.aborted {
		t.Fatal("Esc on the first field did not abort")
	}
}

func TestEmptyOptionalSecretSkipsConfirmation(t *testing.T) {
	m := newFormModel(EN, Form{Fields: []Field{{Key: "p", Label: "p", Kind: KindSecret, Confirm: true}}}, 80)
	m.submitInput(m.form.Fields[0])
	if m.confirming {
		t.Fatal("an empty optional secret must be accepted without a repeat prompt")
	}
}
