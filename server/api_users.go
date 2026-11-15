package server

import (
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/bolgov0zero/linager/server/store"
)

var loginRe = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{2,31}$`)

type userView struct {
	ID           int64     `json:"id"`
	Login        string    `json:"login"`
	Role         string    `json:"role"`
	TOTPEnabled  bool      `json:"totp_enabled"`
	Passkeys     int       `json:"passkeys"`
	RecoveryLeft int       `json:"recovery_left"`
	CreatedAt    time.Time `json:"created_at"`
	Current      bool      `json:"current"`
}

func (a *Admin) usersList(w http.ResponseWriter, r *http.Request, u *store.User) {
	rows, err := a.DB.ListUsers(r.Context())
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	out := make([]userView, 0, len(rows))
	for _, x := range rows {
		out = append(out, userView{ID: x.ID, Login: x.Login, Role: "admin", TOTPEnabled: x.TOTPEnabled, Passkeys: x.Passkeys,
			RecoveryLeft: x.RecoveryLeft, CreatedAt: x.CreatedAt, Current: x.ID == u.ID})
	}
	writeJSON(w, http.StatusOK, map[string]any{"users": out})
}

type userReq struct {
	Login    string `json:"login"`
	Password string `json:"password"`
	Current  string `json:"current"`
	Confirm  bool   `json:"confirm"`
}

func (a *Admin) userCreate(w http.ResponseWriter, r *http.Request, u *store.User) {
	var req userReq
	if !decode(w, r, &req) {
		return
	}
	login := strings.ToLower(strings.TrimSpace(req.Login))
	if !loginRe.MatchString(login) {
		jsonErr(w, http.StatusUnprocessableEntity, "login: 3-32 characters, a-z 0-9 . _ -")
		return
	}
	hash, err := HashPassword(req.Password)
	if err != nil {
		jsonErr(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	nu, err := a.DB.CreateAdmin(r.Context(), login, hash)
	if err != nil {
		a.audit(r, u, "user.create", "user", "", login, "failed", nil)
		jsonErr(w, http.StatusConflict, "this login is taken")
		return
	}
	a.audit(r, u, "user.create", "user", strconv.FormatInt(nu.ID, 10), login, "ok", nil)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "id": nu.ID})
}

func (a *Admin) targetUser(w http.ResponseWriter, r *http.Request) *store.User {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		jsonErr(w, http.StatusNotFound, "not found")
		return nil
	}
	t, err := a.DB.UserByID(r.Context(), id)
	if err != nil {
		jsonErr(w, http.StatusNotFound, "not found")
		return nil
	}
	return t
}

func (a *Admin) userDelete(w http.ResponseWriter, r *http.Request, u *store.User) {
	t := a.targetUser(w, r)
	if t == nil {
		return
	}
	var req userReq
	_ = decode(w, r, &req)
	if t.ID == u.ID {
		jsonErr(w, http.StatusConflict, "you cannot delete yourself")
		return
	}
	if !req.Confirm {
		needConfirm(w)
		return
	}
	if err := a.DB.DeleteUser(r.Context(), t.ID); err != nil {
		jsonErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	a.audit(r, u, "user.delete", "user", strconv.FormatInt(t.ID, 10), t.Login, "ok", nil)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// changeOwnPassword replaces the password of the signed-in user; the other sessions of the user end.
func (a *Admin) changeOwnPassword(w http.ResponseWriter, r *http.Request, u *store.User) {
	var req userReq
	if !decode(w, r, &req) {
		return
	}
	if !VerifyPassword(req.Current, u.PasswordHash) {
		a.audit(r, u, "user.password.change", "user", strconv.FormatInt(u.ID, 10), u.Login, "failed", nil)
		jsonErr(w, http.StatusForbidden, "invalid password")
		return
	}
	hash, err := HashPassword(req.Password)
	if err != nil {
		jsonErr(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	if err := a.DB.SetPassword(r.Context(), u.ID, hash); err != nil {
		jsonErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	var keep []byte
	if c, err := r.Cookie(cookieName); err == nil {
		keep = HashToken(c.Value)
	}
	_ = a.DB.DeleteSessionsOf(r.Context(), u.ID, keep)
	a.audit(r, u, "user.password.change", "user", strconv.FormatInt(u.ID, 10), u.Login, "ok", nil)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// userSetPassword sets a new password for another user (reset); all sessions of that user end.
func (a *Admin) userSetPassword(w http.ResponseWriter, r *http.Request, u *store.User) {
	t := a.targetUser(w, r)
	if t == nil {
		return
	}
	if t.ID == u.ID {
		jsonErr(w, http.StatusConflict, "use the password change of your profile")
		return
	}
	var req userReq
	if !decode(w, r, &req) {
		return
	}
	if !req.Confirm {
		needConfirm(w)
		return
	}
	hash, err := HashPassword(req.Password)
	if err != nil {
		jsonErr(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	if err := a.DB.SetPassword(r.Context(), t.ID, hash); err != nil {
		jsonErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	_ = a.DB.RecordLoginSuccess(r.Context(), t.ID) // also lifts a lock-out
	_ = a.DB.DeleteSessionsOf(r.Context(), t.ID, nil)
	a.audit(r, u, "user.password.reset", "user", strconv.FormatInt(t.ID, 10), t.Login, "ok", nil)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// userReset2FA removes the second factors of another user (lost phone and codes).
func (a *Admin) userReset2FA(w http.ResponseWriter, r *http.Request, u *store.User) {
	t := a.targetUser(w, r)
	if t == nil {
		return
	}
	if t.ID == u.ID {
		jsonErr(w, http.StatusConflict, "use the 2FA settings of your profile")
		return
	}
	var req userReq
	_ = decode(w, r, &req)
	if !req.Confirm {
		needConfirm(w)
		return
	}
	if err := a.DB.ClearSecondFactors(r.Context(), t.ID); err != nil {
		jsonErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	_ = a.DB.DeleteSessionsOf(r.Context(), t.ID, nil)
	a.audit(r, u, "user.2fa.reset", "user", strconv.FormatInt(t.ID, 10), t.Login, "ok", nil)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// recoveryRegenerate replaces all recovery codes of the user with new ones (shown once).
func (a *Admin) recoveryRegenerate(w http.ResponseWriter, r *http.Request, u *store.User) {
	var req userReq
	if !decode(w, r, &req) {
		return
	}
	if !u.TOTPEnabled {
		jsonErr(w, http.StatusConflict, "enable 2FA first")
		return
	}
	if !VerifyPassword(req.Password, u.PasswordHash) {
		a.audit(r, u, "auth.recovery.regenerate", "user", "", u.Login, "failed", nil)
		jsonErr(w, http.StatusForbidden, "invalid password")
		return
	}
	codes, hashes := NewRecoveryCodes()
	if err := a.DB.ReplaceRecoveryCodes(r.Context(), u.ID, hashes); err != nil {
		jsonErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	a.audit(r, u, "auth.recovery.regenerate", "user", "", u.Login, "ok", nil)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "recovery_codes": codes})
}

// ---- snippets ----

type snippetReq struct {
	Name    string  `json:"name"`
	Command string  `json:"command"`
	IDs     []int64 `json:"ids"`
	Server  string  `json:"server_id"`
}

const (
	maxSnippetName    = 80
	maxSnippetCommand = 4000
)

func validSnippet(w http.ResponseWriter, req *snippetReq) bool {
	req.Name = strings.TrimSpace(req.Name)
	req.Command = strings.TrimRight(req.Command, "\r\n")
	switch {
	case req.Name == "" || len(req.Name) > maxSnippetName:
		jsonErr(w, http.StatusUnprocessableEntity, "name: 1-80 characters")
	case strings.TrimSpace(req.Command) == "" || len(req.Command) > maxSnippetCommand:
		jsonErr(w, http.StatusUnprocessableEntity, "command: 1-4000 characters")
	default:
		return true
	}
	return false
}

func (a *Admin) snippetsList(w http.ResponseWriter, r *http.Request, _ *store.User) {
	list, err := a.DB.Snippets(r.Context())
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"snippets": list})
}

func (a *Admin) snippetCreate(w http.ResponseWriter, r *http.Request, u *store.User) {
	var req snippetReq
	if !decode(w, r, &req) || !validSnippet(w, &req) {
		return
	}
	sn, err := a.DB.CreateSnippet(r.Context(), req.Name, req.Command, u.ID)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	a.audit(r, u, "snippet.create", "snippet", strconv.FormatInt(sn.ID, 10), sn.Name, "ok", nil)
	writeJSON(w, http.StatusOK, sn)
}

func snippetID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		jsonErr(w, http.StatusNotFound, "not found")
		return 0, false
	}
	return id, true
}

func (a *Admin) snippetUpdate(w http.ResponseWriter, r *http.Request, u *store.User) {
	id, ok := snippetID(w, r)
	if !ok {
		return
	}
	var req snippetReq
	if !decode(w, r, &req) || !validSnippet(w, &req) {
		return
	}
	if err := a.DB.UpdateSnippet(r.Context(), id, req.Name, req.Command); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			jsonErr(w, http.StatusNotFound, "not found")
			return
		}
		jsonErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	a.audit(r, u, "snippet.update", "snippet", strconv.FormatInt(id, 10), req.Name, "ok", nil)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *Admin) snippetDelete(w http.ResponseWriter, r *http.Request, u *store.User) {
	id, ok := snippetID(w, r)
	if !ok {
		return
	}
	sn, err := a.DB.SnippetByID(r.Context(), id)
	if err != nil {
		jsonErr(w, http.StatusNotFound, "not found")
		return
	}
	if err := a.DB.DeleteSnippet(r.Context(), id); err != nil {
		jsonErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	a.audit(r, u, "snippet.delete", "snippet", strconv.FormatInt(id, 10), sn.Name, "ok", nil)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *Admin) snippetsOrder(w http.ResponseWriter, r *http.Request, u *store.User) {
	var req snippetReq
	if !decode(w, r, &req) {
		return
	}
	if len(req.IDs) > 1000 {
		jsonErr(w, http.StatusBadRequest, "bad request")
		return
	}
	if err := a.DB.ReorderSnippets(r.Context(), req.IDs); err != nil {
		jsonErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	a.audit(r, u, "snippet.order", "snippet", "", "", "ok", nil)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// snippetRan records that a snippet was run in a terminal (the browser reports it; the keystrokes
// themselves go through the terminal stream, which is audited as a whole).
func (a *Admin) snippetRan(w http.ResponseWriter, r *http.Request, u *store.User) {
	id, ok := snippetID(w, r)
	if !ok {
		return
	}
	var req snippetReq
	_ = decode(w, r, &req)
	sn, err := a.DB.SnippetByID(r.Context(), id)
	if err != nil {
		jsonErr(w, http.StatusNotFound, "not found")
		return
	}
	target := req.Server
	if len(target) > 64 {
		target = ""
	}
	a.audit(r, u, "snippet.run", "snippet", strconv.FormatInt(id, 10), sn.Name, "ok", map[string]any{"server_id": target})
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
