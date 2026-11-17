package server

import (
	"encoding/json"
	"math"
	"net/http"
	"strconv"

	"github.com/bolgov0zero/linager/server/store"
)

// The scheme view of the dashboard lets a user drag the panel and the servers on a plane that can be zoomed and
// shifted. A position is in pixels of that plane at 100% zoom, the panel's own place is 0,0; positions are kept by
// server id, so they do not depend on the window or on how many servers there are. They belong to the user's own
// dashboard.
const (
	schemeMaxNodes = 500
	schemeMaxKey   = 36 // the length of a server id
	schemeMaxBody  = 128 << 10
	schemeMaxCoord = 50000 // far beyond any real layout; keeps a bad client from storing absurd numbers
)

type schemePoint struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

type schemeLayout struct {
	Panel *schemePoint           `json:"panel,omitempty"`
	Nodes map[string]schemePoint `json:"nodes"`
}

func (p schemePoint) valid() bool {
	in := func(v float64) bool { return !math.IsNaN(v) && v >= -schemeMaxCoord && v <= schemeMaxCoord }
	return in(p.X) && in(p.Y)
}

func (l *schemeLayout) valid() bool {
	if len(l.Nodes) > schemeMaxNodes || (l.Panel != nil && !l.Panel.valid()) {
		return false
	}
	for id, p := range l.Nodes {
		if id == "" || len(id) > schemeMaxKey || !p.valid() {
			return false
		}
	}
	return true
}

// dashboardSchemeSet saves the layout of the scheme view of the user's dashboard.
func (a *Admin) dashboardSchemeSet(w http.ResponseWriter, r *http.Request, u *store.User) {
	var req schemeLayout
	// the common body limit (16 KiB) is too small for 500 nodes with ids of 36 characters
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, schemeMaxBody)).Decode(&req); err != nil {
		jsonErr(w, http.StatusBadRequest, "bad request")
		return
	}
	if req.Nodes == nil {
		req.Nodes = map[string]schemePoint{}
	}
	if !req.valid() {
		jsonErr(w, http.StatusUnprocessableEntity, "bad layout")
		return
	}
	did, ok := a.dashID(r, u)
	if !ok {
		jsonErr(w, http.StatusInternalServerError, "no dashboard")
		return
	}
	raw, err := json.Marshal(req)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := a.DB.SetDashboardScheme(r.Context(), did, raw); err != nil {
		jsonErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	a.audit(r, u, "dashboard.scheme", "dashboard", strconv.FormatInt(did, 10), "", "ok", map[string]any{"nodes": len(req.Nodes)})
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// dashboardSchemeReset returns the scheme view of the user's dashboard to the automatic layout.
func (a *Admin) dashboardSchemeReset(w http.ResponseWriter, r *http.Request, u *store.User) {
	did, ok := a.dashID(r, u)
	if !ok {
		jsonErr(w, http.StatusInternalServerError, "no dashboard")
		return
	}
	if err := a.DB.SetDashboardScheme(r.Context(), did, nil); err != nil {
		jsonErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	a.audit(r, u, "dashboard.scheme", "dashboard", strconv.FormatInt(did, 10), "", "ok", map[string]any{"reset": true})
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
