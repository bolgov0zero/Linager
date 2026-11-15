package server

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"

	"github.com/bolgov0zero/linager/server/store"
)

// Passkeys (WebAuthn) are a separate way to sign in: a button on the sign-in page, no login and no
// password. The authenticator is asked for user verification (PIN or biometrics), so the passkey is
// possession plus verification and needs no second factor. Passkeys are discoverable credentials, the
// browser lets the user pick one, so the login is not typed.
//
// WebAuthn only works on a secure origin with a domain name (HTTPS, or http://localhost); the relying
// party id is the host the panel is opened with, so keys stop working if that host changes.

const (
	ceremonyTTL     = 5 * time.Minute
	maxPasskeyBody  = 64 << 10
	maxPasskeyName  = 60
	errPasskeyHost  = "passkeys need the panel to be opened over HTTPS (or http://localhost) with a domain name"
	passkeyLimitMax = 20 // passkeys per user
)

// ceremony is the state between the "begin" and "finish" calls of a registration or a sign-in.
type ceremony struct {
	data    webauthn.SessionData
	userID  int64 // registration: the user; sign-in: 0
	expires time.Time
}

func (a *Admin) putCeremony(c *ceremony) string {
	b := make([]byte, 18)
	_, _ = rand.Read(b)
	id := base64.RawURLEncoding.EncodeToString(b)
	c.expires = time.Now().Add(ceremonyTTL)
	a.cerMu.Lock()
	defer a.cerMu.Unlock()
	now := time.Now()
	for k, v := range a.cers {
		if now.After(v.expires) {
			delete(a.cers, k)
		}
	}
	a.cers[id] = c
	return id
}

// takeCeremony returns the ceremony and forgets it: every challenge is used once.
func (a *Admin) takeCeremony(id string) *ceremony {
	a.cerMu.Lock()
	defer a.cerMu.Unlock()
	c := a.cers[id]
	delete(a.cers, id)
	if c == nil || time.Now().After(c.expires) {
		return nil
	}
	return c
}

// webAuthnFor builds the relying party for the host the browser used. The Origin header must be that
// same host, so a page on another site cannot start a ceremony for this panel.
func (a *Admin) webAuthnFor(r *http.Request) (*webauthn.WebAuthn, error) {
	host := r.Host
	hostname := host
	if h, _, err := net.SplitHostPort(host); err == nil {
		hostname = h
	}
	hostname = strings.Trim(hostname, "[]")
	if hostname == "" || net.ParseIP(hostname) != nil {
		return nil, errors.New(errPasskeyHost)
	}
	scheme := "http"
	if a.Trust.IsHTTPS(r) {
		scheme = "https"
	}
	if scheme == "http" && hostname != "localhost" && !strings.HasSuffix(hostname, ".localhost") {
		return nil, errors.New(errPasskeyHost)
	}
	origin := scheme + "://" + host
	if o := r.Header.Get("Origin"); o != "" {
		u, err := url.Parse(o)
		if err != nil || !strings.EqualFold(u.Scheme+"://"+u.Host, origin) {
			return nil, errors.New("origin mismatch")
		}
	}
	return webauthn.New(&webauthn.Config{RPID: hostname, RPDisplayName: "Linager", RPOrigins: []string{origin}})
}

// waUser adapts a user and the stored passkeys to the library.
type waUser struct {
	u     *store.User
	creds []webauthn.Credential
}

func (w *waUser) WebAuthnID() []byte                         { return w.u.WebAuthnHandle }
func (w *waUser) WebAuthnName() string                       { return w.u.Login }
func (w *waUser) WebAuthnDisplayName() string                { return w.u.Login }
func (w *waUser) WebAuthnCredentials() []webauthn.Credential { return w.creds }

func (a *Admin) loadWAUser(ctx context.Context, u *store.User) (*waUser, error) {
	if len(u.WebAuthnHandle) == 0 {
		h, err := a.DB.EnsureWebAuthnHandle(ctx, u.ID)
		if err != nil {
			return nil, err
		}
		u.WebAuthnHandle = h
	}
	pks, err := a.DB.Passkeys(ctx, u.ID)
	if err != nil {
		return nil, err
	}
	w := &waUser{u: u}
	for _, p := range pks {
		var c webauthn.Credential
		if json.Unmarshal(p.Credential, &c) == nil {
			w.creds = append(w.creds, c)
		}
	}
	return w, nil
}

func readBody(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxPasskeyBody)).Decode(v); err != nil {
		jsonErr(w, http.StatusBadRequest, "bad request")
		return false
	}
	return true
}

// ---- registration (signed in) ----

type passkeyView struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at"`
}

func (a *Admin) passkeyList(w http.ResponseWriter, r *http.Request, u *store.User) {
	pks, err := a.DB.Passkeys(r.Context(), u.ID)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	out := make([]passkeyView, 0, len(pks))
	for _, p := range pks {
		out = append(out, passkeyView{ID: base64.RawURLEncoding.EncodeToString(p.ID), Name: p.Name, CreatedAt: p.CreatedAt, LastUsedAt: p.LastUsedAt})
	}
	writeJSON(w, http.StatusOK, map[string]any{"passkeys": out})
}

func (a *Admin) passkeyRegisterBegin(w http.ResponseWriter, r *http.Request, u *store.User) {
	wa, err := a.webAuthnFor(r)
	if err != nil {
		jsonErr(w, http.StatusConflict, err.Error())
		return
	}
	wu, err := a.loadWAUser(r.Context(), u)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	if len(wu.creds) >= passkeyLimitMax {
		jsonErr(w, http.StatusConflict, "too many passkeys")
		return
	}
	var exclude []protocol.CredentialDescriptor
	for _, c := range wu.creds {
		exclude = append(exclude, c.Descriptor())
	}
	creation, session, err := wa.BeginRegistration(wu,
		webauthn.WithExclusions(exclude),
		webauthn.WithAuthenticatorSelection(protocol.AuthenticatorSelection{
			ResidentKey:        protocol.ResidentKeyRequirementRequired,
			RequireResidentKey: protocol.ResidentKeyRequired(),
			UserVerification:   protocol.VerificationRequired,
		}))
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	id := a.putCeremony(&ceremony{data: *session, userID: u.ID})
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "options": creation})
}

type passkeyFinishReq struct {
	ID       string          `json:"id"`
	Name     string          `json:"name"`
	Response json.RawMessage `json:"response"`
}

func (a *Admin) passkeyRegisterFinish(w http.ResponseWriter, r *http.Request, u *store.User) {
	var req passkeyFinishReq
	if !readBody(w, r, &req) {
		return
	}
	c := a.takeCeremony(req.ID)
	if c == nil || c.userID != u.ID {
		jsonErr(w, http.StatusBadRequest, "the request expired, try again")
		return
	}
	wa, err := a.webAuthnFor(r)
	if err != nil {
		jsonErr(w, http.StatusConflict, err.Error())
		return
	}
	wu, err := a.loadWAUser(r.Context(), u)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	parsed, err := protocol.ParseCredentialCreationResponseBytes(req.Response)
	if err != nil {
		jsonErr(w, http.StatusBadRequest, "bad passkey response")
		return
	}
	cred, err := wa.CreateCredential(wu, c.data, parsed)
	if err != nil {
		a.audit(r, u, "auth.passkey.add", "user", "", u.Login, "failed", nil)
		jsonErr(w, http.StatusBadRequest, "the passkey was not accepted")
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = "Passkey"
	}
	if len(name) > maxPasskeyName {
		name = name[:maxPasskeyName]
	}
	raw, _ := json.Marshal(cred)
	if err := a.DB.AddPasskey(r.Context(), u.ID, cred.ID, name, raw); err != nil {
		jsonErr(w, http.StatusConflict, "this passkey is already registered")
		return
	}
	a.audit(r, u, "auth.passkey.add", "user", "", u.Login, "ok", map[string]any{"name": name})
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *Admin) passkeyDelete(w http.ResponseWriter, r *http.Request, u *store.User) {
	id, err := base64.RawURLEncoding.DecodeString(r.PathValue("id"))
	if err != nil {
		jsonErr(w, http.StatusNotFound, "not found")
		return
	}
	var req userReq
	_ = decode(w, r, &req)
	if !VerifyPassword(req.Password, u.PasswordHash) {
		a.audit(r, u, "auth.passkey.delete", "user", "", u.Login, "failed", nil)
		jsonErr(w, http.StatusForbidden, "invalid password")
		return
	}
	ok, err := a.DB.DeletePasskey(r.Context(), u.ID, id)
	if err != nil || !ok {
		jsonErr(w, http.StatusNotFound, "not found")
		return
	}
	a.audit(r, u, "auth.passkey.delete", "user", "", u.Login, "ok", nil)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// ---- sign-in with a passkey ----

func (a *Admin) passkeyLoginBegin(w http.ResponseWriter, r *http.Request) {
	ip := a.Trust.ClientIP(r)
	if !a.loginL.Allow(ip) || !a.loginL.Allow("peer:"+remoteAddr(r).String()) {
		jsonErr(w, http.StatusTooManyRequests, "too many attempts, try later")
		return
	}
	wa, err := a.webAuthnFor(r)
	if err != nil {
		jsonErr(w, http.StatusConflict, err.Error())
		return
	}
	assertion, session, err := wa.BeginDiscoverableLogin(webauthn.WithUserVerification(protocol.VerificationRequired))
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	id := a.putCeremony(&ceremony{data: *session})
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "options": assertion})
}

func (a *Admin) passkeyLoginFinish(w http.ResponseWriter, r *http.Request) {
	ip := a.Trust.ClientIP(r)
	if !a.loginL.Allow(ip) || !a.loginL.Allow("peer:"+remoteAddr(r).String()) {
		jsonErr(w, http.StatusTooManyRequests, "too many attempts, try later")
		return
	}
	var req passkeyFinishReq
	if !readBody(w, r, &req) {
		return
	}
	c := a.takeCeremony(req.ID)
	if c == nil || c.userID != 0 {
		jsonErr(w, http.StatusBadRequest, "the request expired, try again")
		return
	}
	wa, err := a.webAuthnFor(r)
	if err != nil {
		jsonErr(w, http.StatusConflict, err.Error())
		return
	}
	parsed, err := protocol.ParseCredentialRequestResponseBytes(req.Response)
	if err != nil {
		jsonErr(w, http.StatusBadRequest, "bad passkey response")
		return
	}
	var found *store.User
	handler := func(_, userHandle []byte) (webauthn.User, error) {
		u, err := a.DB.UserByHandle(r.Context(), userHandle)
		if err != nil {
			return nil, err
		}
		found = u
		return a.loadWAUser(r.Context(), u)
	}
	user, cred, err := wa.ValidatePasskeyLogin(handler, c.data, parsed)
	if err != nil || found == nil {
		var pe *protocol.Error
		if errors.As(err, &pe) {
			a.Log.Debug("passkey sign-in refused", "err", pe.Details, "dev", pe.DevInfo)
		} else {
			a.Log.Debug("passkey sign-in refused", "err", err)
		}
		a.audit(r, nil, "auth.login", "user", "", "", "failed", map[string]any{"method": "passkey"})
		jsonErr(w, http.StatusUnauthorized, "invalid credentials")
		return
	}
	_ = user
	if found.LockedUntil != nil && found.LockedUntil.After(time.Now()) {
		a.audit(r, found, "auth.login", "user", "", found.Login, "locked", map[string]any{"method": "passkey"})
		jsonErr(w, http.StatusTooManyRequests, "account is temporarily locked")
		return
	}
	raw, _ := json.Marshal(cred)
	_ = a.DB.UpdatePasskey(r.Context(), cred.ID, raw)
	a.finishLogin(w, r, found, "passkey")
}
