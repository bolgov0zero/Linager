package agent

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/bolgov0zero/linager/internal/gwnet"
	"github.com/bolgov0zero/linager/internal/protocol"
)

// The "gateway" module: the agent looks for devices in the networks the administrator chose and reports them. The
// panel sends the configuration (gateway.configure) at every connection; the agent keeps its own copy in the data
// directory and goes on scanning when the link is lost. Scanning is off until the panel turns it on, and `gateway:
// false` in agent.yaml removes the module altogether.

const gwConfigFile = "gateway.json"

type gwModule struct {
	dir  string
	emit func(typ string, payload any) error

	mu       sync.Mutex
	cfg      protocol.GatewayConfig
	running  bool
	index    int
	total    int
	cidr     string
	phase    string
	lastPass int64
	nextPass int64
	results  map[string]protocol.GatewayPass // the latest result of each network, replayed after a reconnect
	pingOK   bool
	scanNow  bool
	cancel   context.CancelFunc // of the pass in progress

	wake chan struct{}
	stop context.CancelFunc
}

func newGatewayModule(dir string, emit func(typ string, payload any) error) *gwModule {
	m := &gwModule{dir: dir, emit: emit, results: map[string]protocol.GatewayPass{}, wake: make(chan struct{}, 1), pingOK: gwPingAvailable()}
	m.load()
	return m
}

func (m *gwModule) load() {
	b, err := os.ReadFile(filepath.Join(m.dir, gwConfigFile)) //nolint:gosec // our own data directory
	if err != nil {
		return
	}
	var c protocol.GatewayConfig
	if json.Unmarshal(b, &c) == nil {
		m.cfg = c
	}
}

func (m *gwModule) save() error {
	b, err := json.Marshal(m.cfg)
	if err != nil {
		return err
	}
	tmp := filepath.Join(m.dir, gwConfigFile+".tmp")
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(m.dir, gwConfigFile))
}

// start runs the loop of passes; it lives as long as the agent (stop is for tests).
func (m *gwModule) start() {
	ctx, cancel := context.WithCancel(context.Background())
	m.stop = cancel
	go m.loop(ctx)
}

func (m *gwModule) close() {
	if m.stop != nil {
		m.stop()
	}
}

func (m *gwModule) poke() {
	select {
	case m.wake <- struct{}{}:
	default:
	}
}

func (m *gwModule) setPhase(idx, total int, cidr, phase string) {
	m.mu.Lock()
	m.index, m.total, m.cidr, m.phase = idx, total, cidr, phase
	m.mu.Unlock()
}

func enabledNets(c protocol.GatewayConfig) []protocol.GatewayNetCfg {
	var out []protocol.GatewayNetCfg
	for _, n := range c.Nets {
		if n.On {
			out = append(out, n)
		}
	}
	return out
}

// loop waits for the time of the next pass, a change of the configuration or "scan now", and runs a pass.
func (m *gwModule) loop(ctx context.Context) {
	for {
		m.mu.Lock()
		cfg := m.cfg
		m.mu.Unlock()
		if !cfg.Enabled || len(enabledNets(cfg)) == 0 {
			m.mu.Lock()
			m.nextPass = 0
			m.mu.Unlock()
			select {
			case <-ctx.Done():
				return
			case <-m.wake:
			}
			continue
		}
		m.mu.Lock()
		m.scanNow = false
		m.mu.Unlock()
		m.runPass(ctx, cfg)
		if ctx.Err() != nil {
			return
		}
		m.mu.Lock()
		wait := time.Duration(max(cfg.Interval, protocol.GatewayMinInterval)) * time.Second
		m.nextPass = time.Now().Add(wait).Unix()
		m.mu.Unlock()
		t := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			t.Stop()
			return
		case <-t.C:
		case <-m.wake: // a new configuration or "scan now": start at once
		}
		t.Stop()
	}
}

func newPassID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// runPass scans every network that is switched on, one after another, and sends the result of each as it is ready.
func (m *gwModule) runPass(ctx context.Context, cfg protocol.GatewayConfig) {
	pctx, cancel := context.WithCancel(ctx)
	m.mu.Lock()
	m.cancel = cancel
	m.running = true
	m.mu.Unlock()
	defer func() {
		cancel()
		m.mu.Lock()
		m.cancel = nil
		m.running = false
		m.index, m.total, m.cidr, m.phase = 0, 0, "", ""
		m.mu.Unlock()
	}()

	nets := enabledNets(cfg)
	ifs := gwInterfaces()
	id := newPassID()
	for i, nc := range nets {
		if pctx.Err() != nil {
			return
		}
		n, err := gwCheckNet(nc)
		var pass protocol.GatewayPass
		if err != nil {
			pass = protocol.GatewayPass{PassID: id, CIDR: nc.CIDR, StartedAt: time.Now().Unix(), FinishedAt: time.Now().Unix(), Err: err.Error(), Devices: []protocol.GatewayDevice{}}
		} else {
			nctx, ncancel := context.WithTimeout(pctx, 15*time.Minute)
			pass = m.scanNet(nctx, id, n, nc, ifs, i+1, len(nets))
			ncancel()
		}
		if pctx.Err() != nil {
			return // the configuration changed or the agent stops: this result is not sent
		}
		pass.Last = i == len(nets)-1
		pass.FinishedAt = time.Now().Unix()
		m.mu.Lock()
		m.results[pass.CIDR] = pass
		if pass.Last {
			m.lastPass = pass.FinishedAt
		}
		m.mu.Unlock()
		if m.emit != nil {
			_ = m.emit(protocol.TypeGatewayPass, pass)
		}
	}
}

// gwCheckNet applies the rules of the networks and the confirmation of a public one.
func gwCheckNet(nc protocol.GatewayNetCfg) (gwnet.Net, error) {
	n, err := gwnet.Parse(nc.CIDR)
	if err != nil {
		return gwnet.Net{}, err
	}
	if n.Kind == gwnet.Public && !nc.Public {
		return gwnet.Net{}, errors.New("a public network needs the confirmation of the administrator")
	}
	return n, nil
}

// validateGatewayConfig is the second check of a configuration (the server has made the first one).
func validateGatewayConfig(c protocol.GatewayConfig, ifs []gwIface, routes []gwRouteEntry) error {
	if c.Interval < protocol.GatewayMinInterval || c.Interval > protocol.GatewayMaxInterval {
		return fmt.Errorf("the interval must be from %d to %d seconds", protocol.GatewayMinInterval, protocol.GatewayMaxInterval)
	}
	if len(c.Nets) > gwnet.MaxNets {
		return fmt.Errorf("at most %d networks", gwnet.MaxNets)
	}
	var seen []netip.Prefix
	for _, nc := range c.Nets {
		n, err := gwCheckNet(nc)
		if err != nil {
			return fmt.Errorf("%s: %w", nc.CIDR, err)
		}
		for _, p := range seen {
			if p.Overlaps(n.Prefix) {
				return fmt.Errorf("%s overlaps %s", n.Prefix, p)
			}
		}
		seen = append(seen, n.Prefix)
		if c.Enabled && nc.On {
			if _, own := gwOwnIface(ifs, n.Prefix); !own {
				if _, ok := bestRoute(routes, n.Prefix.Addr()); !ok {
					return fmt.Errorf("%s: the agent has no route to this network", n.Prefix)
				}
			}
		}
	}
	return nil
}

// ---- commands ----

func (m *gwModule) liveState() protocol.GatewayLive {
	m.mu.Lock()
	defer m.mu.Unlock()
	return protocol.GatewayLive{
		Enabled: m.cfg.Enabled, Running: m.running, Index: m.index, Total: m.total, CIDR: m.cidr, Phase: m.phase,
		LastPassAt: m.lastPass, NextPassAt: m.nextPass, PingOK: m.pingOK, OwnNets: gwOwnNets(gwInterfaces()),
	}
}

func (m *gwModule) stateCmd(context.Context, json.RawMessage, bool) (any, error) {
	return m.liveState(), nil
}

// configureCmd stores a configuration from the panel and starts scanning by it. Changing what the agent scans is a
// confirmed action: the server marks the command, and the agent refuses it without the mark.
func (m *gwModule) configureCmd(_ context.Context, raw json.RawMessage, dangerous bool) (any, error) {
	if !dangerous {
		return nil, errDangerous
	}
	var c protocol.GatewayConfig
	if err := json.Unmarshal(raw, &c); err != nil {
		return nil, errors.New("invalid request")
	}
	if c.Nets == nil {
		c.Nets = []protocol.GatewayNetCfg{}
	}
	if err := validateGatewayConfig(c, gwInterfaces(), gwRoutes()); err != nil {
		return nil, err
	}
	m.mu.Lock()
	same := configEqual(m.cfg, c)
	m.cfg = c
	err := m.save()
	if !same && m.cancel != nil {
		m.cancel() // a pass by the old configuration is dropped
	}
	replay := make([]protocol.GatewayPass, 0, len(m.results))
	for _, r := range m.results {
		replay = append(replay, r)
	}
	m.mu.Unlock()
	if err != nil {
		return nil, fmt.Errorf("saving the configuration: %w", err)
	}
	if !same {
		m.poke()
	}
	// the server may have missed results while the link was down: send the latest ones again (it ignores a pass it has)
	sort.Slice(replay, func(i, j int) bool { return replay[i].CIDR < replay[j].CIDR })
	go func() {
		for _, r := range replay {
			if m.emit != nil {
				_ = m.emit(protocol.TypeGatewayPass, r)
			}
		}
	}()
	return m.liveState(), nil
}

func configEqual(a, b protocol.GatewayConfig) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return string(x) == string(y)
}

// scanCmd asks for a pass now.
func (m *gwModule) scanCmd(_ context.Context, _ json.RawMessage, dangerous bool) (any, error) {
	if !dangerous {
		return nil, errDangerous
	}
	m.mu.Lock()
	if !m.cfg.Enabled || len(enabledNets(m.cfg)) == 0 {
		m.mu.Unlock()
		return nil, errors.New("scanning is off")
	}
	if m.running {
		m.mu.Unlock()
		return nil, errBusy
	}
	m.scanNow = true
	m.mu.Unlock()
	m.poke()
	return map[string]bool{"ok": true}, nil
}
