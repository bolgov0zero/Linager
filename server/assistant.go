package server

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"time"

	"github.com/coder/websocket"

	"github.com/bolgov0zero/linager/internal/assist"
	"github.com/bolgov0zero/linager/server/store"
)

// assistLimitCode is the WebSocket close code sent when the Assistant of this session is already open: one person
// at a time sees and controls a session.
const assistLimitCode websocket.StatusCode = 4031

// assistSessionRe is the session the browser asks for: the one at the console of the computer or a number of Windows.
var assistSessionRe = regexp.MustCompile(`^(console|[0-9]{1,6})$`)

// assistSlot counts the open Assistants per session of a computer; delta +1 reserves, -1 releases.
func (a *Admin) assistSlot(serverID, session string, delta int) bool {
	key := serverID + "/" + session
	a.termMu.Lock()
	defer a.termMu.Unlock()
	if a.assists == nil {
		a.assists = map[string]int{}
	}
	if delta > 0 {
		if a.assists[key] >= 1 {
			return false
		}
		a.assists[key]++
		return true
	}
	if a.assists[key] > 0 {
		a.assists[key]--
	}
	return true
}

// assistLiveSet adds or removes (add false) a stream of an open Assistant, so a change of the settings reaches it.
func (a *Admin) assistLiveSet(serverID string, sh *StreamHandle, add bool) {
	a.termMu.Lock()
	defer a.termMu.Unlock()
	if a.assistLive == nil {
		a.assistLive = map[string]map[*StreamHandle]struct{}{}
	}
	if add {
		if a.assistLive[serverID] == nil {
			a.assistLive[serverID] = map[*StreamHandle]struct{}{}
		}
		a.assistLive[serverID][sh] = struct{}{}
		return
	}
	delete(a.assistLive[serverID], sh)
	if len(a.assistLive[serverID]) == 0 {
		delete(a.assistLive, serverID)
	}
}

// assistPush sends a frame to every open Assistant of a computer and says how many there were.
func (a *Admin) assistPush(ctx context.Context, serverID string, typ byte, v any) int {
	a.termMu.Lock()
	var list []*StreamHandle
	for sh := range a.assistLive[serverID] {
		list = append(list, sh)
	}
	a.termMu.Unlock()
	for _, sh := range list {
		_ = sendAssistJSON(ctx, sh, typ, v)
	}
	return len(list)
}

func sendAssistJSON(ctx context.Context, sh *StreamHandle, typ byte, v any) error {
	var buf frameBuf
	if err := assist.WriteJSON(&buf, typ, v); err != nil {
		return err
	}
	return sh.Input(ctx, buf.b)
}

// frameBuf is a writer that keeps what was written.
type frameBuf struct{ b []byte }

func (f *frameBuf) Write(p []byte) (int, error) { f.b = append(f.b, p...); return len(p), nil }

// assistantWS is the Assistant: the picture of a session of the computer goes to the browser, the keys and the mouse
// come back. The browser sends the type byte and the payload of a frame in one message and gets the same; the panel
// adds the length for the stream to the agent and checks every input on the way.
func (a *Admin) assistantWS(w http.ResponseWriter, r *http.Request, u *store.User) {
	s := a.serverOr404(w, r)
	if s == nil {
		return
	}
	session := r.URL.Query().Get("session")
	if session == "" {
		session = "console"
	}
	if !assistSessionRe.MatchString(session) {
		jsonErr(w, http.StatusBadRequest, "invalid session")
		return
	}
	if !a.Hub.Online(s.ID) {
		jsonErr(w, http.StatusServiceUnavailable, "agent is offline")
		return
	}
	set, err := a.DB.AssistantSettings(r.Context(), s.ID)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	if !a.assistSlot(s.ID, session, +1) {
		if ws, err := websocket.Accept(w, r, nil); err == nil {
			_ = ws.Close(assistLimitCode, "this session is already open in the Assistant")
		}
		return
	}
	defer a.assistSlot(s.ID, session, -1)
	ws, err := websocket.Accept(w, r, nil) // same-origin check is on by default
	if err != nil {
		return
	}
	ws.SetReadLimit(4 << 10)
	ctx, cancel := context.WithCancel(context.WithoutCancel(r.Context()))
	defer cancel()
	sh, err := a.Hub.OpenStream(ctx, s.ID, "assistant", map[string]any{"session": session})
	if err != nil {
		_ = ws.Close(websocket.StatusInternalError, "cannot open the Assistant")
		return
	}
	defer sh.Close()
	a.assistLiveSet(s.ID, sh, true)
	defer a.assistLiveSet(s.ID, sh, false)
	if err := sendAssistJSON(ctx, sh, assist.TypeSettings, set); err != nil {
		_ = ws.Close(websocket.StatusInternalError, "cannot open the Assistant")
		return
	}
	start := time.Now()
	a.audit(r, u, "assistant.open", "server", s.ID, s.Name, "ok", map[string]any{"session": session, "sid": sh.SID,
		"colors": set.Colors, "fps": set.FPS})
	var bytesOut int64

	go func() { // browser -> agent: only input, and only what parses
		defer cancel()
		for {
			typ, b, err := ws.Read(ctx)
			if err != nil {
				return
			}
			if typ != websocket.MessageBinary || len(b) < 2 || b[0] != assist.TypeInput {
				continue
			}
			if _, err := assist.ParseInput(b[1:]); err != nil {
				continue
			}
			if sh.Input(ctx, assist.AppendFrame(nil, assist.TypeInput, b[1:])) != nil {
				return
			}
		}
	}()

	var re assist.Reassembler
	forward := func(chunk []byte) error {
		frames, err := re.Push(chunk)
		for _, f := range frames {
			bytesOut += int64(len(f.Data))
			if werr := ws.Write(ctx, websocket.MessageBinary, append([]byte{f.Type}, f.Data...)); werr != nil {
				return werr
			}
		}
		return err
	}
	var loopErr error
loop:
	for {
		select {
		case b := <-sh.Data:
			if loopErr = forward(b); loopErr != nil {
				break loop
			}
		case <-sh.Done():
			for { // what is already queued is still sent
				select {
				case b := <-sh.Data:
					_ = forward(b)
					continue
				default:
				}
				break
			}
			break loop
		case <-ctx.Done():
			break loop
		}
	}
	reason := sh.Reason()
	result := "ok"
	if reason != "" || (loopErr != nil && !errors.Is(loopErr, context.Canceled)) {
		result = "failed"
		if reason == "" && loopErr != nil {
			reason = loopErr.Error()
		}
	}
	a.audit(r, u, "assistant.close", "server", s.ID, s.Name, result, map[string]any{"session": session, "sid": sh.SID,
		"seconds": int(time.Since(start).Seconds()), "bytes_out": bytesOut, "reason": reason})
	if reason != "" {
		var buf frameBuf
		_ = assist.WriteJSON(&buf, assist.TypeEnd, assist.End{Reason: reason})
		_ = ws.Write(context.WithoutCancel(ctx), websocket.MessageBinary, append([]byte{assist.TypeEnd}, buf.b[5:]...))
		_ = ws.Close(websocket.StatusNormalClosure, truncate(reason, 120))
		return
	}
	_ = ws.Close(websocket.StatusNormalClosure, "")
}

// assistantSessions lists the Windows sessions the Assistant can show.
func (a *Admin) assistantSessions(w http.ResponseWriter, r *http.Request, _ *store.User) {
	a.proxyCall(w, r, "assistant", "sessions", map[string]any{}, 15*time.Second)
}

// assistantSettingsGet returns the picture settings of the computer.
func (a *Admin) assistantSettingsGet(w http.ResponseWriter, r *http.Request, _ *store.User) {
	s := a.serverOr404(w, r)
	if s == nil {
		return
	}
	set, err := a.DB.AssistantSettings(r.Context(), s.ID)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, set)
}

// assistantSettingsSet stores the picture settings and sends them to the Assistants that are open now.
func (a *Admin) assistantSettingsSet(w http.ResponseWriter, r *http.Request, u *store.User) {
	s := a.serverOr404(w, r)
	if s == nil {
		return
	}
	var set assist.Settings
	if !decode(w, r, &set) {
		return
	}
	if err := set.Validate(); err != nil {
		jsonErr(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	if err := a.DB.SetAssistantSettings(r.Context(), s.ID, set); err != nil {
		jsonErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	n := a.assistPush(context.WithoutCancel(r.Context()), s.ID, assist.TypeSettings, set)
	a.audit(r, u, "assistant.settings", "server", s.ID, s.Name, "ok", map[string]any{"colors": set.Colors, "fps": set.FPS,
		"mbit": set.Mbit, "nolimit": set.NoLimit, "auto": set.Auto, "applied": n})
	writeJSON(w, http.StatusOK, map[string]any{"settings": set, "applied": n})
}

// desktopDisable turns the remote desktop (RDP) of a Windows computer off. Sessions that are open stay; new ones
// are refused. The agent and the Assistant do not depend on it.
func (a *Admin) desktopDisable(w http.ResponseWriter, r *http.Request, u *store.User) {
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
	a.secCall(w, r, u, s, "desktop", "disable", "desktop.disable", map[string]any{}, map[string]any{}, 60*time.Second)
}
