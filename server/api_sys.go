package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/bolgov0zero/linager/internal/protocol"
	"github.com/bolgov0zero/linager/server/store"
)

const maxUpload = 512 << 20

// agentJSON calls the agent and decodes a JSON object answer.
func (a *Admin) agentJSON(ctx context.Context, serverID, module, action string, params any, dangerous bool, timeout time.Duration) (map[string]any, error) {
	raw, err := a.Hub.Call(ctx, serverID, module, action, params, dangerous, timeout)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func queryPath(w http.ResponseWriter, r *http.Request) (string, bool) {
	p := r.URL.Query().Get("path")
	if p == "" || !strings.HasPrefix(p, "/") || len(p) > 4096 {
		jsonErr(w, http.StatusBadRequest, "invalid path")
		return "", false
	}
	return p, true
}

// ---- files ----

func (a *Admin) filesList(w http.ResponseWriter, r *http.Request, _ *store.User) {
	p, ok := queryPath(w, r)
	if !ok {
		return
	}
	a.proxyCall(w, r, "files", "list", map[string]string{"path": p}, 30*time.Second)
}

func (a *Admin) filesContent(w http.ResponseWriter, r *http.Request, _ *store.User) {
	p, ok := queryPath(w, r)
	if !ok {
		return
	}
	a.proxyCall(w, r, "files", "read", map[string]string{"path": p}, 30*time.Second)
}

type pathReq struct {
	Path    string `json:"path"`
	Content string `json:"content"`
	MTime   int64  `json:"mtime"`
	Confirm bool   `json:"confirm"`
}

// simpleFileOp runs a files.* command from a JSON body and audits it.
func (a *Admin) simpleFileOp(action, auditAction string, needConfirmFlag bool) func(http.ResponseWriter, *http.Request, *store.User) {
	return func(w http.ResponseWriter, r *http.Request, u *store.User) {
		s := a.serverOr404(w, r)
		if s == nil {
			return
		}
		var req pathReq
		if !decode(w, r, &req) {
			return
		}
		if req.Path == "" || !strings.HasPrefix(req.Path, "/") {
			jsonErr(w, http.StatusBadRequest, "invalid path")
			return
		}
		if needConfirmFlag && !req.Confirm {
			needConfirm(w)
			return
		}
		params := map[string]any{"path": req.Path}
		if action == "write" {
			params["content"], params["mtime"] = req.Content, req.MTime
		}
		data, err := a.Hub.Call(r.Context(), s.ID, "files", action, params, needConfirmFlag, 60*time.Second)
		res := "ok"
		if err != nil {
			res = "failed"
		}
		a.audit(r, u, auditAction, "server", s.ID, s.Name, res, map[string]any{"path": req.Path})
		if err != nil {
			callErr(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(data)
	}
}

func (a *Admin) fileDownload(w http.ResponseWriter, r *http.Request, u *store.User) {
	s := a.serverOr404(w, r)
	if s == nil {
		return
	}
	p, ok := queryPath(w, r)
	if !ok {
		return
	}
	sh, err := a.Hub.OpenStream(r.Context(), s.ID, "files.download", map[string]string{"path": p})
	if err != nil {
		callErr(w, err)
		return
	}
	defer sh.Close()
	a.audit(r, u, "files.download", "server", s.ID, s.Name, "ok", map[string]any{"path": p})
	started := false
	for {
		select {
		case b := <-sh.Data:
			if !started {
				w.Header().Set("Content-Type", "application/octet-stream")
				w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, safeFile(path.Base(p))))
				started = true
			}
			if _, err := w.Write(b); err != nil {
				return
			}
		case <-sh.Done():
			for { // flush queued chunks
				select {
				case b := <-sh.Data:
					if !started {
						w.Header().Set("Content-Type", "application/octet-stream")
						w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, safeFile(path.Base(p))))
						started = true
					}
					_, _ = w.Write(b)
					continue
				default:
				}
				break
			}
			if reason := sh.Reason(); reason != "" {
				if !started {
					callErr(w, &AgentError{Msg: reason})
				}
				return
			}
			if !started { // empty file
				w.Header().Set("Content-Type", "application/octet-stream")
				w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, safeFile(path.Base(p))))
			}
			return
		case <-r.Context().Done():
			return
		}
	}
}

// fileStat returns the properties of one entry (type, size, times, permissions, owner).
func (a *Admin) fileStat(w http.ResponseWriter, r *http.Request, _ *store.User) {
	p, ok := queryPath(w, r)
	if !ok {
		return
	}
	a.proxyCall(w, r, "files", "stat", map[string]string{"path": p}, 30*time.Second)
}

// fileDu adds up the size of a folder.
func (a *Admin) fileDu(w http.ResponseWriter, r *http.Request, _ *store.User) {
	p, ok := queryPath(w, r)
	if !ok {
		return
	}
	a.proxyCall(w, r, "files", "du", map[string]string{"path": p}, 40*time.Second)
}

// fileChmod and fileChown change permissions and owner. The setuid, setgid and sticky bits, recursion and every
// owner change need an explicit confirmation.
func (a *Admin) filePerm(action string) func(http.ResponseWriter, *http.Request, *store.User) {
	return func(w http.ResponseWriter, r *http.Request, u *store.User) {
		s := a.serverOr404(w, r)
		if s == nil {
			return
		}
		var req struct {
			Path      string `json:"path"`
			Mode      string `json:"mode"`
			Owner     string `json:"owner"`
			Group     string `json:"group"`
			Recursive bool   `json:"recursive"`
			Confirm   bool   `json:"confirm"`
		}
		if !decode(w, r, &req) {
			return
		}
		if !strings.HasPrefix(req.Path, "/") || len(req.Path) > 4096 || len(req.Owner) > 64 || len(req.Group) > 64 {
			jsonErr(w, http.StatusBadRequest, "invalid path")
			return
		}
		params := map[string]any{"path": req.Path, "recursive": req.Recursive}
		danger := true
		details := map[string]any{"path": req.Path, "recursive": req.Recursive}
		if action == "chmod" {
			v, err := strconv.ParseUint(strings.TrimSpace(req.Mode), 8, 32)
			if err != nil || v > 0o7777 {
				jsonErr(w, http.StatusBadRequest, "invalid mode")
				return
			}
			params["mode"] = req.Mode
			details["mode"] = req.Mode
			danger = req.Recursive || v&0o7000 != 0
		} else {
			params["owner"], params["group"] = req.Owner, req.Group
			details["owner"], details["group"] = req.Owner, req.Group
		}
		if danger && !req.Confirm {
			needConfirm(w)
			return
		}
		data, err := a.Hub.Call(r.Context(), s.ID, "files", action, params, danger, 300*time.Second)
		res := "ok"
		if err != nil {
			res = "failed"
		}
		a.audit(r, u, "files."+action, "server", s.ID, s.Name, res, details)
		if err != nil {
			callErr(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(data)
	}
}

// fileRename renames an entry inside its directory.
func (a *Admin) fileRename(w http.ResponseWriter, r *http.Request, u *store.User) {
	s := a.serverOr404(w, r)
	if s == nil {
		return
	}
	var req struct {
		Path string `json:"path"`
		Name string `json:"name"`
	}
	if !decode(w, r, &req) {
		return
	}
	if req.Path == "" || !strings.HasPrefix(req.Path, "/") || req.Name == "" || strings.ContainsAny(req.Name, "/\x00") {
		jsonErr(w, http.StatusBadRequest, "invalid path")
		return
	}
	data, err := a.Hub.Call(r.Context(), s.ID, "files", "rename", map[string]any{"path": req.Path, "name": req.Name}, false, 30*time.Second)
	res := "ok"
	if err != nil {
		res = "failed"
	}
	a.audit(r, u, "files.rename", "server", s.ID, s.Name, res, map[string]any{"path": req.Path, "name": req.Name})
	if err != nil {
		callErr(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(data)
}

// fileTransfer copies or moves one entry into a directory. mode: fail (default), overwrite (needs confirm), keep.
func (a *Admin) fileTransfer(action string) func(http.ResponseWriter, *http.Request, *store.User) {
	return func(w http.ResponseWriter, r *http.Request, u *store.User) {
		s := a.serverOr404(w, r)
		if s == nil {
			return
		}
		var req struct {
			Path    string `json:"path"`
			Dir     string `json:"dir"`
			Mode    string `json:"mode"`
			Confirm bool   `json:"confirm"`
		}
		if !decode(w, r, &req) {
			return
		}
		if !strings.HasPrefix(req.Path, "/") || !strings.HasPrefix(req.Dir, "/") || len(req.Path) > 4096 || len(req.Dir) > 4096 {
			jsonErr(w, http.StatusBadRequest, "invalid path")
			return
		}
		if req.Mode != "" && req.Mode != "fail" && req.Mode != "overwrite" && req.Mode != "keep" {
			jsonErr(w, http.StatusBadRequest, "invalid mode")
			return
		}
		danger := req.Mode == "overwrite"
		if danger && !req.Confirm {
			needConfirm(w)
			return
		}
		data, err := a.Hub.Call(r.Context(), s.ID, "files", action, map[string]any{"src": req.Path, "dir": req.Dir, "mode": req.Mode}, danger, 180*time.Second)
		res := "ok"
		if err != nil {
			res = "failed"
		}
		a.audit(r, u, "files."+action, "server", s.ID, s.Name, res, map[string]any{"path": req.Path, "dir": req.Dir, "mode": req.Mode})
		if err != nil {
			callErr(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(data)
	}
}

// fileZip streams a zip archive of the named entries of a directory: ?dir=/x&name=a&name=b.
func (a *Admin) fileZip(w http.ResponseWriter, r *http.Request, u *store.User) {
	s := a.serverOr404(w, r)
	if s == nil {
		return
	}
	dir := r.URL.Query().Get("dir")
	names := r.URL.Query()["name"]
	if !strings.HasPrefix(dir, "/") || len(dir) > 4096 || len(names) == 0 || len(names) > 1000 {
		jsonErr(w, http.StatusBadRequest, "invalid path")
		return
	}
	for _, n := range names {
		if n == "" || n == "." || n == ".." || strings.ContainsAny(n, "/\x00") {
			jsonErr(w, http.StatusBadRequest, "invalid name")
			return
		}
	}
	sh, err := a.Hub.OpenStream(r.Context(), s.ID, "files.zip", map[string]any{"dir": dir, "names": names})
	if err != nil {
		callErr(w, err)
		return
	}
	defer sh.Close()
	zipName := "files.zip"
	if len(names) == 1 {
		zipName = names[0] + ".zip"
	} else if b := path.Base(dir); b != "/" && b != "." {
		zipName = b + ".zip"
	}
	a.audit(r, u, "files.zip", "server", s.ID, s.Name, "ok", map[string]any{"dir": dir, "count": len(names)})
	started := false
	start := func() {
		if started {
			return
		}
		started = true
		w.Header().Set("Content-Type", "application/zip")
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"; filename*=UTF-8''%s`, safeFile(zipName), url.PathEscape(zipName)))
	}
	for {
		select {
		case b := <-sh.Data:
			start()
			if _, err := w.Write(b); err != nil {
				return
			}
		case <-sh.Done():
			for {
				select {
				case b := <-sh.Data:
					start()
					_, _ = w.Write(b)
					continue
				default:
				}
				break
			}
			if reason := sh.Reason(); reason != "" && !started {
				callErr(w, &AgentError{Msg: reason})
			}
			return
		case <-r.Context().Done():
			return
		}
	}
}

func (a *Admin) fileUpload(w http.ResponseWriter, r *http.Request, u *store.User) {
	s := a.serverOr404(w, r)
	if s == nil {
		return
	}
	p, ok := queryPath(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	sh, err := a.Hub.OpenStream(ctx, s.ID, "files.upload", map[string]string{"path": p})
	if err != nil {
		callErr(w, err)
		return
	}
	defer sh.Close()
	body := http.MaxBytesReader(w, r.Body, maxUpload)
	buf := make([]byte, 24<<10)
	var total int64
	for {
		n, rerr := body.Read(buf)
		if n > 0 {
			total += int64(n)
			if err := sh.Input(ctx, append([]byte(nil), buf[:n]...)); err != nil {
				callErr(w, err)
				return
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			jsonErr(w, http.StatusRequestEntityTooLarge, protocol.ErrTooLarge)
			return
		}
	}
	if err := sh.Input(ctx, []byte{}); err != nil { // end of file marker
		callErr(w, err)
		return
	}
	select {
	case <-sh.Done():
	case <-time.After(60 * time.Second):
		callErr(w, ErrCallTimeout)
		return
	}
	res := "ok"
	if reason := sh.Reason(); reason != "" {
		a.audit(r, u, "files.upload", "server", s.ID, s.Name, "failed", map[string]any{"path": p, "error": reason})
		callErr(w, &AgentError{Msg: reason})
		return
	}
	a.audit(r, u, "files.upload", "server", s.ID, s.Name, res, map[string]any{"path": p, "bytes": total})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "size": total})
}

// ---- pending changes (network, firewall) ----

type pendingView struct {
	ID       string `json:"id"`
	Kind     string `json:"kind"`
	Deadline int64  `json:"deadline"`
}

// pendingOf returns the live pending change of a server; stale rows are removed.
func (a *Admin) pendingOf(ctx context.Context, serverID, kind string) *pendingView {
	p, err := a.DB.PendingFor(ctx, serverID)
	if err != nil {
		return nil
	}
	if time.Now().After(p.Deadline.Add(10 * time.Second)) { // the agent has rolled it back by now
		_ = a.DB.ClearPending(ctx, serverID)
		return nil
	}
	if kind != "" && p.Kind != kind {
		return nil
	}
	return &pendingView{ID: p.ChangeID, Kind: p.Kind, Deadline: p.Deadline.Unix()}
}

func (a *Admin) draftOf(ctx context.Context, serverID, kind string) json.RawMessage {
	b, err := a.DB.Draft(ctx, serverID, kind)
	if err != nil {
		return nil
	}
	return b
}

func (a *Admin) resetDraft(kind string) func(http.ResponseWriter, *http.Request, *store.User) {
	return func(w http.ResponseWriter, r *http.Request, u *store.User) {
		s := a.serverOr404(w, r)
		if s == nil {
			return
		}
		_ = a.DB.DeleteDraft(r.Context(), s.ID, kind)
		a.audit(r, u, kind+".draft.reset", "server", s.ID, s.Name, "ok", nil)
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	}
}

// confirmOrRollback handles POST /{kind}/confirm and /rollback.
func (a *Admin) confirmOrRollback(kind, action string) func(http.ResponseWriter, *http.Request, *store.User) {
	return func(w http.ResponseWriter, r *http.Request, u *store.User) {
		s := a.serverOr404(w, r)
		if s == nil {
			return
		}
		var req struct {
			ID string `json:"id"`
		}
		if !decode(w, r, &req) {
			return
		}
		pv := a.pendingOf(r.Context(), s.ID, kind)
		if pv == nil || pv.ID != req.ID {
			jsonErr(w, http.StatusGone, msg(r, "Изменение уже подтверждено или откачено", "The change was already confirmed or rolled back"))
			return
		}
		_, err := a.Hub.Call(r.Context(), s.ID, "changes", action, map[string]string{"id": req.ID}, true, 40*time.Second)
		var ae *AgentError
		if errors.As(err, &ae) && ae.Msg == protocol.ErrNoPending {
			_ = a.DB.ClearPending(r.Context(), s.ID)
			jsonErr(w, http.StatusGone, msg(r, "Изменение уже откатилось", "The change was already rolled back"))
			return
		}
		res := "ok"
		if err != nil {
			res = "failed"
		}
		a.audit(r, u, kind+"."+action, "server", s.ID, s.Name, res, map[string]any{"change": req.ID})
		if err != nil {
			callErr(w, err)
			return
		}
		_ = a.DB.ClearPending(r.Context(), s.ID)
		if action == "confirm" {
			_ = a.DB.DeleteDraft(r.Context(), s.ID, kind)
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	}
}

// startChange records the pending row and sends the apply command; linkMayDrop tolerates a lost answer.
func (a *Admin) startChange(w http.ResponseWriter, r *http.Request, s *store.Server, kind, module string, params map[string]any, linkMayDrop bool) (*pendingView, bool) {
	id := protocol.NewID()
	deadline := time.Now().Add(protocol.ChangeWindow)
	params["id"] = id
	if err := a.DB.SetPending(r.Context(), s.ID, store.Pending{Kind: kind, ChangeID: id, Deadline: deadline}); err != nil {
		jsonErr(w, http.StatusInternalServerError, "internal error")
		return nil, false
	}
	_, err := a.Hub.Call(r.Context(), s.ID, module, "apply", params, true, 25*time.Second)
	if err != nil {
		lost := errors.Is(err, ErrCallTimeout) || errors.Is(err, ErrAgentDisconect) || errors.Is(err, context.DeadlineExceeded)
		if !linkMayDrop || !lost {
			_ = a.DB.ClearPending(r.Context(), s.ID)
			callErr(w, err)
			return nil, false
		}
	}
	return &pendingView{ID: id, Kind: kind, Deadline: deadline.Unix()}, true
}

// ---- network ----

type netDraft struct {
	Changes []NetChange `json:"changes"`
}

func (a *Admin) netIfaces(ctx context.Context, serverID string) (map[string]any, []netIfaceInfo, error) {
	st, err := a.agentJSON(ctx, serverID, "network", "state", nil, false, 30*time.Second)
	if err != nil {
		return nil, nil, err
	}
	var ifs []netIfaceInfo
	if b, err := json.Marshal(st["interfaces"]); err == nil {
		_ = json.Unmarshal(b, &ifs)
	}
	return st, ifs, nil
}

func (a *Admin) networkState(w http.ResponseWriter, r *http.Request, _ *store.User) {
	s := a.serverOr404(w, r)
	if s == nil {
		return
	}
	st, _, err := a.netIfaces(r.Context(), s.ID)
	if err != nil {
		callErr(w, err)
		return
	}
	st["draft"] = a.draftOf(r.Context(), s.ID, "network")
	st["pending"] = a.pendingOf(r.Context(), s.ID, "network")
	writeJSON(w, http.StatusOK, st)
}

func (a *Admin) networkValidate(w http.ResponseWriter, r *http.Request, _ *store.User) {
	s := a.serverOr404(w, r)
	if s == nil {
		return
	}
	var d netDraft
	if !decode(w, r, &d) {
		return
	}
	_, ifs, err := a.netIfaces(r.Context(), s.ID)
	if err != nil {
		callErr(w, err)
		return
	}
	errs := validateNetDraft(d.Changes, ifs, func(ru, en string) string { return msg(r, ru, en) })
	if errs == nil {
		errs = []FieldError{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"errors": errs})
}

func (a *Admin) networkSaveDraft(w http.ResponseWriter, r *http.Request, u *store.User) {
	s := a.serverOr404(w, r)
	if s == nil {
		return
	}
	var d netDraft
	if !decode(w, r, &d) {
		return
	}
	_, ifs, err := a.netIfaces(r.Context(), s.ID)
	if err != nil {
		callErr(w, err)
		return
	}
	if errs := validateNetDraft(d.Changes, ifs, func(ru, en string) string { return msg(r, ru, en) }); len(errs) > 0 {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"error": "invalid draft", "errors": errs})
		return
	}
	b, _ := json.Marshal(d)
	if err := a.DB.SaveDraft(r.Context(), s.ID, "network", b); err != nil {
		jsonErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	a.audit(r, u, "network.draft.save", "server", s.ID, s.Name, "ok", map[string]any{"interfaces": len(d.Changes)})
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *Admin) networkApply(w http.ResponseWriter, r *http.Request, u *store.User) {
	s := a.serverOr404(w, r)
	if s == nil {
		return
	}
	var req confirmReq
	if !decode(w, r, &req) {
		return
	}
	raw := a.draftOf(r.Context(), s.ID, "network")
	var d netDraft
	if raw == nil || json.Unmarshal(raw, &d) != nil || len(d.Changes) == 0 {
		jsonErr(w, http.StatusBadRequest, msg(r, "Нет черновика", "There is no draft"))
		return
	}
	if a.pendingOf(r.Context(), s.ID, "") != nil {
		jsonErr(w, http.StatusConflict, msg(r, "Есть неподтверждённое изменение", "Another change is waiting for confirmation"))
		return
	}
	_, ifs, err := a.netIfaces(r.Context(), s.ID)
	if err != nil {
		callErr(w, err)
		return
	}
	text := func(ru, en string) string { return msg(r, ru, en) }
	if errs := validateNetDraft(d.Changes, ifs, text); len(errs) > 0 {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"error": "invalid draft", "errors": errs})
		return
	}
	warns := netWarnings(d.Changes, ifs, text)
	if len(warns) > 0 && !req.Confirm {
		writeJSON(w, http.StatusConflict, map[string]any{"error": "confirmation required", "dangerous": true, "warnings": warns})
		return
	}
	pv, ok := a.startChange(w, r, s, "network", "network", map[string]any{"changes": d.Changes}, len(warns) > 0)
	a.audit(r, u, "network.apply", "server", s.ID, s.Name, map[bool]string{true: "ok", false: "failed"}[ok], map[string]any{"changes": d.Changes, "warnings": len(warns)})
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, pv)
}

var netplanFileRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,60}\.yaml$`)

// networkApplyConfig applies a hand-edited interfaces file through the same pending/rollback flow.
func (a *Admin) networkApplyConfig(w http.ResponseWriter, r *http.Request, u *store.User) {
	s := a.serverOr404(w, r)
	if s == nil {
		return
	}
	var req struct {
		Text    string `json:"text"`
		File    string `json:"file"` // netplan file name; empty for /etc/network/interfaces
		Confirm bool   `json:"confirm"`
	}
	if !decode(w, r, &req) {
		return
	}
	if req.File != "" && !netplanFileRe.MatchString(req.File) {
		jsonErr(w, http.StatusBadRequest, msg(r, "Недопустимое имя файла", "Invalid file name"))
		return
	}
	if !req.Confirm {
		jsonErr(w, http.StatusBadRequest, msg(r, "Нужно подтверждение", "Confirmation required"))
		return
	}
	if strings.TrimSpace(req.Text) == "" || len(req.Text) > 64*1024 {
		jsonErr(w, http.StatusBadRequest, msg(r, "Файл пуст или слишком большой (до 64 КБ)", "The file is empty or too large (64 KB max)"))
		return
	}
	if a.pendingOf(r.Context(), s.ID, "") != nil {
		jsonErr(w, http.StatusConflict, msg(r, "Есть неподтверждённое изменение", "Another change is waiting for confirmation"))
		return
	}
	pv, ok := a.startChange(w, r, s, "network", "network", map[string]any{"raw": req.Text, "file": req.File}, true)
	a.audit(r, u, "network.config.apply", "server", s.ID, s.Name, map[bool]string{true: "ok", false: "failed"}[ok], map[string]any{"file": req.File, "bytes": len(req.Text), "lines": strings.Count(req.Text, "\n") + 1})
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, pv)
}

// ---- firewall ----

type fwDraftReq struct {
	Sets   []FwDraftSet   `json:"sets"`
	Chains []FwDraftChain `json:"chains"`
}

func (a *Admin) firewallState(w http.ResponseWriter, r *http.Request, _ *store.User) {
	s := a.serverOr404(w, r)
	if s == nil {
		return
	}
	st, err := a.agentJSON(r.Context(), s.ID, "firewall", "state", nil, false, 30*time.Second)
	if err != nil {
		callErr(w, err)
		return
	}
	st["draft"] = a.draftOf(r.Context(), s.ID, "firewall")
	st["pending"] = a.pendingOf(r.Context(), s.ID, "firewall")
	writeJSON(w, http.StatusOK, st)
}

// firewallPorts returns the listening ports with the state of the firewall for each of them.
func (a *Admin) firewallPorts(w http.ResponseWriter, r *http.Request, _ *store.User) {
	s := a.serverOr404(w, r)
	if s == nil {
		return
	}
	data, err := a.Hub.Call(r.Context(), s.ID, "firewall", "ports", nil, false, 30*time.Second)
	if err != nil {
		callErr(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(data)
}

// firewallLog returns what the rules marked for logging matched (read from the kernel journal by the agent).
func (a *Admin) firewallLog(w http.ResponseWriter, r *http.Request, _ *store.User) {
	a.proxyCall(w, r, "firewall", "log", nil, 40*time.Second)
}

func (a *Admin) firewallValidate(w http.ResponseWriter, r *http.Request, _ *store.User) {
	s := a.serverOr404(w, r)
	if s == nil {
		return
	}
	var d fwDraftReq
	if !decode(w, r, &d) {
		return
	}
	out, err := a.agentJSON(r.Context(), s.ID, "firewall", "validate", d, false, 30*time.Second)
	if err != nil {
		callErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *Admin) firewallSaveDraft(w http.ResponseWriter, r *http.Request, u *store.User) {
	s := a.serverOr404(w, r)
	if s == nil {
		return
	}
	var d fwDraftReq
	if !decode(w, r, &d) {
		return
	}
	if len(d.Sets) > 100 {
		jsonErr(w, http.StatusUnprocessableEntity, "invalid draft")
		return
	}
	for _, set := range d.Sets {
		if len(set.Elements) > 5000 {
			jsonErr(w, http.StatusUnprocessableEntity, "invalid draft")
			return
		}
	}
	for _, c := range d.Chains {
		if (c.Name != "input" && c.Name != "forward" && c.Name != "output") || (c.Policy != "accept" && c.Policy != "drop") || len(c.Rules) > 500 {
			jsonErr(w, http.StatusUnprocessableEntity, "invalid draft")
			return
		}
	}
	b, _ := json.Marshal(d)
	if err := a.DB.SaveDraft(r.Context(), s.ID, "firewall", b); err != nil {
		jsonErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	a.audit(r, u, "firewall.draft.save", "server", s.ID, s.Name, "ok", nil)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *Admin) firewallApply(w http.ResponseWriter, r *http.Request, u *store.User) {
	s := a.serverOr404(w, r)
	if s == nil {
		return
	}
	var req confirmReq
	if !decode(w, r, &req) {
		return
	}
	raw := a.draftOf(r.Context(), s.ID, "firewall")
	var d fwDraftReq
	if raw == nil || json.Unmarshal(raw, &d) != nil || len(d.Chains) == 0 {
		jsonErr(w, http.StatusBadRequest, msg(r, "Нет черновика", "There is no draft"))
		return
	}
	if a.pendingOf(r.Context(), s.ID, "") != nil {
		jsonErr(w, http.StatusConflict, msg(r, "Есть неподтверждённое изменение", "Another change is waiting for confirmation"))
		return
	}
	// The agent reports the ports sshd really listens on; older agents do not, then port 22 is assumed.
	var fst struct {
		SSHPorts []int `json:"ssh_ports"`
	}
	if data, err := a.Hub.Call(r.Context(), s.ID, "firewall", "state", nil, false, 30*time.Second); err == nil {
		_ = json.Unmarshal(data, &fst)
	}
	warns := fwWarnings(d.Chains, fst.SSHPorts, func(ru, en string) string { return msg(r, ru, en) })
	if len(warns) > 0 && !req.Confirm {
		writeJSON(w, http.StatusConflict, map[string]any{"error": "confirmation required", "dangerous": true, "warnings": warns})
		return
	}
	pv, ok := a.startChange(w, r, s, "firewall", "firewall", map[string]any{"chains": d.Chains, "sets": d.Sets}, len(warns) > 0)
	a.audit(r, u, "firewall.apply", "server", s.ID, s.Name, map[bool]string{true: "ok", false: "failed"}[ok], map[string]any{"warnings": len(warns)})
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, pv)
}

// ---- docker ----

func (a *Admin) dockerState(w http.ResponseWriter, r *http.Request, _ *store.User) {
	s := a.serverOr404(w, r)
	if s == nil {
		return
	}
	data, err := a.Hub.Call(r.Context(), s.ID, "docker", "state", nil, false, 45*time.Second)
	var ae *AgentError
	if errors.As(err, &ae) && ae.Msg == protocol.ErrModuleDisabled { // no docker on this host
		writeJSON(w, http.StatusOK, map[string]any{"available": false, "containers": []any{}, "images": []any{}})
		return
	}
	if err != nil {
		callErr(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(data)
}

func (a *Admin) dockerContainerAction(w http.ResponseWriter, r *http.Request, u *store.User) {
	s := a.serverOr404(w, r)
	if s == nil {
		return
	}
	action := r.PathValue("action")
	var req confirmReq
	if !decode(w, r, &req) {
		return
	}
	if action == "remove" && !req.Confirm {
		needConfirm(w)
		return
	}
	cid := r.PathValue("cid")
	_, err := a.Hub.Call(r.Context(), s.ID, "docker", "container", map[string]string{"id": cid, "action": action}, action == "remove", 60*time.Second)
	res := "ok"
	if err != nil {
		res = "failed"
	}
	a.audit(r, u, "docker.container."+action, "server", s.ID, s.Name, res, map[string]any{"container": cid})
	if err != nil {
		callErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// dockerCheck asks the agent to compare the images of the containers with the registries.
func (a *Admin) dockerCheck(w http.ResponseWriter, r *http.Request, u *store.User) {
	s := a.serverOr404(w, r)
	if s == nil {
		return
	}
	data, err := a.Hub.Call(r.Context(), s.ID, "docker", "check", nil, false, 130*time.Second)
	res := "ok"
	if err != nil {
		res = "failed"
	}
	a.audit(r, u, "docker.check", "server", s.ID, s.Name, res, nil)
	if err != nil {
		callErr(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(data)
}

func (a *Admin) dockerRemoveImage(w http.ResponseWriter, r *http.Request, u *store.User) {
	s := a.serverOr404(w, r)
	if s == nil {
		return
	}
	id := r.PathValue("iid")
	_, err := a.Hub.Call(r.Context(), s.ID, "docker", "imageRemove", map[string]string{"id": id}, false, 60*time.Second)
	res := "ok"
	if err != nil {
		res = "failed"
	}
	a.audit(r, u, "docker.image.remove", "server", s.ID, s.Name, res, map[string]any{"image": id})
	if err != nil {
		callErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
