package server

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/bolgov0zero/linager/server/store"
)

const (
	maxGroupName = 40
	maxGroups    = 50
)

func cleanGroupName(s string) (string, bool) {
	s = strings.TrimSpace(s)
	return s, s != "" && utf8.RuneCountInString(s) <= maxGroupName
}

func (a *Admin) groupsList(w http.ResponseWriter, r *http.Request, _ *store.User) {
	list, err := a.DB.Groups(r.Context())
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (a *Admin) groupCreate(w http.ResponseWriter, r *http.Request, u *store.User) {
	var req struct {
		Name string `json:"name"`
	}
	if !decode(w, r, &req) {
		return
	}
	name, ok := cleanGroupName(req.Name)
	if !ok {
		jsonErr(w, http.StatusBadRequest, "invalid group name")
		return
	}
	if list, err := a.DB.Groups(r.Context()); err == nil && len(list) >= maxGroups {
		jsonErr(w, http.StatusBadRequest, "too many groups")
		return
	}
	g, err := a.DB.GroupCreate(r.Context(), name)
	if errors.Is(err, store.ErrGroupExists) {
		jsonErr(w, http.StatusConflict, "group name already in use")
		return
	}
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	a.audit(r, u, "group.create", "group", strconv.FormatInt(g.ID, 10), g.Name, "ok", nil)
	a.Live.Publish("dashboard", map[string]any{})
	writeJSON(w, http.StatusOK, g)
}

func (a *Admin) groupRename(w http.ResponseWriter, r *http.Request, u *store.User) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	var req struct {
		Name string `json:"name"`
	}
	if err != nil || !decode(w, r, &req) {
		if err != nil {
			jsonErr(w, http.StatusBadRequest, "bad request")
		}
		return
	}
	name, ok := cleanGroupName(req.Name)
	if !ok {
		jsonErr(w, http.StatusBadRequest, "invalid group name")
		return
	}
	switch err := a.DB.GroupRename(r.Context(), id, name); {
	case errors.Is(err, store.ErrNotFound):
		jsonErr(w, http.StatusNotFound, "group not found")
	case errors.Is(err, store.ErrGroupExists):
		jsonErr(w, http.StatusConflict, "group name already in use")
	case err != nil:
		jsonErr(w, http.StatusInternalServerError, "internal error")
	default:
		a.audit(r, u, "group.rename", "group", strconv.FormatInt(id, 10), name, "ok", nil)
		a.Live.Publish("dashboard", map[string]any{})
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	}
}

func (a *Admin) groupDelete(w http.ResponseWriter, r *http.Request, u *store.User) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		jsonErr(w, http.StatusBadRequest, "bad request")
		return
	}
	name, err := a.DB.GroupDelete(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		jsonErr(w, http.StatusNotFound, "group not found")
		return
	}
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	a.audit(r, u, "group.delete", "group", strconv.FormatInt(id, 10), name, "ok", nil)
	a.Live.Publish("dashboard", map[string]any{})
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// serverSetGroup puts a server into a group; group_id null removes it from its group.
func (a *Admin) serverSetGroup(w http.ResponseWriter, r *http.Request, u *store.User) {
	var req struct {
		GroupID *int64 `json:"group_id"`
	}
	if !decode(w, r, &req) {
		return
	}
	srv, err := a.DB.ServerByID(r.Context(), r.PathValue("id"))
	if err != nil {
		jsonErr(w, http.StatusNotFound, "server not found")
		return
	}
	gname := ""
	if req.GroupID != nil {
		if gname, err = a.DB.GroupName(r.Context(), *req.GroupID); err != nil {
			jsonErr(w, http.StatusNotFound, "group not found")
			return
		}
	}
	if err := a.DB.SetServerGroup(r.Context(), srv.ID, req.GroupID); err != nil {
		jsonErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	a.audit(r, u, "group.assign", "server", srv.ID, srv.Name, "ok", map[string]any{"group": gname})
	a.Live.Publish("dashboard", map[string]any{})
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
