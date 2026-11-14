package server

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io/fs"
	"log/slog"
	"mime"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/bolgov0zero/linager/internal/config"
	"github.com/bolgov0zero/linager/internal/pki"
	"github.com/bolgov0zero/linager/internal/protocol"
	"github.com/bolgov0zero/linager/server/store"
)

const (
	cookieName     = "linager_session"
	sessionTTL     = 12 * time.Hour
	maxEnrollTries = 5
)

type ctxKey int

const (
	keyUser ctxKey = iota
	keySession
)

// Admin serves the web admin panel and its JSON API.
type Admin struct {
	DB      *store.Store
	Keys    *Keys
	Hub     *Hub
	Live    *Live
	Metrics *Metrics
	Updates *Updates
	Log     *slog.Logger
	Cfg     *config.Server
	Trust   *ProxyTrust
	Static  fs.FS // built frontend (may be nil in tests)

	loginL *ipLimiter

	termMu sync.Mutex
	terms  map[string]int
}

// NewAdmin builds the admin handler set.
func NewAdmin(db *store.Store, keys *Keys, hub *Hub, live *Live, metrics *Metrics, updates *Updates, cfg *config.Server, static fs.FS, log *slog.Logger) (*Admin, error) {
	trust, err := NewProxyTrust(cfg.Admin.TrustedProxies)
	if err != nil {
		return nil, err
	}
	return &Admin{DB: db, Keys: keys, Hub: hub, Live: live, Metrics: metrics, Updates: updates, Log: log, Cfg: cfg, Trust: trust, Static: static, loginL: newLimiter(10, time.Minute), terms: map[string]int{}}, nil
}

// Handler returns the full admin handler including the base path and security headers.
func (a *Admin) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/login", a.login)
	mux.HandleFunc("POST /api/logout", a.auth(a.logout))
	mux.HandleFunc("GET /api/me", a.auth(a.me))
	mux.HandleFunc("POST /api/me/totp/setup", a.auth(a.totpSetup))
	mux.HandleFunc("POST /api/me/totp/enable", a.auth(a.totpEnable))
	mux.HandleFunc("POST /api/me/totp/disable", a.auth(a.totpDisable))
	mux.HandleFunc("GET /api/enrollments", a.auth(a.listEnrollments))
	mux.HandleFunc("POST /api/enrollments/{id}/approve", a.auth(a.approve))
	mux.HandleFunc("POST /api/enrollments/{id}/reject", a.auth(a.reject))
	mux.HandleFunc("GET /api/servers", a.auth(a.listServers))
	mux.HandleFunc("DELETE /api/servers/{id}", a.auth(a.deleteServer))
	mux.HandleFunc("GET /api/servers/{id}", a.auth(a.serverDetail))
	mux.HandleFunc("GET /api/servers/{id}/metrics", a.auth(a.history))
	mux.HandleFunc("GET /api/servers/{id}/metrics.csv", a.auth(a.historyCSV))
	mux.HandleFunc("GET /api/connect", a.auth(a.connectInfo))
	mux.HandleFunc("GET /api/groups", a.auth(a.groupsList))
	mux.HandleFunc("POST /api/groups", a.auth(a.groupCreate))
	mux.HandleFunc("PUT /api/groups/{id}", a.auth(a.groupRename))
	mux.HandleFunc("DELETE /api/groups/{id}", a.auth(a.groupDelete))
	mux.HandleFunc("PUT /api/servers/{id}/group", a.auth(a.serverSetGroup))
	mux.HandleFunc("GET /api/dashboard", a.auth(a.dashboard))
	mux.HandleFunc("GET /api/dashboard/available", a.auth(a.dashboardAvailable))
	mux.HandleFunc("POST /api/dashboard/items", a.auth(a.dashboardAdd))
	mux.HandleFunc("DELETE /api/dashboard/items/{id}", a.auth(a.dashboardRemove))
	mux.HandleFunc("PUT /api/dashboard/order", a.auth(a.dashboardOrder))
	mux.HandleFunc("GET /api/live", a.auth(a.liveHandler))
	mux.HandleFunc("GET /api/updates", a.auth(a.updatesGet))
	mux.HandleFunc("GET /api/updates/summary", a.auth(a.updatesSummary))
	mux.HandleFunc("POST /api/updates/check", a.auth(a.updatesCheck))
	mux.HandleFunc("PUT /api/updates/channel", a.auth(a.updatesChannel))
	mux.HandleFunc("POST /api/updates/cache", a.auth(a.updatesCache))
	mux.HandleFunc("POST /api/updates/server", a.auth(a.updatesServer))
	mux.HandleFunc("POST /api/updates/agents", a.auth(a.updatesAgents))
	mux.HandleFunc("GET /api/audit", a.auth(a.auditList))
	mux.HandleFunc("GET /api/servers/{id}/services", a.auth(a.servicesList))
	mux.HandleFunc("GET /api/servers/{id}/services-ignored", a.auth(a.servicesIgnored))
	mux.HandleFunc("PUT /api/servers/{id}/services-ignored", a.auth(a.servicesIgnore))
	mux.HandleFunc("POST /api/servers/{id}/services/{unit}/{action}", a.auth(a.serviceAction))
	mux.HandleFunc("GET /api/servers/{id}/apt", a.auth(a.aptList))
	mux.HandleFunc("POST /api/servers/{id}/reboot", a.auth(a.reboot))
	mux.HandleFunc("GET /api/servers/{id}/processes", a.auth(a.processesList))
	mux.HandleFunc("POST /api/servers/{id}/processes/{pid}/kill", a.auth(a.killProcess))
	mux.HandleFunc("GET /api/servers/{id}/stream", a.auth(a.streamWS))
	mux.HandleFunc("GET /api/servers/{id}/files", a.auth(a.filesList))
	mux.HandleFunc("GET /api/servers/{id}/files/content", a.auth(a.filesContent))
	mux.HandleFunc("PUT /api/servers/{id}/files/content", a.auth(a.simpleFileOp("write", "files.write", false)))
	mux.HandleFunc("POST /api/servers/{id}/files/mkdir", a.auth(a.simpleFileOp("mkdir", "files.mkdir", false)))
	mux.HandleFunc("POST /api/servers/{id}/files/create", a.auth(a.simpleFileOp("create", "files.create", false)))
	mux.HandleFunc("POST /api/servers/{id}/files/rename", a.auth(a.fileRename))
	mux.HandleFunc("POST /api/servers/{id}/files/copy", a.auth(a.fileTransfer("copy")))
	mux.HandleFunc("POST /api/servers/{id}/files/move", a.auth(a.fileTransfer("move")))
	mux.HandleFunc("GET /api/servers/{id}/files/zip", a.auth(a.fileZip))
	mux.HandleFunc("POST /api/servers/{id}/files/delete", a.auth(a.simpleFileOp("delete", "files.delete", true)))
	mux.HandleFunc("GET /api/servers/{id}/files/download", a.auth(a.fileDownload))
	mux.HandleFunc("POST /api/servers/{id}/files/upload", a.auth(a.fileUpload))
	mux.HandleFunc("GET /api/servers/{id}/network", a.auth(a.networkState))
	mux.HandleFunc("POST /api/servers/{id}/network/validate", a.auth(a.networkValidate))
	mux.HandleFunc("PUT /api/servers/{id}/network/draft", a.auth(a.networkSaveDraft))
	mux.HandleFunc("DELETE /api/servers/{id}/network/draft", a.auth(a.resetDraft("network")))
	mux.HandleFunc("POST /api/servers/{id}/network/apply", a.auth(a.networkApply))
	mux.HandleFunc("POST /api/servers/{id}/network/config", a.auth(a.networkApplyConfig))
	mux.HandleFunc("POST /api/servers/{id}/network/confirm", a.auth(a.confirmOrRollback("network", "confirm")))
	mux.HandleFunc("POST /api/servers/{id}/network/rollback", a.auth(a.confirmOrRollback("network", "rollback")))
	mux.HandleFunc("GET /api/servers/{id}/firewall", a.auth(a.firewallState))
	mux.HandleFunc("POST /api/servers/{id}/firewall/validate", a.auth(a.firewallValidate))
	mux.HandleFunc("PUT /api/servers/{id}/firewall/draft", a.auth(a.firewallSaveDraft))
	mux.HandleFunc("DELETE /api/servers/{id}/firewall/draft", a.auth(a.resetDraft("firewall")))
	mux.HandleFunc("POST /api/servers/{id}/firewall/apply", a.auth(a.firewallApply))
	mux.HandleFunc("POST /api/servers/{id}/firewall/confirm", a.auth(a.confirmOrRollback("firewall", "confirm")))
	mux.HandleFunc("POST /api/servers/{id}/firewall/rollback", a.auth(a.confirmOrRollback("firewall", "rollback")))
	mux.HandleFunc("GET /api/servers/{id}/docker", a.auth(a.dockerState))
	mux.HandleFunc("POST /api/servers/{id}/docker/containers/{cid}/{action}", a.auth(a.dockerContainerAction))
	mux.HandleFunc("DELETE /api/servers/{id}/docker/images/{iid}", a.auth(a.dockerRemoveImage))
	if a.Static != nil {
		mux.Handle("/", a.static())
	}
	var h http.Handler = mux
	if bp := strings.TrimRight(a.Cfg.Admin.BasePath, "/"); bp != "" {
		h = http.StripPrefix(bp, mux)
	}
	return a.secureHeaders(h)
}

func (a *Admin) secureHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), payment=(), usb=()")
		h.Set("Cross-Origin-Opener-Policy", "same-origin")
		h.Set("Cross-Origin-Resource-Policy", "same-origin")
		if a.Trust.IsHTTPS(r) {
			h.Set("Strict-Transport-Security", "max-age=31536000")
		}
		h.Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data:; style-src 'self' 'unsafe-inline'; frame-ancestors 'none'; base-uri 'self'; form-action 'self'")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		if strings.HasPrefix(r.URL.Path, "/api/") {
			h.Set("Cache-Control", "no-store")
		}
		next.ServeHTTP(w, r)
	})
}

func (a *Admin) static() http.Handler {
	// Not every system mime table knows the web app manifest; nosniff is set, so the type must be right.
	_ = mime.AddExtensionType(".webmanifest", "application/manifest+json")
	files := http.FileServerFS(a.Static)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(r.URL.Path, "/")
		if p != "" {
			if _, err := fs.Stat(a.Static, p); err != nil {
				r.URL.Path = "/" // SPA fallback
			}
		}
		files.ServeHTTP(w, r)
	})
}

// ---- helpers ----

func jsonErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBody)).Decode(v); err != nil {
		jsonErr(w, http.StatusBadRequest, "bad request")
		return false
	}
	return true
}

func (a *Admin) audit(r *http.Request, u *store.User, action, ttype, tid, tname, result string, details map[string]any) {
	au := store.Audit{Action: action, TargetType: ttype, TargetID: tid, TargetName: tname, Result: result, Details: details,
		RemoteAddr: remoteAddr(r).String(), ClientIP: a.Trust.ClientIP(r)}
	if u != nil {
		au.UserID, au.Actor = &u.ID, u.Login
	}
	if err := a.DB.WriteAudit(context.WithoutCancel(r.Context()), au); err != nil {
		a.Log.Error("audit write failed", "action", action, "err", err)
	}
}

func (a *Admin) secureCookie(r *http.Request) bool {
	switch a.Cfg.Admin.CookieSecure {
	case "always":
		return true
	case "never":
		return false
	}
	return a.Trust.IsHTTPS(r)
}

// auth requires a valid session; non-GET requests also need a matching CSRF token.
func (a *Admin) auth(next func(http.ResponseWriter, *http.Request, *store.User)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie(cookieName)
		if err != nil {
			jsonErr(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		se, err := a.DB.SessionByHash(r.Context(), HashToken(c.Value))
		if err != nil {
			jsonErr(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			if subtle.ConstantTimeCompare([]byte(r.Header.Get("X-CSRF-Token")), []byte(se.CSRF)) != 1 {
				jsonErr(w, http.StatusForbidden, "bad csrf token")
				return
			}
		}
		u, err := a.DB.UserByID(r.Context(), se.UserID)
		if err != nil {
			jsonErr(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		if a.Cfg.Admin.RequireTOTP && !u.TOTPEnabled && !totpSetupPath(r.URL.Path) {
			jsonErr(w, http.StatusForbidden, "2fa_required")
			return
		}
		next(w, r.WithContext(context.WithValue(r.Context(), keySession, se)), u)
	}
}

// ---- auth endpoints ----

type loginReq struct {
	Login    string `json:"login"`
	Password string `json:"password"`
	TOTP     string `json:"totp"`
}

func (a *Admin) login(w http.ResponseWriter, r *http.Request) {
	var req loginReq
	if !decode(w, r, &req) {
		return
	}
	ip := a.Trust.ClientIP(r)
	if !a.loginL.Allow(ip) || !a.loginL.Allow("peer:"+remoteAddr(r).String()) {
		a.audit(r, nil, "auth.login", "user", "", req.Login, "rate_limited", nil)
		jsonErr(w, http.StatusTooManyRequests, "too many attempts, try later")
		return
	}
	login := strings.ToLower(strings.TrimSpace(req.Login))
	u, err := a.DB.UserByLogin(r.Context(), login)
	if err != nil {
		VerifyPassword(req.Password, dummyHash) // equalize timing
		a.audit(r, nil, "auth.login", "user", "", login, "failed", nil)
		jsonErr(w, http.StatusUnauthorized, "invalid credentials")
		return
	}
	if u.LockedUntil != nil && u.LockedUntil.After(time.Now()) {
		a.audit(r, u, "auth.login", "user", "", login, "locked", nil)
		jsonErr(w, http.StatusTooManyRequests, "account is temporarily locked")
		return
	}
	ok := VerifyPassword(req.Password, u.PasswordHash)
	if ok && u.TOTPEnabled {
		step, err := MatchTOTP(u.TOTPSecret, req.TOTP, time.Now())
		if err != nil {
			ok = false
		} else if fresh, err := a.DB.AdvanceTOTPStep(r.Context(), u.ID, step); err != nil || !fresh {
			ok = false // code already used
		}
	}
	if !ok {
		_ = a.DB.RecordLoginFailure(r.Context(), u.ID, lockDuration(u.FailedAttempts+1))
		a.audit(r, u, "auth.login", "user", "", login, "failed", nil)
		jsonErr(w, http.StatusUnauthorized, "invalid credentials")
		return
	}
	_ = a.DB.RecordLoginSuccess(r.Context(), u.ID)
	token, hash := NewToken()
	csrf := NewCSRF()
	if err := a.DB.CreateSession(r.Context(), hash, u.ID, csrf, ip, r.UserAgent(), sessionTTL); err != nil {
		jsonErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: token, //nolint:gosec // Secure follows cookie_secure/forwarded proto: plain-HTTP LANs are supported
		Path: "/", HttpOnly: true, Secure: a.secureCookie(r),
		SameSite: http.SameSiteStrictMode, MaxAge: int(sessionTTL.Seconds())})
	a.audit(r, u, "auth.login", "user", "", login, "ok", nil)
	writeJSON(w, http.StatusOK, map[string]any{"csrf": csrf, "login": u.Login, "totp_enabled": u.TOTPEnabled,
		"totp_required": a.Cfg.Admin.RequireTOTP && !u.TOTPEnabled})
}

func (a *Admin) logout(w http.ResponseWriter, r *http.Request, u *store.User) {
	if c, err := r.Cookie(cookieName); err == nil {
		_ = a.DB.DeleteSession(r.Context(), HashToken(c.Value))
	}
	http.SetCookie(w, &http.Cookie{Name: cookieName, Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteStrictMode}) //nolint:gosec // clearing cookie
	a.audit(r, u, "auth.logout", "user", "", u.Login, "ok", nil)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *Admin) me(w http.ResponseWriter, r *http.Request, u *store.User) {
	se := r.Context().Value(keySession).(*store.Session)
	writeJSON(w, http.StatusOK, map[string]any{"login": u.Login, "totp_enabled": u.TOTPEnabled, "csrf": se.CSRF,
		"totp_required": a.Cfg.Admin.RequireTOTP && !u.TOTPEnabled})
}

// connectInfo returns what a client needs to connect to this server: the agent port, the fingerprint of the agent-port
// certificate and the names/IPs that certificate was issued for. The agent port is not behind the admin reverse proxy,
// so the page picks the host itself (usually the one the administrator opened the panel with).
func (a *Admin) connectInfo(w http.ResponseWriter, _ *http.Request, _ *store.User) {
	port := ""
	if _, p, err := net.SplitHostPort(a.Cfg.Agents.Listen); err == nil {
		port = p
	}
	hosts := a.Cfg.Agents.Hosts
	if hosts == nil {
		hosts = []string{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"port": port, "fingerprint": pki.Fingerprint(a.Keys.TLSDER), "hosts": hosts})
}

func (a *Admin) totpSetup(w http.ResponseWriter, r *http.Request, u *store.User) {
	if u.TOTPEnabled {
		jsonErr(w, http.StatusConflict, "2FA is already enabled")
		return
	}
	secret, uri, err := NewTOTPSecret(u.Login)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := a.DB.SetTOTP(r.Context(), u.ID, secret, false); err != nil {
		jsonErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"secret": secret, "uri": uri})
}

type codeReq struct {
	Code     string `json:"code"`
	Password string `json:"password"`
}

func (a *Admin) totpEnable(w http.ResponseWriter, r *http.Request, u *store.User) {
	var req codeReq
	if !decode(w, r, &req) {
		return
	}
	if u.TOTPSecret == "" || u.TOTPEnabled {
		jsonErr(w, http.StatusConflict, "run setup first")
		return
	}
	step, err := MatchTOTP(u.TOTPSecret, req.Code, time.Now())
	if err != nil {
		jsonErr(w, http.StatusUnprocessableEntity, "invalid code")
		return
	}
	if err := a.DB.SetTOTP(r.Context(), u.ID, u.TOTPSecret, true); err != nil {
		jsonErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	_, _ = a.DB.AdvanceTOTPStep(r.Context(), u.ID, step)
	a.audit(r, u, "auth.totp.enable", "user", "", u.Login, "ok", nil)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *Admin) totpDisable(w http.ResponseWriter, r *http.Request, u *store.User) {
	var req codeReq
	if !decode(w, r, &req) {
		return
	}
	if !VerifyPassword(req.Password, u.PasswordHash) {
		a.audit(r, u, "auth.totp.disable", "user", "", u.Login, "failed", nil)
		jsonErr(w, http.StatusForbidden, "invalid password")
		return
	}
	if err := a.DB.SetTOTP(r.Context(), u.ID, "", false); err != nil {
		jsonErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	a.audit(r, u, "auth.totp.disable", "user", "", u.Login, "ok", nil)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// ---- enrollments ----

type enrollmentView struct {
	ID           string    `json:"id"`
	Hostname     string    `json:"hostname"`
	Address      string    `json:"address"`
	RemoteAddr   string    `json:"remote_addr"`
	OS           string    `json:"os"`
	AgentVersion string    `json:"agent_version"`
	KeyFP        string    `json:"key_fingerprint"`
	VerifyCode   string    `json:"verify_code"`
	ExpiresAt    time.Time `json:"expires_at"`
}

func (a *Admin) listEnrollments(w http.ResponseWriter, r *http.Request, _ *store.User) {
	list, err := a.DB.PendingEnrollments(r.Context())
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	out := make([]enrollmentView, 0, len(list))
	for _, e := range list {
		out = append(out, enrollmentView{
			ID: e.ID, Hostname: e.Hostname, Address: e.Address, RemoteAddr: e.RemoteAddr, OS: e.OS, AgentVersion: e.AgentVersion,
			KeyFP:      pki.ShortFingerprint(pki.KeyFingerprint(e.AgentPubkey)),
			VerifyCode: pki.VerificationCode(a.Keys.TLSDER, e.AgentPubkey, e.Nonce),
			ExpiresAt:  e.ExpiresAt,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *Admin) approve(w http.ResponseWriter, r *http.Request, u *store.User) {
	var req struct {
		Key string `json:"key"`
	}
	if !decode(w, r, &req) {
		return
	}
	id := r.PathValue("id")
	e, err := a.DB.Enrollment(r.Context(), id)
	if err != nil || e.Status != "pending" || time.Now().After(e.ExpiresAt) {
		jsonErr(w, http.StatusNotFound, "request not found or expired")
		return
	}
	want := pki.EnrollmentKey(e.AgentPubkey)
	got := pki.NormalizeKey(req.Key)
	if got == "" || subtle.ConstantTimeCompare([]byte(got), []byte(want)) != 1 {
		n, _ := a.DB.BumpEnrollmentAttempts(r.Context(), id, maxEnrollTries)
		a.audit(r, u, "enroll.approve", "enrollment", id, e.Hostname, "bad_key", map[string]any{"attempts": n})
		if n >= maxEnrollTries {
			jsonErr(w, http.StatusGone, "too many wrong keys, the request was rejected")
			return
		}
		jsonErr(w, http.StatusUnprocessableEntity, "wrong key")
		return
	}
	srv, err := a.DB.ApproveEnrollment(r.Context(), e, func(serverID string) (string, error) {
		pem, err := a.Keys.CA.IssueAgentCert(e.AgentPubkey, serverID, a.Cfg.AgentCertLifetime())
		return string(pem), err
	})
	if err != nil {
		a.Log.Error("approve enrollment", "err", err)
		jsonErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	a.audit(r, u, "enroll.approve", "server", srv.ID, srv.Name, "ok", map[string]any{"address": e.Address})
	a.Live.Publish("dashboard", map[string]any{})
	writeJSON(w, http.StatusOK, srv)
}

func (a *Admin) reject(w http.ResponseWriter, r *http.Request, u *store.User) {
	id := r.PathValue("id")
	if err := a.DB.RejectEnrollment(r.Context(), id); err != nil {
		jsonErr(w, http.StatusNotFound, "request not found")
		return
	}
	a.audit(r, u, "enroll.reject", "enrollment", id, "", "ok", nil)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// ---- servers ----

type serverView struct {
	*store.Server
	Online bool `json:"online"`
}

func (a *Admin) listServers(w http.ResponseWriter, r *http.Request, _ *store.User) {
	list, err := a.DB.Servers(r.Context())
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	out := make([]serverView, 0, len(list))
	for _, s := range list {
		out = append(out, serverView{Server: s, Online: a.Hub.Online(s.ID)})
	}
	writeJSON(w, http.StatusOK, out)
}

// deleteServer removes a client from the server. The caller must repeat the server name
// (protection against mis-clicks). With uninstall=true and the agent online, the agent is
// first told (signed command) to remove itself from its host.
func (a *Admin) deleteServer(w http.ResponseWriter, r *http.Request, u *store.User) {
	var req struct {
		Name      string `json:"name"`
		Uninstall bool   `json:"uninstall"`
	}
	if !decode(w, r, &req) {
		return
	}
	id := r.PathValue("id")
	srv, err := a.DB.ServerByID(r.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			jsonErr(w, http.StatusNotFound, "server not found")
		} else {
			jsonErr(w, http.StatusInternalServerError, "internal error")
		}
		return
	}
	if req.Name != srv.Name {
		jsonErr(w, http.StatusUnprocessableEntity, "confirmation name does not match")
		return
	}
	online := a.Hub.Online(id)
	sent := false
	if req.Uninstall && online {
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		sent = a.Hub.Send(ctx, id, protocol.TypeAgentRemove, protocol.Uninstall{Purge: true}) == nil
		cancel()
		if sent {
			time.Sleep(500 * time.Millisecond) // let the frame leave before the connection is closed
		}
	}
	if err := a.DB.DeleteServer(r.Context(), id); err != nil {
		jsonErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	a.Hub.Kick(id)
	a.Metrics.Forget(id)
	a.Live.Publish("dashboard", map[string]any{})
	a.audit(r, u, "server.delete", "server", id, srv.Name, "ok", map[string]any{"was_online": online, "uninstall_requested": req.Uninstall, "uninstall_sent": sent})
	writeJSON(w, http.StatusOK, map[string]any{"deleted": true, "uninstall_sent": sent, "was_online": online})
}

// bg returns a context for work that must outlive a request.
func (a *Admin) bg() context.Context { return context.Background() }

// totpSetupPath lists the endpoints usable before 2FA is enabled when it is required.
func totpSetupPath(p string) bool {
	switch p {
	case "/api/me", "/api/logout", "/api/me/totp/setup", "/api/me/totp/enable":
		return true
	}
	return false
}
