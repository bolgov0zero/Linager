package server

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/bolgov0zero/linager/internal/gwnet"
	"github.com/bolgov0zero/linager/internal/protocol"
	"github.com/bolgov0zero/linager/server/store"
)

// The API of the "Gateway" tab: what the agent found (read), the configuration of the scanning, "scan now", and the
// name and the exclusion of a device. Changing anything is for administrators; an operator who was given the module
// reads.

type gwLastOut struct {
	FinishedAt int64  `json:"finished_at"`
	Probed     int    `json:"probed"`
	Answered   int    `json:"answered"`
	Found      int    `json:"found"`
	Error      string `json:"error,omitempty"`
}

type gwNetOut struct {
	CIDR   string     `json:"cidr"`
	On     bool       `json:"on"`
	Own    bool       `json:"own"`
	Ports  bool       `json:"ports"`
	Names  bool       `json:"names"`
	Public bool       `json:"public"`
	Iface  string     `json:"iface,omitempty"`
	VLAN   string     `json:"vlan,omitempty"`
	Via    string     `json:"via,omitempty"`
	Last   *gwLastOut `json:"last,omitempty"`
}

type gwDevOut struct {
	ID          int64               `json:"id"`
	IP          string              `json:"ip"`
	MAC         string              `json:"mac,omitempty"`
	Vendor      string              `json:"vendor,omitempty"`
	RandomMAC   bool                `json:"random_mac,omitempty"`
	Name        string              `json:"name,omitempty"`
	NameSrc     string              `json:"name_src,omitempty"`
	OwnName     string              `json:"own_name,omitempty"`
	Ports       []store.GatewayPort `json:"ports"`
	Via         []string            `json:"via"`
	Net         string              `json:"net"`
	FirstSeen   int64               `json:"first_seen"`
	LastSeen    int64               `json:"last_seen"`
	Online      bool                `json:"online"`
	Excluded    bool                `json:"excluded"`
	IsNew       bool                `json:"is_new"`
	Hints       []string            `json:"hints,omitempty"`
	KnownServer string              `json:"known_server,omitempty"`
	Type        string              `json:"type,omitempty"`
	Why         []gwWhy             `json:"why,omitempty"`
	Svc         []string            `json:"svc,omitempty"`
	UPnP        *store.GatewayUPnP  `json:"upnp,omitempty"`
}

type gwEventOut struct {
	ID     int64  `json:"id"`
	TS     int64  `json:"ts"`
	Kind   string `json:"kind"`
	IP     string `json:"ip"`
	Name   string `json:"name,omitempty"`
	Vendor string `json:"vendor,omitempty"`
	Net    string `json:"net"`
}

type gwStateOut struct {
	Enabled     bool                  `json:"enabled"`
	IntervalS   int                   `json:"interval_s"`
	AgentOnline bool                  `json:"agent_online"`
	LastDataAt  int64                 `json:"last_data_at,omitempty"`
	Nets        []gwNetOut            `json:"nets"`
	Devices     []gwDevOut            `json:"devices"`
	Events      []gwEventOut          `json:"events"`
	Live        *protocol.GatewayLive `json:"live,omitempty"`
}

// knownServers maps the addresses and the MAC addresses of the servers of the panel to their names, so a device that
// is an agent is shown as one and not as a stranger.
func (a *Admin) knownServers(ctx context.Context) (byIP, byMAC map[string]string) {
	byIP, byMAC = map[string]string{}, map[string]string{}
	servers, err := a.DB.Servers(ctx)
	if err != nil {
		return
	}
	for _, s := range servers {
		host := s.Address
		if h, _, err := net.SplitHostPort(host); err == nil {
			host = h
		}
		if ip, err := netip.ParseAddr(strings.TrimSpace(host)); err == nil {
			byIP[ip.Unmap().String()] = s.Name
		}
		if a.Metrics == nil {
			continue
		}
		if v := a.Metrics.Get(ctx, s.ID); v.Sample != nil {
			for _, n := range v.Sample.Net {
				if ip, _, err := net.ParseCIDR(n.IP); err == nil {
					byIP[ip.String()] = s.Name
				} else if ip, err := netip.ParseAddr(n.IP); err == nil {
					byIP[ip.String()] = s.Name
				}
				if n.MAC != "" {
					byMAC[strings.ToLower(n.MAC)] = s.Name
				}
			}
		}
	}
	return
}

func gwHints(d store.GatewayDevice, known string) []string {
	var out []string
	var banners string
	for _, p := range d.Ports {
		banners += " " + strings.ToLower(p.Banner)
	}
	if known == "" && (strings.Contains(banners, "debian") || strings.Contains(banners, "ubuntu")) {
		out = append(out, "debian")
	}
	return out
}

func (a *Admin) gatewayState(ctx context.Context, s *store.Server) (*gwStateOut, error) {
	cfg, err := a.DB.GatewayConfig(ctx, s.ID)
	if err != nil {
		return nil, err
	}
	devs, err := a.DB.GatewayDevices(ctx, s.ID)
	if err != nil {
		return nil, err
	}
	events, err := a.DB.GatewayEvents(ctx, s.ID, time.Now().Add(-7*24*time.Hour), 200)
	if err != nil {
		return nil, err
	}
	out := &gwStateOut{Enabled: cfg.Enabled, IntervalS: cfg.IntervalS, AgentOnline: a.Hub.Online(s.ID), Nets: []gwNetOut{}, Devices: []gwDevOut{}, Events: []gwEventOut{}}
	if cfg.DataAt != nil {
		out.LastDataAt = cfg.DataAt.Unix()
	}
	for _, n := range cfg.Nets {
		o := gwNetOut{CIDR: n.CIDR, On: n.On, Own: n.Own, Ports: n.Ports, Names: n.Names, Public: n.Public, Iface: n.Iface, VLAN: gwnet.VLAN(n.Iface), Via: n.Via}
		if n.Last != nil {
			o.Last = &gwLastOut{FinishedAt: n.Last.FinishedAt, Probed: n.Last.Probed, Answered: n.Last.Answered, Found: n.Last.Found, Error: n.Last.Error}
		}
		out.Nets = append(out.Nets, o)
	}
	byIP, byMAC := a.knownServers(ctx)
	for _, d := range devs {
		known := byMAC[d.MAC]
		if known == "" {
			known = byIP[d.IP]
		}
		ports := d.Ports
		if ports == nil {
			ports = []store.GatewayPort{}
		}
		via := d.Via
		if via == nil {
			via = []string{}
		}
		kind, why := classifyDevice(gwClassIn{Vendor: d.Vendor, RandomMAC: d.RandomMAC, Ports: devPorts(ports), Banners: devBanners(ports), Svc: d.Info.Svc,
			UPnPType: upnpType(d.Info.UPnP), NameSrc: d.NameSrc, Router: d.Info.Router})
		out.Devices = append(out.Devices, gwDevOut{
			ID: d.ID, IP: d.IP, MAC: d.MAC, Vendor: d.Vendor, RandomMAC: d.RandomMAC, Name: d.Name, NameSrc: d.NameSrc, OwnName: d.OwnName,
			Ports: ports, Via: via, Net: d.Net, FirstSeen: d.FirstSeen.Unix(), LastSeen: d.LastSeen.Unix(), Online: d.Online, Excluded: d.Excluded,
			IsNew: !d.Initial && time.Since(d.FirstSeen) < 24*time.Hour, Hints: gwHints(d, known), KnownServer: known,
			Type: kind, Why: why, Svc: d.Info.Svc, UPnP: d.Info.UPnP,
		})
	}
	for _, e := range events {
		out.Events = append(out.Events, gwEventOut{ID: e.ID, TS: e.TS.Unix(), Kind: e.Kind, IP: e.IP, Name: e.Name, Vendor: e.Vendor, Net: e.Net})
	}
	if out.AgentOnline {
		if raw, err := a.Hub.Call(ctx, s.ID, protocol.GatewayModule, "state", nil, false, 4*time.Second); err == nil {
			var live protocol.GatewayLive
			if jsonUnmarshal(raw, &live) == nil {
				if live.OwnNets == nil {
					live.OwnNets = []protocol.GatewayOwnNet{}
				}
				for i := range live.OwnNets {
					if live.OwnNets[i].VLAN == "" {
						live.OwnNets[i].VLAN = gwnet.VLAN(live.OwnNets[i].Iface)
					}
				}
				out.Live = &live
			}
		}
	}
	return out, nil
}

func (a *Admin) gatewayGet(w http.ResponseWriter, r *http.Request, _ *store.User) {
	s := a.serverOr404(w, r)
	if s == nil {
		return
	}
	out, err := a.gatewayState(r.Context(), s)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, out)
}

type gwSaveReq struct {
	Enabled   bool `json:"enabled"`
	IntervalS int  `json:"interval_s"`
	Nets      []struct {
		CIDR          string `json:"cidr"`
		On            bool   `json:"on"`
		Ports         bool   `json:"ports"`
		Names         bool   `json:"names"`
		ConfirmPublic bool   `json:"confirm_public"`
	} `json:"nets"`
	Confirm bool `json:"confirm"`
}

func gwRefuse(w http.ResponseWriter, code, msg string) {
	writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": msg, "code": code})
}

// hasGateway tells whether the agent reported the module (an old agent, or one with `gateway: false`, did not).
func (a *Admin) hasGateway(ctx context.Context, serverID string) bool {
	mods, err := a.DB.Modules(ctx, serverID)
	return err == nil && slices.Contains(mods, protocol.GatewayModule)
}

// gatewaySave replaces the configuration: turns scanning on or off, sets the interval and the networks. The panel
// checks every network by the rules of the module, sends the result to the agent (which checks it again) and keeps it
// only when the agent took it; an agent that is offline gets it when it connects.
func (a *Admin) gatewaySave(w http.ResponseWriter, r *http.Request, u *store.User) {
	s := a.serverOr404(w, r)
	if s == nil {
		return
	}
	var req gwSaveReq
	if !decode(w, r, &req) {
		return
	}
	if !req.Confirm {
		needConfirm(w)
		return
	}
	ctx := r.Context()
	if !a.hasGateway(ctx, s.ID) {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "the agent has no gateway module (update the agent, or `gateway: false` is set in agent.yaml)", "code": "no_module"})
		return
	}
	if req.IntervalS < protocol.GatewayMinInterval || req.IntervalS > protocol.GatewayMaxInterval {
		gwRefuse(w, "bad_interval", "the interval must be from "+strconv.Itoa(protocol.GatewayMinInterval)+" to "+strconv.Itoa(protocol.GatewayMaxInterval)+" seconds")
		return
	}
	if len(req.Nets) > gwnet.MaxNets {
		gwRefuse(w, "too_many_nets", "at most "+strconv.Itoa(gwnet.MaxNets)+" networks")
		return
	}
	old, err := a.DB.GatewayConfig(ctx, s.ID)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	oldByCIDR := map[string]store.GatewayNet{}
	for _, n := range old.Nets {
		oldByCIDR[n.CIDR] = n
	}
	next := &store.GatewayCfg{ServerID: s.ID, Enabled: req.Enabled, IntervalS: req.IntervalS, Nets: []store.GatewayNet{}, UpdatedBy: u.Login}
	var prefixes []netip.Prefix
	var added, confirmed []string
	for _, rn := range req.Nets {
		n, err := gwnet.Parse(rn.CIDR)
		if err != nil {
			code := "bad_net"
			var se *gwnet.SizeError
			if errors.As(err, &se) {
				code = "too_big"
			}
			gwRefuse(w, code, rn.CIDR+": "+err.Error())
			return
		}
		cidr := n.Prefix.String()
		for _, p := range prefixes {
			if p.Overlaps(n.Prefix) {
				gwRefuse(w, "overlap", cidr+" overlaps "+p.String())
				return
			}
		}
		prefixes = append(prefixes, n.Prefix)
		rec, existed := oldByCIDR[cidr]
		rec.CIDR, rec.On, rec.Ports, rec.Names = cidr, rn.On, rn.Ports, rn.Names
		if n.Kind == gwnet.Public {
			if !rec.Public {
				if !rn.ConfirmPublic {
					gwRefuse(w, "public_unconfirmed", cidr+" is a public network: the administrator must confirm that it may be scanned")
					return
				}
				rec.Public, rec.ConfirmedBy, rec.ConfirmedAt = true, u.Login, time.Now().Unix()
				confirmed = append(confirmed, cidr)
			}
		} else {
			rec.Public, rec.ConfirmedBy, rec.ConfirmedAt = false, "", 0
		}
		if !existed {
			added = append(added, cidr)
		}
		next.Nets = append(next.Nets, rec)
	}
	var removed []string
	for c := range oldByCIDR {
		if !slices.ContainsFunc(next.Nets, func(n store.GatewayNet) bool { return n.CIDR == c }) {
			removed = append(removed, c)
		}
	}
	sort.Strings(removed)

	details := map[string]any{"enabled": req.Enabled, "was_enabled": old.Enabled, "interval_s": req.IntervalS, "networks": len(next.Nets)}
	if len(added) > 0 {
		details["added"] = added
	}
	if len(removed) > 0 {
		details["removed"] = removed
	}
	if len(confirmed) > 0 {
		details["confirmed_public"] = confirmed
	}
	if a.Hub.Online(s.ID) { // the agent has the last word: it checks the routes, which only it can see
		if _, err := a.Gateway.Push(ctx, s.ID, next); err != nil {
			a.audit(r, u, "gateway.config", "server", s.ID, s.Name, "failed", details)
			var ae *AgentError
			if errors.As(err, &ae) && ae.Msg != protocol.ErrModuleDisabled {
				gwRefuse(w, "agent_refused", ae.Msg)
				return
			}
			callErr(w, err)
			return
		}
	}
	if err := a.DB.SetGatewayConfig(ctx, next); err != nil {
		jsonErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	a.audit(r, u, "gateway.config", "server", s.ID, s.Name, "ok", details)
	out, err := a.gatewayState(ctx, s)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// gatewayScan asks the agent for a pass now.
func (a *Admin) gatewayScan(w http.ResponseWriter, r *http.Request, u *store.User) {
	s := a.serverOr404(w, r)
	if s == nil {
		return
	}
	a.secCall(w, r, u, s, protocol.GatewayModule, "scan", "gateway.scan", nil, nil, 20*time.Second)
}

// gatewayDevice changes the name the administrator gave a device and whether it stays off the map.
func (a *Admin) gatewayDevice(w http.ResponseWriter, r *http.Request, u *store.User) {
	s := a.serverOr404(w, r)
	if s == nil {
		return
	}
	did, err := strconv.ParseInt(r.PathValue("did"), 10, 64)
	if err != nil {
		jsonErr(w, http.StatusNotFound, "device not found")
		return
	}
	var req struct {
		OwnName  *string `json:"own_name"`
		Excluded *bool   `json:"excluded"`
	}
	if !decode(w, r, &req) {
		return
	}
	if req.OwnName == nil && req.Excluded == nil {
		jsonErr(w, http.StatusBadRequest, "nothing to change")
		return
	}
	details := map[string]any{"device": did}
	if req.OwnName != nil {
		name := strings.TrimSpace(*req.OwnName)
		if utf8.RuneCountInString(name) > 100 {
			gwRefuse(w, "name_too_long", "the name is limited to 100 characters")
			return
		}
		req.OwnName = &name
		details["own_name"] = name
	}
	if req.Excluded != nil {
		details["excluded"] = *req.Excluded
	}
	if err := a.DB.PatchGatewayDevice(r.Context(), s.ID, did, req.OwnName, req.Excluded); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			jsonErr(w, http.StatusNotFound, "device not found")
			return
		}
		jsonErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	a.audit(r, u, "gateway.device", "server", s.ID, s.Name, "ok", details)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func devPorts(ps []store.GatewayPort) []int {
	out := make([]int, 0, len(ps))
	for _, p := range ps {
		out = append(out, p.Port)
	}
	return out
}

func devBanners(ps []store.GatewayPort) string {
	var b strings.Builder
	for _, p := range ps {
		b.WriteString(strings.ToLower(p.Banner))
		b.WriteByte(' ')
	}
	return b.String()
}

func upnpType(u *store.GatewayUPnP) string {
	if u == nil {
		return ""
	}
	return u.Type
}
