package server

import (
	"context"
	"encoding/json"
	"log/slog"
	"net"
	"net/netip"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/bolgov0zero/linager/internal/protocol"
	"github.com/bolgov0zero/linager/server/oui"
	"github.com/bolgov0zero/linager/server/store"
)

// Gateway is the panel side of the gateway module: it takes the results of the passes an agent sends, keeps the
// devices and the events, and sends the configuration to the agent when it connects.
type Gateway struct {
	DB  *store.Store
	Hub *Hub
	Log *slog.Logger

	mu        sync.Mutex
	lastPrune time.Time
}

// NewGateway builds the gateway of the panel.
func NewGateway(db *store.Store, hub *Hub, log *slog.Logger) *Gateway {
	return &Gateway{DB: db, Hub: hub, Log: log}
}

const (
	gwEventsKeep   = 90 * 24 * time.Hour
	gwMaxPorts     = 64
	gwMaxSvc       = 32
	gwMaxBanner    = 200
	gwMaxNameBytes = 253
)

var svcTypeRe = regexp.MustCompile(`^_[a-z0-9-]{1,40}\._(tcp|udp)$`)

// gwNameSrcs are the sources of a name the panel accepts.
var gwNameSrcs = []string{"ptr", "mdns", "netbios", "ssdp"}

// namePriority is the order the sources of a name are trusted in.
var namePriority = []string{"mdns", "ptr", "netbios", "ssdp"}

func bestName(names []protocol.GatewayName) (name, src string) {
	best := len(namePriority) + 1
	for _, n := range names {
		nm := strings.TrimSpace(n.Name)
		if nm == "" || len(nm) > gwMaxNameBytes || !slices.Contains(gwNameSrcs, n.Src) {
			continue
		}
		rank := slices.Index(namePriority, n.Src)
		if rank < 0 {
			rank = len(namePriority)
		}
		if rank < best {
			best, name, src = rank, nm, n.Src
		}
	}
	return name, src
}

// Ingest stores the result of one network of a pass. Everything an agent sends is checked here: an agent is trusted
// to scan, not to write what it likes into the database.
func (g *Gateway) Ingest(ctx context.Context, serverID string, p protocol.GatewayPass) {
	prefix, err := netip.ParsePrefix(p.CIDR)
	if err != nil {
		return
	}
	in := store.GatewayPassIn{PassID: p.PassID, CIDR: prefix.Masked().String(), Own: p.Own, Iface: p.Iface, Via: p.Via,
		FinishedAt: p.FinishedAt, Probed: p.Probed, Answered: p.Answered, Err: clip(p.Err, 200)}
	if len(p.PassID) == 0 || len(p.PassID) > 64 {
		return
	}
	devices := p.Devices
	if len(devices) > protocol.GatewayMaxDevices {
		devices = devices[:protocol.GatewayMaxDevices]
	}
	seen := map[netip.Addr]bool{}
	for _, d := range devices {
		ip, err := netip.ParseAddr(d.IP)
		if err != nil || !prefix.Contains(ip.Unmap()) || seen[ip.Unmap()] {
			continue
		}
		seen[ip.Unmap()] = true
		o := store.GatewayObs{IP: ip.Unmap(), Via: sanitizeVia(d.Via)}
		if p.Own { // behind a router the only MAC that can be seen is the router's
			if hw, err := net.ParseMAC(d.MAC); err == nil && len(hw) == 6 {
				o.MAC = strings.ToLower(hw.String())
				o.Vendor, o.RandomMAC = oui.Lookup(o.MAC), oui.Random(o.MAC)
			}
		}
		o.Name, o.NameSrc = bestName(d.Names)
		o.Info.Router = d.Router
		for _, sv := range d.Svc {
			if sv = strings.ToLower(sv); svcTypeRe.MatchString(sv) && len(o.Info.Svc) < gwMaxSvc && !slices.Contains(o.Info.Svc, sv) {
				o.Info.Svc = append(o.Info.Svc, sv)
			}
		}
		if u := d.UPnP; u != nil && (u.Type != "" || u.Model != "" || u.Maker != "") {
			o.Info.UPnP = &store.GatewayUPnP{Type: clip(u.Type, 80), Model: clip(u.Model, 100), Maker: clip(u.Maker, 100)}
		}
		for i, pt := range d.Ports {
			if i >= gwMaxPorts {
				break
			}
			if pt.Port < 1 || pt.Port > 65535 {
				continue
			}
			o.Ports = append(o.Ports, store.GatewayPort{Port: pt.Port, Banner: clip(pt.Banner, gwMaxBanner)})
		}
		in.Devices = append(in.Devices, o)
	}
	known, err := g.DB.GatewayIngest(ctx, serverID, in)
	if err != nil {
		g.Log.Warn("gateway: storing a pass failed", "server", serverID, "net", in.CIDR, "err", err)
		return
	}
	if !known {
		g.Log.Debug("gateway: a result of a network that is not configured", "server", serverID, "net", in.CIDR)
		return
	}
	g.prune(ctx)
}

func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return strings.ToValidUTF8(s[:n], "")
}

func sanitizeVia(v []string) []string {
	out := []string{}
	for _, x := range v {
		if (x == "arp" || x == "ping" || x == "ports") && !slices.Contains(out, x) {
			out = append(out, x)
		}
	}
	return out
}

// prune drops the events older than 90 days, at most once an hour.
func (g *Gateway) prune(ctx context.Context) {
	g.mu.Lock()
	due := time.Since(g.lastPrune) > time.Hour
	if due {
		g.lastPrune = time.Now()
	}
	g.mu.Unlock()
	if due {
		_ = g.DB.PruneGatewayEvents(ctx, time.Now().Add(-gwEventsKeep))
	}
}

// agentConfig turns the stored configuration into what the agent is sent.
func agentConfig(c *store.GatewayCfg) protocol.GatewayConfig {
	out := protocol.GatewayConfig{Enabled: c.Enabled, Interval: c.IntervalS, Nets: []protocol.GatewayNetCfg{}}
	for _, n := range c.Nets {
		out.Nets = append(out.Nets, protocol.GatewayNetCfg{CIDR: n.CIDR, On: n.On, Ports: n.Ports, Names: n.Names, Public: n.Public})
	}
	return out
}

// Push sends the stored configuration to an agent that is online; the command is marked as confirmed because the
// administrator confirmed it when saving it.
func (g *Gateway) Push(ctx context.Context, serverID string, c *store.GatewayCfg) (json.RawMessage, error) {
	return g.Hub.Call(ctx, serverID, protocol.GatewayModule, "configure", agentConfig(c), true, 30*time.Second)
}

// OnHello sends the configuration to an agent that has just connected (it keeps its own copy, but the panel is the
// one that knows the current one) and asks nothing when no gateway was ever configured for it.
func (g *Gateway) OnHello(ctx context.Context, serverID string, modules []string) {
	if !slices.Contains(modules, protocol.GatewayModule) {
		return
	}
	cfg, err := g.DB.GatewayConfig(ctx, serverID)
	if err != nil || (!cfg.Enabled && len(cfg.Nets) == 0) {
		return
	}
	cctx, cancel := context.WithTimeout(ctx, 40*time.Second)
	defer cancel()
	if _, err := g.Push(cctx, serverID, cfg); err != nil {
		g.Log.Warn("gateway: sending the configuration to the agent failed", "server", serverID, "err", err)
	}
}

func jsonUnmarshal(raw json.RawMessage, v any) error { return json.Unmarshal(raw, v) }
