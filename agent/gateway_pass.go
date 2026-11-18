package agent

import (
	"context"
	"net/netip"
	"sort"
	"sync"
	"time"

	"github.com/bolgov0zero/linager/internal/gwnet"
	"github.com/bolgov0zero/linager/internal/protocol"
)

// gwDev collects what is learnt about one address during the scan of a network.
type gwDev struct {
	mac   string
	via   map[string]bool
	ports map[int]string // open port -> banner
	name  string         // reverse DNS
	names []protocol.GatewayName
	svc   []string
	upnp  *protocol.GatewayUPnP
}

// gwBannerPorts are the ports a banner is read from.
var gwBannerPorts = map[int]bool{22: true, 80: true, 443: true, 8006: true, 5000: true, 5001: true, 8080: true}

// gwEach runs fn for 0..n-1 on a few workers.
func gwEach(ctx context.Context, workers, n int, fn func(i int)) {
	if n == 0 {
		return
	}
	jobs := make(chan int)
	var wg sync.WaitGroup
	for w := 0; w < min(workers, n); w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				fn(i)
			}
		}()
	}
loop:
	for i := 0; i < n; i++ {
		select {
		case jobs <- i:
		case <-ctx.Done():
			break loop
		}
	}
	close(jobs)
	wg.Wait()
}

// scanNet looks at every address of one network and returns what answered. The order is the cheap and certain
// first: ARP (a network the host is attached to: it also shows hosts that drop ping), ping, then the TCP ports, then
// the names and the banners of the hosts that are known to be up. A public network gets ping and the short list of
// ports only, at a lower rate.
func (m *gwModule) scanNet(ctx context.Context, passID string, n gwnet.Net, nc protocol.GatewayNetCfg, ifs []gwIface, idx, total int) protocol.GatewayPass {
	p := n.Prefix
	cidr := p.String()
	pass := protocol.GatewayPass{PassID: passID, CIDR: cidr, StartedAt: time.Now().Unix(), Devices: []protocol.GatewayDevice{}}
	own, isOwn := gwOwnIface(ifs, p)
	if isOwn {
		pass.Own, pass.Iface = true, own.Name
	} else if r, ok := bestRoute(gwRoutes(), p.Addr()); ok {
		pass.Iface = r.Iface
		if !r.Direct {
			pass.Via = r.Gate.String() + " (" + r.Iface + ")"
		}
	}

	local := gwLocalAddrs()
	var hosts []netip.Addr
	for _, h := range gwnet.Hosts(p) {
		if !local[h] {
			hosts = append(hosts, h)
		}
	}
	pass.Probed = len(hosts)
	public := n.Kind == gwnet.Public
	rate := gwRatePrivate
	if public {
		rate = gwRatePublic
	}
	lim := newGwLimiter(rate)

	var mu sync.Mutex
	devs := map[netip.Addr]*gwDev{}
	get := func(a netip.Addr) *gwDev { // the caller holds mu
		d := devs[a]
		if d == nil {
			d = &gwDev{via: map[string]bool{}, ports: map[int]string{}}
			devs[a] = d
		}
		return d
	}
	inNet := map[netip.Addr]bool{}
	for _, h := range hosts {
		inNet[h] = true
	}

	// ARP: ask the kernel for every address and read what it learnt
	if isOwn {
		m.setPhase(idx, total, cidr, "arp")
		for _, h := range hosts {
			if lim.wait(ctx) != nil {
				break
			}
			arpKick(h)
		}
		t := time.NewTimer(gwArpSettle)
		select {
		case <-ctx.Done():
		case <-t.C:
		}
		t.Stop()
		for _, e := range gwARPTable() {
			if e.Iface == own.Name && inNet[e.IP] {
				mu.Lock()
				d := get(e.IP)
				d.mac, d.via["arp"] = e.MAC, true
				mu.Unlock()
			}
		}
	}

	// ping
	m.mu.Lock()
	pingOK := m.pingOK
	m.mu.Unlock()
	if pingOK && ctx.Err() == nil {
		m.setPhase(idx, total, cidr, "ping")
		if alive, err := gwPing(ctx, hosts, lim); err == nil || len(alive) > 0 {
			mu.Lock()
			for a := range alive {
				get(a).via["ping"] = true
			}
			mu.Unlock()
		}
	}

	// TCP: a host that is up gets the whole list, a host that was not seen gets the short one (many devices drop
	// ping and ARP does not cross a router). An accepted or refused connection means the host is there.
	if nc.Ports && ctx.Err() == nil {
		m.setPhase(idx, total, cidr, "ports")
		type job struct {
			ip   netip.Addr
			port int
		}
		var jobs []job
		mu.Lock()
		for _, h := range hosts {
			list := gwLivePortList
			if _, up := devs[h]; up && !public {
				list = gwPortList
			}
			for _, port := range list {
				jobs = append(jobs, job{h, port})
			}
		}
		mu.Unlock()
		gwEach(ctx, gwWorkers, len(jobs), func(i int) {
			j := jobs[i]
			if lim.wait(ctx) != nil {
				return
			}
			open, alive := gwTCP(ctx, j.ip, j.port)
			if !open && !alive {
				return
			}
			mu.Lock()
			d := get(j.ip)
			d.via["ports"] = true
			if open {
				d.ports[j.port] = ""
			}
			mu.Unlock()
		})
	}

	// names and banners of the hosts that answered
	if ctx.Err() == nil && !public {
		mu.Lock()
		ips := make([]netip.Addr, 0, len(devs))
		for a := range devs {
			ips = append(ips, a)
		}
		mu.Unlock()
		sort.Slice(ips, func(i, j int) bool { return ips[i].Less(ips[j]) })
		if nc.Names {
			m.setPhase(idx, total, cidr, "names")
			gwEach(ctx, 16, len(ips), func(i int) {
				if name := gwPTR(ctx, ips[i]); name != "" {
					mu.Lock()
					get(ips[i]).name = name
					mu.Unlock()
				}
			})
			// mDNS and NetBIOS go to the host itself, so they also work in a network behind a router
			gwEach(ctx, 32, len(ips), func(i int) {
				for k := 0; k < 3; k++ { // two mDNS datagrams and one NetBIOS: three probes of the rate
					if lim.wait(ctx) != nil {
						return
					}
				}
				name, svc := gwMDNS(ctx, ips[i])
				nb := gwNetBIOS(ctx, ips[i])
				if name == "" && len(svc) == 0 && nb == "" {
					return
				}
				mu.Lock()
				d := get(ips[i])
				if name != "" {
					d.names = append(d.names, protocol.GatewayName{Src: "mdns", Name: name})
				}
				if nb != "" {
					d.names = append(d.names, protocol.GatewayName{Src: "netbios", Name: nb})
				}
				d.svc = svc
				mu.Unlock()
			})
			// SSDP is multicast: only in a network the agent is attached to
			if isOwn && ctx.Err() == nil {
				want := make(map[netip.Addr]bool, len(ips))
				for _, a := range ips {
					want[a] = true
				}
				for a, info := range gwSSDP(ctx, own.Addr, want) {
					mu.Lock()
					d := get(a)
					if info.Name != "" {
						d.names = append(d.names, protocol.GatewayName{Src: "ssdp", Name: info.Name})
					}
					up := info.UPnP
					d.upnp = &up
					mu.Unlock()
				}
			}
		}
		if nc.Ports {
			type bj struct {
				ip   netip.Addr
				port int
			}
			var bjs []bj
			mu.Lock()
			for _, a := range ips {
				for port := range devs[a].ports {
					if gwBannerPorts[port] {
						bjs = append(bjs, bj{a, port})
					}
				}
			}
			mu.Unlock()
			gwEach(ctx, 16, len(bjs), func(i int) {
				if lim.wait(ctx) != nil {
					return
				}
				if b := gwBanner(ctx, bjs[i].ip, bjs[i].port); b != "" {
					mu.Lock()
					get(bjs[i].ip).ports[bjs[i].port] = b
					mu.Unlock()
				}
			})
		}
	}

	if ctx.Err() != nil {
		pass.Err = "cancelled"
		return pass
	}
	mu.Lock()
	defer mu.Unlock()
	addrs := make([]netip.Addr, 0, len(devs))
	for a := range devs {
		addrs = append(addrs, a)
	}
	sort.Slice(addrs, func(i, j int) bool { return addrs[i].Less(addrs[j]) })
	gate := gwDefaultGate()
	for _, a := range addrs {
		d := devs[a]
		dv := protocol.GatewayDevice{IP: a.String(), MAC: d.mac, Via: gwMethods(d.via), Svc: d.svc, UPnP: d.upnp, Router: a == gate}
		dv.Names = append(dv.Names, d.names...)
		if d.name != "" {
			dv.Names = append(dv.Names, protocol.GatewayName{Src: "ptr", Name: d.name})
		}
		ports := make([]int, 0, len(d.ports))
		for port := range d.ports {
			ports = append(ports, port)
		}
		sort.Ints(ports)
		for _, port := range ports {
			dv.Ports = append(dv.Ports, protocol.GatewayPort{Port: port, Banner: d.ports[port]})
		}
		pass.Devices = append(pass.Devices, dv)
	}
	pass.Answered = len(pass.Devices)
	return pass
}

// gwMethods lists the methods in a fixed order: arp, ping, ports.
func gwMethods(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for _, k := range []string{"arp", "ping", "ports"} {
		if m[k] {
			out = append(out, k)
		}
	}
	return out
}

// gwDefaultGate is the address of the default gateway of the host (the zero address when there is none): the device
// with that address is the router of the network.
func gwDefaultGate() netip.Addr {
	best := -1
	var gate netip.Addr
	for _, r := range gwRoutes() {
		if r.Dest.Bits() == 0 && !r.Direct && (best < 0 || r.Dest.Bits() > best) {
			best, gate = r.Dest.Bits(), r.Gate
		}
	}
	return gate
}
