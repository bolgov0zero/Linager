package server

import (
	"bytes"
	"crypto/ed25519"
	"net/http"
	"path/filepath"
	"regexp"
	"strconv"

	"github.com/bolgov0zero/linager/internal/release"
	"github.com/bolgov0zero/linager/internal/version"
	"github.com/bolgov0zero/linager/server/store"
	"github.com/bolgov0zero/linager/update"
)

var verRe = regexp.MustCompile(`^v?\d+\.\d+\.\d+(-[A-Za-z0-9.]+)?$`)

func (a *Admin) updatesGet(w http.ResponseWriter, r *http.Request, _ *store.User) {
	a.Updates.mu.Lock()
	never := a.Updates.checkedAt.IsZero()
	a.Updates.mu.Unlock()
	if never {
		go func() { _ = a.Updates.Check(a.bg()) }()
	}
	writeJSON(w, http.StatusOK, a.Updates.Overview(r.Context()))
}

func (a *Admin) updatesCheck(w http.ResponseWriter, r *http.Request, u *store.User) {
	err := a.Updates.Check(r.Context())
	a.audit(r, u, "update.check", "system", "", "", map[bool]string{true: "ok", false: "failed"}[err == nil], nil)
	writeJSON(w, http.StatusOK, a.Updates.Overview(r.Context()))
}

func (a *Admin) updatesChannel(w http.ResponseWriter, r *http.Request, u *store.User) {
	var req struct {
		Channel string `json:"channel"`
	}
	if !decode(w, r, &req) {
		return
	}
	if req.Channel != update.Stable && req.Channel != update.Beta {
		jsonErr(w, http.StatusBadRequest, "unknown channel")
		return
	}
	_ = a.Updates.SetChannel(r.Context(), req.Channel)
	a.audit(r, u, "update.channel", "system", "", req.Channel, "ok", nil)
	writeJSON(w, http.StatusOK, a.Updates.Overview(r.Context()))
}

func (a *Admin) updatesCache(w http.ResponseWriter, r *http.Request, u *store.User) {
	var req struct {
		Version string `json:"version"`
	}
	if !decode(w, r, &req) || !verRe.MatchString(req.Version) {
		if req.Version != "" {
			jsonErr(w, http.StatusBadRequest, "invalid version")
		}
		return
	}
	_, err := a.Updates.Prefetch(r.Context(), req.Version)
	a.audit(r, u, "update.prefetch", "system", "", req.Version, map[bool]string{true: "ok", false: "failed"}[err == nil], nil)
	if err != nil {
		jsonErr(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, a.Updates.Overview(r.Context()))
}

// updatesCachePrune deletes cached releases except the newest one and the current version.
func (a *Admin) updatesCachePrune(w http.ResponseWriter, r *http.Request, u *store.User) {
	var req struct {
		Confirm bool `json:"confirm"`
	}
	if !decode(w, r, &req) {
		return
	}
	if !req.Confirm {
		needConfirm(w)
		return
	}
	removed, freed, err := a.Updates.PruneCache()
	if err != nil {
		a.audit(r, u, "update.cache.prune", "system", "", "", "failed", map[string]any{"error": err.Error()})
		jsonErr(w, http.StatusConflict, err.Error())
		return
	}
	a.audit(r, u, "update.cache.prune", "system", "", "", "ok", map[string]any{"removed": removed, "bytes": freed})
	out := a.Updates.Overview(r.Context())
	out["pruned"] = map[string]any{"count": len(removed), "bytes": freed}
	writeJSON(w, http.StatusOK, out)
}

func (a *Admin) updatesServer(w http.ResponseWriter, r *http.Request, u *store.User) {
	var req struct {
		Version string `json:"version"`
		Confirm bool   `json:"confirm"`
	}
	if !decode(w, r, &req) {
		return
	}
	if !req.Confirm {
		needConfirm(w)
		return
	}
	if req.Version == "" {
		req.Version = a.Updates.Available()
	}
	if !verRe.MatchString(req.Version) {
		jsonErr(w, http.StatusBadRequest, "invalid version")
		return
	}
	// download and verify first so that a bad release is refused before the service is touched
	if _, err := a.Updates.Prefetch(r.Context(), req.Version); err != nil {
		a.audit(r, u, "update.server", "system", "", req.Version, "failed", map[string]any{"error": err.Error()})
		jsonErr(w, http.StatusBadGateway, err.Error())
		return
	}
	if err := a.Updates.RequestServerUpdate(req.Version, u.Login); err != nil {
		a.audit(r, u, "update.server", "system", "", req.Version, "failed", map[string]any{"error": err.Error()})
		jsonErr(w, http.StatusConflict, err.Error())
		return
	}
	a.audit(r, u, "update.server", "system", "", req.Version, "ok", map[string]any{"from": version.Version})
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *Admin) updatesAgents(w http.ResponseWriter, r *http.Request, u *store.User) {
	var req RolloutRequest
	if !decode(w, r, &req) {
		return
	}
	n, err := a.Updates.StartRollout(a.bg(), req)
	res := "ok"
	if err != nil {
		res = "failed"
	}
	a.audit(r, u, "update.agents", "system", "", version.Version, res, map[string]any{"count": n, "mode": req.Mode, "stop_on_error": req.StopOnError})
	if err != nil {
		jsonErr(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, a.Updates.Overview(r.Context()))
}

func (a *Admin) updatesSummary(w http.ResponseWriter, r *http.Request, _ *store.User) {
	writeJSON(w, http.StatusOK, map[string]int{"count": a.Updates.Summary(r.Context())})
}

func (a *Admin) auditList(w http.ResponseWriter, r *http.Request, _ *store.User) {
	q := r.URL.Query()
	f := store.AuditFilter{Action: q.Get("action"), Actor: q.Get("actor"), Result: q.Get("result"), Query: q.Get("q")}
	f.Limit, _ = strconv.Atoi(q.Get("limit"))
	f.Before, _ = strconv.ParseInt(q.Get("before"), 10, 64)
	rows, err := a.DB.AuditList(r.Context(), f)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	limit := f.Limit
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	var next int64
	if len(rows) > 0 && len(rows) == limit {
		next = rows[len(rows)-1].ID
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": rows, "next_before": next})
}

// ---- release distribution to agents (agent port, mTLS) ----

// agentAuth verifies the client certificate and that the server still exists.
func (a *AgentPort) agentAuth(w http.ResponseWriter, r *http.Request) bool {
	if r.TLS == nil || len(r.TLS.PeerCertificates) == 0 {
		http.Error(w, "client certificate required", http.StatusUnauthorized)
		return false
	}
	id, err := a.Keys.CA.VerifyAgentCert(r.TLS.PeerCertificates[0])
	if err != nil {
		http.Error(w, "invalid certificate", http.StatusUnauthorized)
		return false
	}
	srv, err := a.DB.ServerByID(r.Context(), id)
	if err != nil || !bytes.Equal(srv.AgentPubkey, r.TLS.PeerCertificates[0].PublicKey.(ed25519.PublicKey)) {
		http.Error(w, "server is not registered", http.StatusUnauthorized)
		return false
	}
	return true
}

func (a *AgentPort) releaseLatest(w http.ResponseWriter, r *http.Request) {
	if !a.agentAuth(w, r) || a.Updates == nil {
		return
	}
	c, err := a.Updates.CurrentRelease(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"version": c.Version, "min_agent": c.Manifest.MinAgent})
}

// releaseFiles maps the URL name to the cached file; "binary" is resolved by architecture.
var releaseFiles = map[string]string{"manifest.json": "manifest.json", "manifest.json.minisig": "manifest.json.minisig", "binary": ""}

// releaseBinary picks the cached file for the architecture an agent asked for. Agents of releases
// before arm64 support send none and are always amd64.
func releaseBinary(m release.Manifest, arch string) (string, bool) {
	if arch == "" {
		arch = release.ArchAMD64
	}
	if arch == release.WindowsKey {
		a, ok := m.WindowsAsset()
		return a.Name, ok
	}
	a, ok := m.AssetList()[arch]
	return a.Name, ok
}

func (a *AgentPort) releaseFile(w http.ResponseWriter, r *http.Request) {
	if !a.agentAuth(w, r) || a.Updates == nil {
		return
	}
	ver := r.PathValue("ver")
	file, known := releaseFiles[r.PathValue("file")]
	if !verRe.MatchString(ver) || !known {
		http.NotFound(w, r)
		return
	}
	c, err := update.LoadCached(a.Updates.Key, a.Updates.Cache, ver) // verified before every distribution
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if file == "" {
		name, ok := releaseBinary(c.Manifest, r.URL.Query().Get("arch"))
		if !ok {
			http.Error(w, "this release has no binary for the requested architecture", http.StatusNotFound)
			return
		}
		file = name
	}
	w.Header().Set("Cache-Control", "no-store")
	http.ServeFile(w, r, filepath.Join(c.Dir, file)) //nolint:gosec // path is built from a verified cache entry and a fixed file name
}
