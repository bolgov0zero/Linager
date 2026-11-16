package server

import (
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/bolgov0zero/linager/server/store"
)

// The "Users" tab: the local accounts of a managed host (not the users of the panel, see api_users.go).
// Every change needs the confirmation of the administrator, goes to the audit log and is checked again by
// the agent. A password goes to the agent and nowhere else: it is not put into the audit log.

var hostUserNameRe = regexp.MustCompile(`^[a-zA-Z0-9._][a-zA-Z0-9._-]{0,63}$`)

func (a *Admin) hostUsersState(w http.ResponseWriter, r *http.Request, _ *store.User) {
	a.proxyCall(w, r, "users", "state", nil, 40*time.Second)
}

type hostUserAddReq struct {
	Name     string   `json:"name"`
	Gecos    string   `json:"gecos"`
	Shell    string   `json:"shell"`
	Home     string   `json:"home"`
	Groups   []string `json:"groups"`
	Sudo     string   `json:"sudo"`
	Password string   `json:"password"`
	SSHKey   string   `json:"ssh_key"`
	Confirm  bool     `json:"confirm"`
}

func validGroupList(groups []string) bool {
	if len(groups) > 50 {
		return false
	}
	for _, g := range groups {
		if !hostUserNameRe.MatchString(g) {
			return false
		}
	}
	return true
}

func (a *Admin) hostUserAdd(w http.ResponseWriter, r *http.Request, u *store.User) {
	s := a.serverOr404(w, r)
	if s == nil {
		return
	}
	var req hostUserAddReq
	if !decode(w, r, &req) {
		return
	}
	if !hostUserNameRe.MatchString(req.Name) || !validGroupList(req.Groups) || len(req.Password) > 128 || len(req.SSHKey) > 4096 ||
		len(req.Gecos) > 100 || len(req.Shell) > 100 || len(req.Home) > 200 {
		jsonErr(w, http.StatusBadRequest, "invalid request")
		return
	}
	if !req.Confirm {
		needConfirm(w)
		return
	}
	params := map[string]any{"name": req.Name, "gecos": req.Gecos, "shell": req.Shell, "home": req.Home, "groups": req.Groups,
		"sudo": req.Sudo, "password": req.Password, "ssh_key": req.SSHKey}
	details := map[string]any{"user": req.Name, "groups": req.Groups, "sudo": req.Sudo, "shell": req.Shell, "home": req.Home,
		"password_set": req.Password != "", "ssh_key": req.SSHKey != ""}
	a.secCall(w, r, u, s, "users", "add", "hostuser.add", params, details, 90*time.Second)
}

type hostUserUpdateReq struct {
	Groups  *[]string `json:"groups"`
	Sudo    *string   `json:"sudo"`
	Shell   *string   `json:"shell"`
	Locked  *bool     `json:"locked"`
	Confirm bool      `json:"confirm"`
}

func (a *Admin) hostUserUpdate(w http.ResponseWriter, r *http.Request, u *store.User) {
	s := a.serverOr404(w, r)
	if s == nil {
		return
	}
	name := r.PathValue("name")
	var req hostUserUpdateReq
	if !decode(w, r, &req) {
		return
	}
	if !hostUserNameRe.MatchString(name) || (req.Groups != nil && !validGroupList(*req.Groups)) ||
		(req.Shell != nil && len(*req.Shell) > 100) || (req.Sudo != nil && len(*req.Sudo) > 10) {
		jsonErr(w, http.StatusBadRequest, "invalid request")
		return
	}
	if !req.Confirm {
		needConfirm(w)
		return
	}
	params := map[string]any{"name": name}
	details := map[string]any{"user": name}
	if req.Groups != nil {
		params["groups"], details["groups"] = *req.Groups, *req.Groups
	}
	if req.Sudo != nil {
		params["sudo"], details["sudo"] = *req.Sudo, *req.Sudo
	}
	if req.Shell != nil {
		params["shell"], details["shell"] = *req.Shell, *req.Shell
	}
	if req.Locked != nil {
		params["locked"], details["locked"] = *req.Locked, *req.Locked
	}
	a.secCall(w, r, u, s, "users", "update", "hostuser.update", params, details, 60*time.Second)
}

func (a *Admin) hostUserDelete(w http.ResponseWriter, r *http.Request, u *store.User) {
	s := a.serverOr404(w, r)
	if s == nil {
		return
	}
	name := r.PathValue("name")
	var req struct {
		RemoveHome bool `json:"remove_home"`
		Confirm    bool `json:"confirm"`
	}
	if !decode(w, r, &req) {
		return
	}
	if !hostUserNameRe.MatchString(name) {
		jsonErr(w, http.StatusBadRequest, "invalid request")
		return
	}
	if !req.Confirm {
		needConfirm(w)
		return
	}
	a.secCall(w, r, u, s, "users", "delete", "hostuser.delete", map[string]any{"name": name, "remove_home": req.RemoveHome},
		map[string]any{"user": name, "remove_home": req.RemoveHome}, 120*time.Second)
}

func (a *Admin) hostUserPassword(w http.ResponseWriter, r *http.Request, u *store.User) {
	s := a.serverOr404(w, r)
	if s == nil {
		return
	}
	name := r.PathValue("name")
	var req struct {
		Password string `json:"password"`
		Confirm  bool   `json:"confirm"`
	}
	if !decode(w, r, &req) {
		return
	}
	if !hostUserNameRe.MatchString(name) || len(req.Password) < 8 || len(req.Password) > 128 || strings.ContainsAny(req.Password, "\r\n\x00") {
		jsonErr(w, http.StatusBadRequest, "invalid request")
		return
	}
	if !req.Confirm {
		needConfirm(w)
		return
	}
	a.secCall(w, r, u, s, "users", "passwd", "hostuser.password", map[string]any{"name": name, "password": req.Password},
		map[string]any{"user": name}, 40*time.Second)
}

func (a *Admin) hostUserKill(w http.ResponseWriter, r *http.Request, u *store.User) {
	s := a.serverOr404(w, r)
	if s == nil {
		return
	}
	name := r.PathValue("name")
	var req struct {
		Confirm bool `json:"confirm"`
	}
	if !decode(w, r, &req) {
		return
	}
	if !hostUserNameRe.MatchString(name) {
		jsonErr(w, http.StatusBadRequest, "invalid request")
		return
	}
	if !req.Confirm {
		needConfirm(w)
		return
	}
	a.secCall(w, r, u, s, "users", "kill", "hostuser.kill", map[string]any{"name": name}, map[string]any{"user": name}, 40*time.Second)
}
