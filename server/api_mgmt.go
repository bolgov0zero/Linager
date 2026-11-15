package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/coder/websocket"

	"github.com/bolgov0zero/linager/agent"
	"github.com/bolgov0zero/linager/internal/protocol"
	"github.com/bolgov0zero/linager/server/store"
)

// callErr maps agent-call errors to HTTP responses.
func callErr(w http.ResponseWriter, err error) {
	var ae *AgentError
	switch {
	case errors.Is(err, ErrOffline), errors.Is(err, ErrAgentDisconect):
		jsonErr(w, http.StatusServiceUnavailable, "agent is offline")
	case errors.Is(err, ErrCallTimeout):
		jsonErr(w, http.StatusGatewayTimeout, "agent did not answer in time")
	case errors.As(err, &ae) && ae.Msg == protocol.ErrModuleDisabled:
		jsonErr(w, http.StatusNotImplemented, protocol.ErrModuleDisabled)
	case errors.As(err, &ae) && ae.Msg == protocol.ErrBusy:
		jsonErr(w, http.StatusConflict, protocol.ErrBusy)
	case errors.As(err, &ae) && ae.Msg == protocol.ErrTooLarge:
		jsonErr(w, http.StatusRequestEntityTooLarge, ae.Msg)
	case errors.As(err, &ae) && ae.Msg == protocol.ErrBinary:
		jsonErr(w, http.StatusUnsupportedMediaType, ae.Msg)
	case errors.As(err, &ae) && (ae.Msg == protocol.ErrConflict || ae.Msg == protocol.ErrExists):
		jsonErr(w, http.StatusConflict, ae.Msg)
	case errors.As(err, &ae) && ae.Msg == protocol.ErrNotFound:
		jsonErr(w, http.StatusNotFound, ae.Msg)
	case errors.As(err, &ae) && ae.Msg == "permission denied":
		jsonErr(w, http.StatusForbidden, ae.Msg)
	case errors.As(err, &ae) && ae.Msg == protocol.ErrNoPending:
		jsonErr(w, http.StatusGone, ae.Msg)
	case errors.As(err, &ae) && dockerConflict(ae.Msg):
		jsonErr(w, http.StatusConflict, ae.Msg)
	case errors.As(err, &ae):
		jsonErr(w, http.StatusBadGateway, ae.Msg)
	default:
		jsonErr(w, http.StatusInternalServerError, "internal error")
	}
}

func dockerConflict(m string) bool {
	m = strings.ToLower(m)
	return strings.Contains(m, "conflict") || strings.Contains(m, "being used") || strings.Contains(m, "in use")
}

func (a *Admin) serverOr404(w http.ResponseWriter, r *http.Request) *store.Server {
	s, err := a.DB.ServerByID(r.Context(), r.PathValue("id"))
	if err != nil {
		jsonErr(w, http.StatusNotFound, "server not found")
		return nil
	}
	return s
}

// proxyCall runs a read-only command and writes the agent's JSON answer through.
func (a *Admin) proxyCall(w http.ResponseWriter, r *http.Request, module, action string, params any, timeout time.Duration) {
	s := a.serverOr404(w, r)
	if s == nil {
		return
	}
	data, err := a.Hub.Call(r.Context(), s.ID, module, action, params, false, timeout)
	if err != nil {
		callErr(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(data)
}

func (a *Admin) servicesList(w http.ResponseWriter, r *http.Request, _ *store.User) {
	a.proxyCall(w, r, "services", "list", nil, 30*time.Second)
}

// servicesIgnored returns the units whose failure does not count towards the server status.
func (a *Admin) servicesIgnored(w http.ResponseWriter, r *http.Request, _ *store.User) {
	s := a.serverOr404(w, r)
	if s == nil {
		return
	}
	list, err := a.DB.IgnoredServices(r.Context(), s.ID)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ignored": list})
}

// servicesIgnore adds a unit to the ignore list or removes it.
func (a *Admin) servicesIgnore(w http.ResponseWriter, r *http.Request, u *store.User) {
	s := a.serverOr404(w, r)
	if s == nil {
		return
	}
	var req struct {
		Unit   string `json:"unit"`
		Ignore bool   `json:"ignore"`
	}
	if !decode(w, r, &req) {
		return
	}
	if req.Unit == "" || len(req.Unit) > 256 || strings.ContainsAny(req.Unit, " \t\n/") {
		jsonErr(w, http.StatusBadRequest, "invalid unit")
		return
	}
	if err := a.DB.SetServiceIgnored(r.Context(), s.ID, req.Unit, req.Ignore); err != nil {
		jsonErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	list, _ := a.DB.IgnoredServices(r.Context(), s.ID)
	a.Metrics.SetIgnored(r.Context(), s.ID, list)
	action := "service.ignore"
	if !req.Ignore {
		action = "service.unignore"
	}
	a.audit(r, u, action, "server", s.ID, s.Name, "ok", map[string]any{"unit": req.Unit})
	a.Live.Publish("dashboard", map[string]any{})
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *Admin) aptList(w http.ResponseWriter, r *http.Request, _ *store.User) {
	a.proxyCall(w, r, "apt", "list", nil, 90*time.Second)
}

func (a *Admin) processesList(w http.ResponseWriter, r *http.Request, _ *store.User) {
	sortBy := r.URL.Query().Get("sort")
	if sortBy != "mem" {
		sortBy = "cpu"
	}
	n, _ := strconv.Atoi(r.URL.Query().Get("n"))
	a.proxyCall(w, r, "processes", "list", map[string]any{"sort": sortBy, "n": n}, 20*time.Second)
}

type confirmReq struct {
	Confirm bool   `json:"confirm"`
	Signal  string `json:"signal"`
}

func needConfirm(w http.ResponseWriter) {
	writeJSON(w, http.StatusConflict, map[string]any{"error": "confirmation required", "dangerous": true})
}

var unitNameRe = regexp.MustCompile(`^[a-zA-Z0-9:_.@-]{1,120}\.service$`)

func (a *Admin) serviceAction(w http.ResponseWriter, r *http.Request, u *store.User) {
	s := a.serverOr404(w, r)
	if s == nil {
		return
	}
	unit, action := r.PathValue("unit"), r.PathValue("action")
	if !unitNameRe.MatchString(unit) {
		jsonErr(w, http.StatusBadRequest, "invalid unit")
		return
	}
	switch action {
	case "start", "stop", "restart", "enable", "disable":
	default:
		jsonErr(w, http.StatusBadRequest, "invalid action")
		return
	}
	var req confirmReq
	if !decode(w, r, &req) {
		return
	}
	danger := agent.IsDangerousService(unit, action)
	if danger && !req.Confirm {
		needConfirm(w)
		return
	}
	_, err := a.Hub.Call(r.Context(), s.ID, "services", "action", map[string]string{"unit": unit, "action": action}, danger, 60*time.Second)
	res := "ok"
	if err != nil {
		res = "failed"
	}
	a.audit(r, u, "service."+action, "server", s.ID, s.Name, res, map[string]any{"unit": unit, "dangerous": danger})
	if err != nil {
		callErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *Admin) reboot(w http.ResponseWriter, r *http.Request, u *store.User) {
	s := a.serverOr404(w, r)
	if s == nil {
		return
	}
	var req confirmReq
	if !decode(w, r, &req) {
		return
	}
	if !req.Confirm {
		needConfirm(w)
		return
	}
	_, err := a.Hub.Call(r.Context(), s.ID, "system", "reboot", nil, true, 20*time.Second)
	res := "ok"
	if err != nil {
		res = "failed"
	}
	a.audit(r, u, "server.reboot", "server", s.ID, s.Name, res, nil)
	if err != nil {
		callErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *Admin) killProcess(w http.ResponseWriter, r *http.Request, u *store.User) {
	s := a.serverOr404(w, r)
	if s == nil {
		return
	}
	pid, err := strconv.Atoi(r.PathValue("pid"))
	if err != nil || pid <= 1 {
		jsonErr(w, http.StatusBadRequest, "invalid pid")
		return
	}
	var req confirmReq
	if !decode(w, r, &req) {
		return
	}
	if !req.Confirm {
		needConfirm(w)
		return
	}
	if req.Signal != "KILL" {
		req.Signal = "TERM"
	}
	_, err = a.Hub.Call(r.Context(), s.ID, "processes", "kill", map[string]any{"pid": pid, "signal": req.Signal}, true, 20*time.Second)
	res := "ok"
	if err != nil {
		res = "failed"
	}
	a.audit(r, u, "process.kill", "server", s.ID, s.Name, res, map[string]any{"pid": pid, "signal": req.Signal})
	if err != nil {
		callErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// ---- streams (terminal, logs, apt output) over WebSocket ----

var (
	logUnitRe = regexp.MustCompile(`^[a-zA-Z0-9:_.@*-]{1,100}$`)
	termLimit = 3 // simultaneous terminals per server
)

// termLimitCode is the WebSocket close code sent when the terminal limit for a server is reached.
const termLimitCode websocket.StatusCode = 4029

func atoiClamp(s string, lo, hi, def int) int {
	n, err := strconv.Atoi(s)
	if err != nil || n < lo || n > hi {
		return def
	}
	return n
}

func (a *Admin) streamParams(r *http.Request) (module string, params map[string]any, ok bool) {
	q := r.URL.Query()
	switch q.Get("kind") {
	case "terminal":
		return "terminal", map[string]any{"cols": atoiClamp(q.Get("cols"), 1, 500, 80), "rows": atoiClamp(q.Get("rows"), 1, 500, 24)}, true
	case "logs":
		p := map[string]any{"lines": atoiClamp(q.Get("lines"), 0, 1000, 200)}
		if u := q.Get("unit"); u != "" {
			if !logUnitRe.MatchString(u) {
				return "", nil, false
			}
			p["unit"] = u
		}
		if pr := q.Get("priority"); pr != "" {
			p["priority"] = strconv.Itoa(atoiClamp(pr, 0, 7, 6))
		}
		if q.Get("kernel") == "1" {
			p["kernel"] = "1"
		}
		return "logs", p, true
	case "docker-logs":
		if c := q.Get("container"); c != "" && len(c) <= 128 {
			return "docker.logs", map[string]any{"container": c, "lines": atoiClamp(q.Get("lines"), 0, 1000, 200)}, true
		}
	case "docker-pull":
		if img := q.Get("image"); img != "" && len(img) <= 256 {
			return "docker.pull", map[string]any{"image": img}, true
		}
	case "docker-update":
		if c := q.Get("container"); c != "" && len(c) <= 128 && len(q.Get("image")) <= 256 {
			p := map[string]any{"container": c}
			if img := q.Get("image"); img != "" {
				p["image"] = img
			}
			return "docker.update", p, true
		}
	case "apt":
		switch m := q.Get("mode"); m {
		case "update", "upgrade", "security":
			return "apt", map[string]any{"mode": m}, true
		}
	}
	return "", nil, false
}

func (a *Admin) streamWS(w http.ResponseWriter, r *http.Request, u *store.User) {
	s := a.serverOr404(w, r)
	if s == nil {
		return
	}
	module, params, ok := a.streamParams(r)
	if !ok {
		jsonErr(w, http.StatusBadRequest, "invalid stream parameters")
		return
	}
	if !a.Hub.Online(s.ID) {
		jsonErr(w, http.StatusServiceUnavailable, "agent is offline")
		return
	}
	if module == "terminal" && !a.termSlot(s.ID, +1) {
		// Browsers cannot read the status of a failed WebSocket handshake, so the limit is reported
		// as close code 4029 on an accepted socket (Accept answers non-WebSocket requests itself).
		if ws, err := websocket.Accept(w, r, nil); err == nil {
			_ = ws.Close(termLimitCode, "too many open terminals")
		}
		return
	}
	releaseTerm := func() {
		if module == "terminal" {
			a.termSlot(s.ID, -1)
		}
	}
	ws, err := websocket.Accept(w, r, nil) // same-origin check is on by default
	if err != nil {
		releaseTerm()
		return
	}
	defer releaseTerm()
	ws.SetReadLimit(1 << 20)
	ctx, cancel := context.WithCancel(context.WithoutCancel(r.Context()))
	defer cancel()

	sh, err := a.Hub.OpenStream(ctx, s.ID, module, params)
	if err != nil {
		_ = ws.Close(websocket.StatusInternalError, "cannot open stream")
		return
	}
	defer sh.Close()
	start := time.Now()
	var bytesIn, bytesOut int64
	a.audit(r, u, "stream.open", "server", s.ID, s.Name, "ok", map[string]any{"kind": module, "params": params, "sid": sh.SID})
	defer func() {
		a.audit(r, u, "stream.close", "server", s.ID, s.Name, "ok", map[string]any{"kind": module, "sid": sh.SID,
			"seconds": int(time.Since(start).Seconds()), "bytes_in": bytesIn, "bytes_out": bytesOut})
	}()

	go func() { // browser -> agent
		defer cancel()
		for {
			typ, b, err := ws.Read(ctx)
			if err != nil {
				return
			}
			if module != "terminal" {
				continue // logs and apt are output-only
			}
			if typ == websocket.MessageBinary {
				bytesIn += int64(len(b))
				if sh.Input(ctx, b) != nil {
					return
				}
				continue
			}
			var m struct {
				Resize *struct{ Cols, Rows int } `json:"resize"`
			}
			if json.Unmarshal(b, &m) == nil && m.Resize != nil {
				_ = sh.Resize(ctx, atoiClamp(strconv.Itoa(m.Resize.Cols), 1, 500, 80), atoiClamp(strconv.Itoa(m.Resize.Rows), 1, 500, 24))
			}
		}
	}()
	for { // agent -> browser
		select {
		case b := <-sh.Data:
			bytesOut += int64(len(b))
			if ws.Write(ctx, websocket.MessageBinary, b) != nil {
				return
			}
		case <-sh.Done():
			for { // flush what is already queued
				select {
				case b := <-sh.Data:
					_ = ws.Write(ctx, websocket.MessageBinary, b)
					continue
				default:
				}
				break
			}
			msg, _ := json.Marshal(map[string]string{"closed": sh.Reason()})
			_ = ws.Write(ctx, websocket.MessageText, msg)
			_ = ws.Close(websocket.StatusNormalClosure, "")
			return
		case <-ctx.Done():
			return
		}
	}
}

// termSlot counts open terminals per server; delta +1 reserves, -1 releases.
func (a *Admin) termSlot(serverID string, delta int) bool {
	a.termMu.Lock()
	defer a.termMu.Unlock()
	if delta > 0 {
		if a.terms[serverID] >= termLimit {
			return false
		}
		a.terms[serverID]++
		return true
	}
	if a.terms[serverID] > 0 {
		a.terms[serverID]--
	}
	return true
}
