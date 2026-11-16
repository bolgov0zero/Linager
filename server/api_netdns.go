package server

import (
	"net"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/bolgov0zero/linager/internal/protocol"
	"github.com/bolgov0zero/linager/server/store"
)

var dnsLabelRe = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9-]{0,61}[A-Za-z0-9])?$`)

func validDNSName(h string) bool {
	if h == "" || len(h) > 253 {
		return false
	}
	for _, l := range strings.Split(h, ".") {
		if !dnsLabelRe.MatchString(l) {
			return false
		}
	}
	return true
}

// cleanDNS trims and normalizes the lists; the problem is returned as a Russian / English pair.
func cleanDNS(ns, search []string) (outNS, outSearch []string, ru, en string) {
	seen := map[string]bool{}
	for _, s := range ns {
		if s = strings.TrimSpace(s); s == "" {
			continue
		}
		ip := net.ParseIP(s)
		if ip == nil {
			return nil, nil, "Некорректный адрес DNS-сервера: " + s, "Invalid name server address: " + s
		}
		if n := ip.String(); !seen[n] {
			seen[n] = true
			outNS = append(outNS, n)
		}
	}
	if len(outNS) < 1 || len(outNS) > 3 {
		return nil, nil, "Нужно от 1 до 3 DNS-серверов", "1 to 3 name servers are required"
	}
	seen = map[string]bool{}
	for _, s := range search {
		if s = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(s), ".")); s == "" {
			continue
		}
		if !validDNSName(s) {
			return nil, nil, "Некорректный домен поиска: " + s, "Invalid search domain: " + s
		}
		if !seen[s] {
			seen[s] = true
			outSearch = append(outSearch, s)
		}
	}
	if len(outSearch) > 6 {
		return nil, nil, "Не более 6 доменов поиска", "At most 6 search domains"
	}
	if len(outSearch) > 0 && len("search "+strings.Join(outSearch, " ")) > 256 {
		return nil, nil, "Домены поиска слишком длинные (до 256 символов)", "The search domains are too long (256 characters at most)"
	}
	return outNS, outSearch, "", ""
}

func stringList(v any) []string {
	var out []string
	if l, ok := v.([]any); ok {
		for _, e := range l {
			if s, ok := e.(string); ok {
				out = append(out, s)
			}
		}
	}
	return out
}

// networkApplyDNS changes the name servers and search domains through the same pending/rollback flow as the
// interface changes: the agent restores the old settings unless the change is confirmed in time.
func (a *Admin) networkApplyDNS(w http.ResponseWriter, r *http.Request, u *store.User) {
	s := a.serverOr404(w, r)
	if s == nil {
		return
	}
	var req struct {
		Nameservers []string `json:"nameservers"`
		Search      []string `json:"search"`
		Confirm     bool     `json:"confirm"`
	}
	if !decode(w, r, &req) {
		return
	}
	ns, search, ru, en := cleanDNS(req.Nameservers, req.Search)
	if ru != "" {
		jsonErr(w, http.StatusUnprocessableEntity, msg(r, ru, en))
		return
	}
	if !req.Confirm {
		jsonErr(w, http.StatusBadRequest, msg(r, "Нужно подтверждение", "Confirmation required"))
		return
	}
	if a.pendingOf(r.Context(), s.ID, "") != nil {
		jsonErr(w, http.StatusConflict, msg(r, "Есть неподтверждённое изменение", "Another change is waiting for confirmation"))
		return
	}
	if raw := a.draftOf(r.Context(), s.ID, "network"); raw != nil {
		// confirming a change drops the network draft, so it must not be waiting
		jsonErr(w, http.StatusConflict, msg(r, "Сначала примените или сбросьте черновик настроек сети", "Apply or reset the network draft first"))
		return
	}
	st, _, err := a.netIfaces(r.Context(), s.ID)
	if err != nil {
		callErr(w, err)
		return
	}
	if ed, _ := st["dns_editable"].(bool); !ed {
		jsonErr(w, http.StatusConflict, msg(r, "Изменение DNS недоступно в этой системе", "DNS editing is not available on this system"))
		return
	}
	oldNS, oldSearch := []string{}, []string{}
	if d, ok := st["dns"].(map[string]any); ok {
		oldNS, oldSearch = stringList(d["nameservers"]), stringList(d["search"])
	}
	pv, ok := a.startDNSChange(w, r, s, map[string]any{"nameservers": ns, "search": search})
	a.audit(r, u, "network.dns", "server", s.ID, s.Name, map[bool]string{true: "ok", false: "failed"}[ok], map[string]any{
		"old": map[string]any{"nameservers": oldNS, "search": oldSearch},
		"new": map[string]any{"nameservers": ns, "search": search},
	})
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, pv)
}

// startDNSChange is startChange for the network.dns command: record the pending row, then call the agent.
func (a *Admin) startDNSChange(w http.ResponseWriter, r *http.Request, s *store.Server, params map[string]any) (*pendingView, bool) {
	id := protocol.NewID()
	deadline := time.Now().Add(protocol.ChangeWindow)
	params["id"] = id
	if err := a.DB.SetPending(r.Context(), s.ID, store.Pending{Kind: "network", ChangeID: id, Deadline: deadline}); err != nil {
		jsonErr(w, http.StatusInternalServerError, "internal error")
		return nil, false
	}
	if _, err := a.Hub.Call(r.Context(), s.ID, "network", "dns", params, true, 25*time.Second); err != nil {
		// a DNS change does not touch the link to the agent: a lost answer is a failure here
		_ = a.DB.ClearPending(r.Context(), s.ID)
		callErr(w, err)
		return nil, false
	}
	return &pendingView{ID: id, Kind: "network", Deadline: deadline.Unix()}, true
}
