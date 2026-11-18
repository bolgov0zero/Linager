package server

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/coder/websocket"

	"github.com/bolgov0zero/linager/internal/config"
	"github.com/bolgov0zero/linager/internal/protocol"
	"github.com/bolgov0zero/linager/server/store"
)

const (
	enrollTTL        = 15 * time.Minute
	maxPendingGlobal = 100
	maxBody          = 16 << 10
)

// AgentPort serves enrollment and the agent WebSocket on the agent listener.
type AgentPort struct {
	DB      *store.Store
	Keys    *Keys
	Hub     *Hub
	Metrics *Metrics
	Updates *Updates
	Log     *slog.Logger
	// CertLifetime is the validity of renewed agent certificates (0 = the default).
	CertLifetime time.Duration
	// ListenPort is the port of the agent listener, for the install script when a request names none.
	ListenPort string
	// Live, when set, tells the admin panel that something changed on the dashboard.
	Live    *Live
	enrollL *ipLimiter
	joinL   *ipLimiter
}

// NewAgentPort builds the handler set.
func NewAgentPort(db *store.Store, keys *Keys, hub *Hub, metrics *Metrics, updates *Updates, log *slog.Logger) *AgentPort {
	return &AgentPort{DB: db, Keys: keys, Hub: hub, Metrics: metrics, Updates: updates, Log: log, enrollL: newLimiter(20, 10*time.Minute), joinL: newLimiter(40, 10*time.Minute)}
}

// TLSConfig is the agent listener configuration: TLS 1.3, client certificate optional
// at the TLS layer (enrollment has none) and verified per route.
func (a *AgentPort) TLSConfig() *tls.Config {
	pool := x509.NewCertPool()
	pool.AddCert(a.Keys.CA.Cert)
	return &tls.Config{
		MinVersion:   tls.VersionTLS13,
		Certificates: []tls.Certificate{a.Keys.TLS},
		ClientAuth:   tls.VerifyClientCertIfGiven,
		ClientCAs:    pool,
	}
}

// Handler returns the agent-port routes.
func (a *AgentPort) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/enroll", a.enroll)
	mux.HandleFunc("GET /v1/enroll/{id}", a.enrollStatus)
	mux.HandleFunc("GET /v1/ws", a.ws)
	mux.HandleFunc("GET /v1/join/{token}", a.joinScript)
	mux.HandleFunc("GET /v1/join/{token}/binary", a.joinBinary)
	mux.HandleFunc("GET /v1/join/{token}/windows", a.joinWindowsScript)
	mux.HandleFunc("GET /v1/join/{token}/msi", a.joinMSI)
	mux.HandleFunc("GET /v1/release/latest", a.releaseLatest)
	mux.HandleFunc("GET /v1/release/{ver}/{file}", a.releaseFile)
	return mux
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func (a *AgentPort) enroll(w http.ResponseWriter, r *http.Request) {
	ip := remoteAddr(r).String()
	if !a.enrollL.Allow(ip) {
		http.Error(w, "too many requests", http.StatusTooManyRequests)
		return
	}
	var req protocol.EnrollRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBody)).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if len(req.Pubkey) != ed25519.PublicKeySize || req.Hostname == "" || len(req.Hostname) > 253 ||
		len(req.OS) > 200 || len(req.AgentVersion) > 50 || len(req.Address) > 100 ||
		req.Protocol < protocol.MinVersion || req.Protocol > protocol.Version {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if d := time.Since(time.Unix(req.TS, 0)); d > protocol.MaxSkew || d < -protocol.MaxSkew ||
		!ed25519.Verify(req.Pubkey, protocol.EnrollSigningBytes(req.Pubkey, req.Hostname, req.TS), req.Sig) {
		http.Error(w, "bad signature", http.StatusUnauthorized)
		return
	}
	if pend, err := a.DB.PendingEnrollments(r.Context()); err == nil && len(pend) >= maxPendingGlobal {
		http.Error(w, "too many pending requests", http.StatusServiceUnavailable)
		return
	}
	// An install link names who asked for this agent: its use is counted here, so the last use of a link
	// goes to one agent only.
	var tok *store.JoinToken
	if req.JoinToken != "" {
		t, ok := a.lookupJoin(r.Context(), req.JoinToken, remoteAddr(r))
		if !ok {
			http.Error(w, "the install link is not valid", http.StatusUnauthorized)
			return
		}
		if used, err := a.DB.UseJoinToken(r.Context(), t.ID, req.Hostname); err != nil || !used {
			http.Error(w, "the install link is not valid", http.StatusUnauthorized)
			return
		}
		tok = t
	}
	nonce := make([]byte, 16)
	_, _ = rand.Read(nonce)
	pending := &store.Enrollment{
		AgentPubkey: req.Pubkey, Hostname: req.Hostname, Address: req.Address, RemoteAddr: ip,
		OS: req.OS, AgentVersion: req.AgentVersion, Protocol: req.Protocol, Nonce: nonce,
	}
	if tok != nil {
		pending.TokenID = tok.ID
	}
	e, err := a.DB.CreateEnrollment(r.Context(), pending, enrollTTL)
	if err != nil {
		a.Log.Error("create enrollment", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if tok != nil {
		a.afterJoin(r, tok, e)
	}
	writeJSON(w, http.StatusOK, protocol.EnrollResponse{ID: e.ID, Nonce: nonce, ExpiresAt: e.ExpiresAt})
}

// afterJoin records a request that came with a link and, when the link says so, approves it on the spot.
func (a *AgentPort) afterJoin(r *http.Request, tok *store.JoinToken, e *store.Enrollment) {
	ctx := context.WithoutCancel(r.Context())
	ip := remoteAddr(r).String()
	actor := "link:" + tok.Label
	_ = a.DB.WriteAudit(ctx, store.Audit{Actor: actor, Action: "enroll.join", TargetType: "enrollment", TargetID: e.ID, TargetName: e.Hostname,
		RemoteAddr: ip, ClientIP: ip, Result: "ok", Details: map[string]any{"link": tok.Label, "auto": tok.AutoApprove}})
	if a.Live != nil {
		a.Live.Publish("dashboard", map[string]any{})
	}
	if !tok.AutoApprove {
		return
	}
	srv, err := a.approveJoined(ctx, e, tok)
	if err != nil {
		a.Log.Error("auto-approve an enrollment", "err", err)
		return
	}
	_ = a.DB.WriteAudit(ctx, store.Audit{Actor: actor, Action: "enroll.approve", TargetType: "server", TargetID: srv.ID, TargetName: srv.Name,
		RemoteAddr: ip, ClientIP: ip, Result: "ok", Details: map[string]any{"link": tok.Label, "auto": true}})
}

// approveJoined approves an enrollment that came with a link: it issues the certificate and puts the server into
// the group of the link. The name stays the one the agent reported.
func (a *AgentPort) approveJoined(ctx context.Context, e *store.Enrollment, tok *store.JoinToken) (*store.Server, error) {
	life := a.CertLifetime
	if life == 0 {
		life = config.DefaultAgentCertLifetime
	}
	srv, err := a.DB.ApproveEnrollment(ctx, e, func(serverID string) (string, error) {
		pem, err := a.Keys.CA.IssueAgentCert(e.AgentPubkey, serverID, life)
		return string(pem), err
	})
	if err != nil {
		return nil, err
	}
	if tok.GroupID != nil {
		_ = a.DB.SetServerGroup(ctx, srv.ID, tok.GroupID)
	}
	if a.Live != nil {
		a.Live.Publish("dashboard", map[string]any{})
	}
	return srv, nil
}

func (a *AgentPort) enrollStatus(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	e, err := a.DB.Enrollment(r.Context(), id)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	var ts int64
	if v := r.URL.Query().Get("ts"); v != "" {
		ts, _ = strconv.ParseInt(v, 10, 64)
	}
	sig, _ := base64.RawURLEncoding.DecodeString(r.URL.Query().Get("sig"))
	if d := time.Since(time.Unix(ts, 0)); d > protocol.MaxSkew || d < -protocol.MaxSkew ||
		!ed25519.Verify(e.AgentPubkey, protocol.PollSigningBytes(id, ts), sig) {
		http.Error(w, "bad signature", http.StatusUnauthorized)
		return
	}
	st := e.Status
	if st == "pending" && time.Now().After(e.ExpiresAt) {
		st = "expired"
	}
	out := protocol.EnrollStatus{Status: st}
	if st == "approved" {
		out.ServerID, out.CertPEM = e.ServerID, e.CertPEM
		out.CAPEM = string(a.Keys.CA.CertPEM)
		out.CmdPubkey = a.Keys.CmdPubkey
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *AgentPort) ws(w http.ResponseWriter, r *http.Request) {
	if r.TLS == nil || len(r.TLS.PeerCertificates) == 0 {
		http.Error(w, "client certificate required", http.StatusUnauthorized)
		return
	}
	id, err := a.Keys.CA.VerifyAgentCert(r.TLS.PeerCertificates[0])
	if err != nil {
		var inv x509.CertificateInvalidError
		if errors.As(err, &inv) && inv.Reason == x509.Expired {
			w.Header().Set("X-Linager-Reason", protocol.ReasonExpired)
		}
		http.Error(w, "invalid certificate", http.StatusUnauthorized)
		return
	}
	srv, err := a.DB.ServerByID(r.Context(), id)
	if err != nil || !bytes.Equal(srv.AgentPubkey, r.TLS.PeerCertificates[0].PublicKey.(ed25519.PublicKey)) {
		w.Header().Set("X-Linager-Reason", protocol.ReasonRevoked)
		http.Error(w, "server is not registered", http.StatusUnauthorized)
		return
	}
	ws, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	ws.SetReadLimit(1 << 20)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := &conn{ws: ws, key: a.Keys.CmdKey, cancel: cancel, state: newConnState()}
	defer c.failAll()
	defer func() { _ = ws.Close(websocket.StatusNormalClosure, "") }()

	pub := ed25519.PublicKey(srv.AgentPubkey)
	var guard protocol.SeqGuard
	read := func() (*protocol.Envelope, error) {
		_, b, err := ws.Read(ctx)
		if err != nil {
			return nil, err
		}
		var env protocol.Envelope
		if err := json.Unmarshal(b, &env); err != nil {
			return nil, err
		}
		if err := protocol.Verify(pub, &env, time.Now()); err != nil {
			return nil, err
		}
		return &env, guard.Accept(env.Seq)
	}

	hello, err := read()
	if err != nil || hello.Type != protocol.TypeHello {
		_ = ws.Close(websocket.StatusPolicyViolation, "bad hello")
		return
	}
	var h protocol.Hello
	_ = json.Unmarshal(hello.Payload, &h)
	c.version = h.AgentVersion
	if err := c.send(ctx, protocol.TypeHello, protocol.Hello{MinProtocol: protocol.MinVersion}); err != nil {
		return
	}
	a.Hub.add(id, c)
	defer a.Hub.remove(id, c)
	_ = a.DB.TouchServer(ctx, id, h.OS, h.AgentVersion, hello.V)
	_ = a.DB.SetModules(ctx, id, h.Modules)
	a.Metrics.Hostname(ctx, id, h.Hostname)
	if a.Updates != nil {
		a.Updates.AgentHello(ctx, id, h.AgentVersion)
	}
	a.Log.Info("agent connected", "server", srv.Name, "version", h.AgentVersion)
	certLife := a.CertLifetime
	if certLife <= 0 {
		certLife = config.DefaultAgentCertLifetime
	}
	var lastRenew time.Time

	for {
		env, err := read()
		if err != nil {
			a.Log.Info("agent disconnected", "server", srv.Name, "reason", err.Error())
			return
		}
		switch env.Type {
		case protocol.TypeCmdResult, protocol.TypeStreamData, protocol.TypeStreamClose:
			c.deliver(ctx, env)
		case protocol.TypeUpdateEvent:
			var ev protocol.UpdateEvent
			if json.Unmarshal(env.Payload, &ev) == nil && a.Updates != nil {
				a.Updates.AgentEvent(ctx, id, ev)
			}
		case protocol.TypeMetrics:
			var batch protocol.MetricsBatch
			if err := json.Unmarshal(env.Payload, &batch); err != nil || len(batch.Samples) > protocol.MaxBatchSamples {
				a.Log.Warn("bad metrics batch", "server", srv.Name)
				continue
			}
			a.Metrics.Ingest(ctx, id, &batch)
			if batch.System != nil {
				a.Metrics.Hostname(ctx, id, batch.System.Hostname)
			}
		case protocol.TypeCertRenew:
			// The agent is authenticated (mTLS and a signed message) and still registered, so it may have a
			// fresh certificate for the same key. Requests are rate-limited per connection; a connection that
			// stays up for years still gets every renewal it needs.
			if !lastRenew.IsZero() && time.Since(lastRenew) < min(time.Hour, certLife/6) {
				continue
			}
			lastRenew = time.Now()
			pem, err := a.Keys.CA.IssueAgentCert(pub, id, certLife)
			if err != nil {
				a.Log.Error("renew agent certificate", "server", srv.Name, "err", err)
				continue
			}
			if c.send(ctx, protocol.TypeCertRenewed, protocol.CertRenewed{CertPEM: string(pem)}) != nil {
				return
			}
			a.Log.Info("agent certificate renewed", "server", srv.Name)
		case protocol.TypePing:
			_ = a.DB.TouchServer(ctx, id, "", "", env.V)
			if c.send(ctx, protocol.TypePong, nil) != nil {
				return
			}
		}
	}
}
