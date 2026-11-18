package agent

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"net/netip"
	"os"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode"

	"github.com/bolgov0zero/linager/internal/gwnet"
	"github.com/bolgov0zero/linager/internal/protocol"
)

// The probes of the gateway module. Nothing here reaches beyond the networks the module was told to scan, and every
// probe goes through a limiter, so one pass is a gentle trickle of small packets and not a burst.

// The TCP ports looked at on a host that is up, and the shorter list used to find a host that answers neither ARP
// nor ping (many devices drop ping but have a web page or a share). Tests replace them.
var (
	gwPortList     = []int{22, 80, 443, 445, 3389, 8006, 5000, 5001, 9100, 554, 8080, 8291, 1883, 1400, 8001}
	gwLivePortList = []int{22, 80, 443, 445, 3389}
)

// Timeouts and rates; tests make them short.
var (
	gwProbeTimeout = 400 * time.Millisecond
	gwArpSettle    = 2500 * time.Millisecond // the kernel gives up on a host that does not answer ARP after about 3 s
	gwPingWait     = 1500 * time.Millisecond
	gwBannerWait   = 800 * time.Millisecond
	gwNameWait     = 800 * time.Millisecond
)

const (
	gwWorkers     = 64
	gwRatePrivate = 200 // probes per second in a private network
	gwRatePublic  = 50  // and in a public one
	gwBannerMax   = 120 // characters kept of a banner
)

// gwLimiter spaces probes evenly: one every `every`, whoever asks.
type gwLimiter struct {
	mu    sync.Mutex
	every time.Duration
	next  time.Time
}

func newGwLimiter(perSecond int) *gwLimiter {
	if perSecond < 1 {
		perSecond = 1
	}
	return &gwLimiter{every: time.Second / time.Duration(perSecond)}
}

func (l *gwLimiter) wait(ctx context.Context) error {
	l.mu.Lock()
	now := time.Now()
	if l.next.Before(now) {
		l.next = now
	}
	at := l.next
	l.next = l.next.Add(l.every)
	l.mu.Unlock()
	d := time.Until(at)
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// ---- the networks of the host ----

// gwIface is an IPv4 address of a network card with the real subnet it belongs to.
type gwIface struct {
	Name   string
	Addr   netip.Addr
	Subnet netip.Prefix // masked
}

// gwVirtualPrefixes are cards of containers and virtual bridges: their networks are not the LAN of the host.
var gwVirtualPrefixes = []string{"docker", "br-", "veth", "virbr", "cni", "flannel", "cali", "kube", "lo"}

// gwInterfaces lists the private IPv4 networks the host is directly connected to. A test replaces it.
var gwInterfaces = func() []gwIface {
	ifs, err := net.Interfaces()
	if err != nil {
		return nil
	}
	var out []gwIface
	for _, ni := range ifs {
		if ni.Flags&net.FlagUp == 0 || ni.Flags&net.FlagLoopback != 0 || gwVirtualName(ni.Name) {
			continue
		}
		addrs, err := ni.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			ipn, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			addr, ok := netip.AddrFromSlice(ipn.IP)
			if !ok || !addr.Unmap().Is4() {
				continue
			}
			addr = addr.Unmap()
			ones, _ := ipn.Mask.Size()
			if gwnet.Classify(addr) != gwnet.Private || addr.IsLinkLocalUnicast() {
				continue
			}
			out = append(out, gwIface{Name: ni.Name, Addr: addr, Subnet: netip.PrefixFrom(addr, ones).Masked()})
		}
	}
	return out
}

func gwVirtualName(name string) bool {
	for _, p := range gwVirtualPrefixes {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}

// gwLocalAddrs are the addresses of this host: they are never reported as devices. A test replaces it.
var gwLocalAddrs = func() map[netip.Addr]bool {
	out := map[netip.Addr]bool{}
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return out
	}
	for _, a := range addrs {
		if ipn, ok := a.(*net.IPNet); ok {
			if addr, ok := netip.AddrFromSlice(ipn.IP); ok {
				out[addr.Unmap()] = true
			}
		}
	}
	return out
}

// gwOwnNets offers the /24 around the address of each card (a card with a smaller network offers that network).
func gwOwnNets(ifs []gwIface) []protocol.GatewayOwnNet {
	seen := map[string]bool{}
	var out []protocol.GatewayOwnNet
	for _, f := range ifs {
		p := f.Subnet
		if p.Bits() < gwnet.MinBits {
			p = netip.PrefixFrom(f.Addr, gwnet.MinBits).Masked()
		}
		if p.Bits() > gwnet.MaxBits || seen[p.String()] {
			continue
		}
		seen[p.String()] = true
		out = append(out, protocol.GatewayOwnNet{CIDR: p.String(), Iface: f.Name, VLAN: gwnet.VLAN(f.Name)})
	}
	return out
}

// gwOwnIface finds the card that is connected to a network directly: the real subnet of the card contains it.
func gwOwnIface(ifs []gwIface, p netip.Prefix) (gwIface, bool) {
	for _, f := range ifs {
		if f.Subnet.Bits() <= p.Bits() && f.Subnet.Contains(p.Addr()) {
			return f, true
		}
	}
	return gwIface{}, false
}

// ---- the routing table ----

type gwRouteEntry struct {
	Iface  string
	Dest   netip.Prefix
	Gate   netip.Addr
	Direct bool // no gateway: the network is attached to the card
}

// parseRoutes reads /proc/net/route. The addresses are hexadecimal numbers in the byte order of the host (little
// endian on every platform the agent supports).
func parseRoutes(r io.Reader) []gwRouteEntry {
	var out []gwRouteEntry
	sc := bufio.NewScanner(r)
	first := true
	for sc.Scan() {
		if first {
			first = false
			continue
		}
		f := strings.Fields(sc.Text())
		if len(f) < 8 {
			continue
		}
		dest, ok1 := hexAddr(f[1])
		gate, ok2 := hexAddr(f[2])
		mask, ok3 := hexAddr(f[7])
		flags, err := strconv.ParseUint(f[3], 16, 32)
		if !ok1 || !ok2 || !ok3 || err != nil || flags&1 == 0 { // RTF_UP
			continue
		}
		ones := maskBits(mask)
		if ones < 0 {
			continue
		}
		out = append(out, gwRouteEntry{Iface: f[0], Dest: netip.PrefixFrom(dest, ones), Gate: gate, Direct: gate.IsUnspecified()})
	}
	return out
}

func hexAddr(s string) (netip.Addr, bool) {
	v, err := strconv.ParseUint(s, 16, 32)
	if err != nil {
		return netip.Addr{}, false
	}
	var b [4]byte
	binary.LittleEndian.PutUint32(b[:], uint32(v))
	return netip.AddrFrom4(b), true
}

func maskBits(m netip.Addr) int {
	b := m.As4()
	v := uint32(b[0])<<24 | uint32(b[1])<<16 | uint32(b[2])<<8 | uint32(b[3])
	ones := 0
	for v&0x80000000 != 0 {
		ones++
		v <<= 1
	}
	if v != 0 {
		return -1
	}
	return ones
}

// bestRoute is the longest matching route.
func bestRoute(routes []gwRouteEntry, a netip.Addr) (gwRouteEntry, bool) {
	best := -1
	var out gwRouteEntry
	for _, r := range routes {
		if r.Dest.Contains(a) && r.Dest.Bits() > best {
			best, out = r.Dest.Bits(), r
		}
	}
	return out, best >= 0
}

// gwRoutes reads the routing table of the host. A test replaces it.
var gwRoutes = func() []gwRouteEntry {
	f, err := os.Open("/proc/net/route")
	if err != nil {
		return nil
	}
	defer func() { _ = f.Close() }()
	return parseRoutes(f)
}

// ---- ARP ----

type arpEntry struct {
	IP    netip.Addr
	MAC   string
	Iface string
}

// parseARP reads /proc/net/arp and keeps the complete entries (flag 0x2): the host answered.
func parseARP(r io.Reader) []arpEntry {
	var out []arpEntry
	sc := bufio.NewScanner(r)
	first := true
	for sc.Scan() {
		if first {
			first = false
			continue
		}
		f := strings.Fields(sc.Text())
		if len(f) < 6 {
			continue
		}
		flags, err := strconv.ParseUint(strings.TrimPrefix(f[2], "0x"), 16, 32)
		ip, err2 := netip.ParseAddr(f[0])
		if err != nil || err2 != nil || flags&2 == 0 || f[3] == "00:00:00:00:00:00" {
			continue
		}
		out = append(out, arpEntry{IP: ip, MAC: strings.ToLower(f[3]), Iface: f[5]})
	}
	return out
}

// gwARPTable reads the neighbour table of the host. A test replaces it.
var gwARPTable = func() []arpEntry {
	f, err := os.Open("/proc/net/arp")
	if err != nil {
		return nil
	}
	defer func() { _ = f.Close() }()
	return parseARP(f)
}

// arpKick makes the kernel ask who has an address: a datagram to the discard port needs the hardware address first.
func arpKick(ip netip.Addr) {
	c, err := net.DialUDP("udp4", nil, &net.UDPAddr{IP: ip.AsSlice(), Port: 9})
	if err != nil {
		return
	}
	_, _ = c.Write([]byte{0})
	_ = c.Close()
}

// ---- ICMP ----

// gwPingAvailable tells whether this host may send raw ICMP (the agent runs as root, but a container may forbid it).
var gwPingAvailable = func() bool {
	c, err := net.ListenPacket("ip4:icmp", "0.0.0.0")
	if err != nil {
		return false
	}
	_ = c.Close()
	return true
}

func icmpChecksum(b []byte) uint16 {
	var sum uint32
	for i := 0; i+1 < len(b); i += 2 {
		sum += uint32(b[i])<<8 | uint32(b[i+1])
	}
	if len(b)%2 == 1 {
		sum += uint32(b[len(b)-1]) << 8
	}
	for sum>>16 != 0 {
		sum = sum&0xffff + sum>>16
	}
	return ^uint16(sum)
}

func echoRequest(id, seq uint16) []byte {
	b := make([]byte, 8+16)
	b[0], b[1] = 8, 0
	binary.BigEndian.PutUint16(b[4:], id)
	binary.BigEndian.PutUint16(b[6:], seq)
	copy(b[8:], "linager-gateway!")
	binary.BigEndian.PutUint16(b[2:], icmpChecksum(b))
	return b
}

// gwPing sends one echo request to each host and returns the hosts that answered.
var gwPing = func(ctx context.Context, hosts []netip.Addr, lim *gwLimiter) (map[netip.Addr]bool, error) {
	c, err := net.ListenPacket("ip4:icmp", "0.0.0.0")
	if err != nil {
		return nil, err
	}
	defer func() { _ = c.Close() }()
	var idb [2]byte
	_, _ = rand.Read(idb[:])
	id := binary.BigEndian.Uint16(idb[:])
	want := map[netip.Addr]bool{}
	for _, h := range hosts {
		want[h] = true
	}
	alive := map[netip.Addr]bool{}
	var mu sync.Mutex
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		buf := make([]byte, 1500)
		for {
			_ = c.SetReadDeadline(time.Now().Add(150 * time.Millisecond))
			n, from, err := c.ReadFrom(buf)
			if err != nil {
				select {
				case <-stop:
					return
				default:
				}
				var ne net.Error
				if errors.As(err, &ne) && ne.Timeout() {
					continue
				}
				return
			}
			if n < 8 || buf[0] != 0 || binary.BigEndian.Uint16(buf[4:6]) != id { // echo reply of ours
				continue
			}
			if ia, ok := from.(*net.IPAddr); ok {
				if a, ok := netip.AddrFromSlice(ia.IP); ok && want[a.Unmap()] {
					mu.Lock()
					alive[a.Unmap()] = true
					mu.Unlock()
				}
			}
		}
	}()
	for i, h := range hosts {
		if err := lim.wait(ctx); err != nil {
			break
		}
		_, _ = c.WriteTo(echoRequest(id, uint16(i)), &net.IPAddr{IP: h.AsSlice()}) //nolint:gosec // i is a host index below 65536
	}
	t := time.NewTimer(gwPingWait)
	select {
	case <-ctx.Done():
	case <-t.C:
	}
	t.Stop()
	close(stop)
	<-done
	mu.Lock()
	defer mu.Unlock()
	return alive, ctx.Err()
}

// ---- TCP ----

// gwTCP connects to a port: open when it accepted, alive when it accepted or refused (a closed port still means the
// host answered), neither on a timeout.
var gwTCP = func(ctx context.Context, ip netip.Addr, port int) (open, alive bool) {
	d := net.Dialer{Timeout: gwProbeTimeout}
	c, err := d.DialContext(ctx, "tcp4", net.JoinHostPort(ip.String(), strconv.Itoa(port)))
	if err == nil {
		_ = c.Close()
		return true, true
	}
	return false, errors.Is(err, syscall.ECONNREFUSED)
}

// gwBanner reads what a service says about itself: the first line of SSH, the Server header and the title of a web
// page. It returns "" when there is nothing useful. It only reads; nothing is sent but a plain GET request.
var gwBanner = func(ctx context.Context, ip netip.Addr, port int) string {
	addr := net.JoinHostPort(ip.String(), strconv.Itoa(port))
	d := net.Dialer{Timeout: gwProbeTimeout}
	switch port {
	case 22:
		c, err := d.DialContext(ctx, "tcp4", addr)
		if err != nil {
			return ""
		}
		defer func() { _ = c.Close() }()
		return readSSHBanner(c)
	case 80, 8080, 5000:
		c, err := d.DialContext(ctx, "tcp4", addr)
		if err != nil {
			return ""
		}
		defer func() { _ = c.Close() }()
		return httpBanner(c, ip.String())
	case 443, 8006, 5001:
		td := tls.Dialer{NetDialer: &d, Config: &tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS12}} //nolint:gosec // only reads the page of a device in the LAN, nothing secret is sent
		c, err := td.DialContext(ctx, "tcp4", addr)
		if err != nil {
			return ""
		}
		defer func() { _ = c.Close() }()
		return httpBanner(c, ip.String())
	}
	return ""
}

// readSSHBanner reads the identification line an SSH server sends first ("SSH-2.0-OpenSSH_9.2p1 Debian-2+deb12u3").
func readSSHBanner(c net.Conn) string {
	_ = c.SetReadDeadline(time.Now().Add(gwBannerWait))
	line, _ := bufio.NewReaderSize(io.LimitReader(c, 256), 256).ReadString('\n')
	if !strings.HasPrefix(line, "SSH-") {
		return ""
	}
	return sanitizeBanner(line)
}

func httpBanner(c net.Conn, host string) string {
	_ = c.SetDeadline(time.Now().Add(gwBannerWait + time.Second))
	if _, err := c.Write([]byte("GET / HTTP/1.0\r\nHost: " + host + "\r\nUser-Agent: linager-gateway\r\nAccept: text/html\r\nConnection: close\r\n\r\n")); err != nil {
		return ""
	}
	raw, _ := io.ReadAll(io.LimitReader(c, 8192))
	return parseHTTPBanner(raw)
}

// parseHTTPBanner takes the Server header and the <title> from the first bytes of an answer.
func parseHTTPBanner(raw []byte) string {
	s := string(raw)
	head, body := s, ""
	if i := strings.Index(s, "\r\n\r\n"); i >= 0 {
		head, body = s[:i], s[i+4:]
	}
	var server string
	for _, line := range strings.Split(head, "\r\n") {
		if k, v, ok := strings.Cut(line, ":"); ok && strings.EqualFold(strings.TrimSpace(k), "server") {
			server = strings.TrimSpace(v)
		}
	}
	var title string
	low := strings.ToLower(body)
	if i := strings.Index(low, "<title"); i >= 0 {
		if j := strings.Index(low[i:], ">"); j >= 0 {
			rest := body[i+j+1:]
			if k := strings.Index(strings.ToLower(rest), "</title>"); k >= 0 {
				title = strings.TrimSpace(rest[:k])
			}
		}
	}
	switch {
	case server != "" && title != "":
		return sanitizeBanner(server + " · " + title)
	case title != "":
		return sanitizeBanner(title)
	}
	return sanitizeBanner(server)
}

// sanitizeBanner drops control characters and cuts the text: it comes from a device that may say anything.
func sanitizeBanner(s string) string {
	var b strings.Builder
	n := 0
	for _, r := range strings.TrimSpace(s) {
		if unicode.IsControl(r) || r == unicode.ReplacementChar {
			r = ' '
		}
		b.WriteRune(r)
		if n++; n >= gwBannerMax {
			break
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

// gwPTR asks the resolver of the host for the name of an address.
var gwPTR = func(ctx context.Context, ip netip.Addr) string {
	ctx, cancel := context.WithTimeout(ctx, gwNameWait)
	defer cancel()
	names, err := net.DefaultResolver.LookupAddr(ctx, ip.String())
	if err != nil || len(names) == 0 {
		return ""
	}
	return sanitizeBanner(strings.TrimSuffix(names[0], "."))
}
