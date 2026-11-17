package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/bolgov0zero/linager/server/store"
)

// maxNoteChars is the longest note. A note is for a few paragraphs, not for a document.
const maxNoteChars = 20000

type noteView struct {
	Note      string `json:"note"`
	UpdatedAt int64  `json:"note_updated_at,omitempty"` // Unix seconds
	UpdatedBy string `json:"note_updated_by,omitempty"`
}

func noteViewOf(n store.ServerNote) noteView {
	v := noteView{Note: n.Text, UpdatedBy: n.UpdatedBy}
	if n.UpdatedAt != nil {
		v.UpdatedAt = n.UpdatedAt.Unix()
	}
	return v
}

// serverNoteSet replaces the note of a server (administrators; the other roles read it with the server).
func (a *Admin) serverNoteSet(w http.ResponseWriter, r *http.Request, u *store.User) {
	var req struct {
		Note string `json:"note"`
	}
	// the common body limit (16 KiB) is smaller than a long note in a language with two bytes per letter
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 256<<10)).Decode(&req); err != nil {
		jsonErr(w, http.StatusBadRequest, "bad request")
		return
	}
	text := strings.ReplaceAll(req.Note, "\r\n", "\n")
	if !utf8.ValidString(text) || utf8.RuneCountInString(text) > maxNoteChars {
		jsonErr(w, http.StatusUnprocessableEntity, "the note is too long or is not text")
		return
	}
	id := r.PathValue("id")
	n, err := a.DB.SetServerNote(r.Context(), id, text, u.Login)
	if err != nil {
		jsonErr(w, http.StatusNotFound, "server not found")
		return
	}
	name := id
	if s, err := a.DB.ServerByID(r.Context(), id); err == nil {
		name = s.Name
	}
	// the text itself stays out of the journal: it may hold what the administrator does not want copied around
	a.audit(r, u, "server.note", "server", id, name, "ok", map[string]any{"chars": utf8.RuneCountInString(text)})
	writeJSON(w, http.StatusOK, noteViewOf(n))
}
