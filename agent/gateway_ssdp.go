package agent

import (
	"context"
	"encoding/xml"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sys/unix"

	"github.com/bolgov0zero/linager/internal/protocol"
)

// SSDP and UPnP: TVs, speakers, routers and printers answer a multicast search and describe themselves in a small XML
// document. The search stays in the network the agent is attached to (multicast does not cross a router). The
// description is fetched only from the address that answered, over plain HTTP, without redirects, with a size and
// time limit: an answer that names another host in its LOCATION cannot make the agent talk to it.

var (
	gwSSDPAddr = "239.255.255.250:1900"
	gwSSDPWait = 2500 * time.Millisecond
	gwUPnPWait = 1500 * time.Millisecond
)

const gwUPnPMax = 32 << 10

// gwUPnPInfo is what a device told about itself.
type gwUPnPInfo struct {
	Name string
	UPnP protocol.GatewayUPnP
}

// parseSSDPLocation reads the LOCATION header of an answer to a search.
func parseSSDPLocation(b []byte) string {
	for _, line := range strings.Split(string(b), "\r\n") {
		if k, v, ok := strings.Cut(line, ":"); ok && strings.EqualFold(strings.TrimSpace(k), "location") {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

type upnpDescription struct {
	Device struct {
		DeviceType   string `xml:"deviceType"`
		FriendlyName string `xml:"friendlyName"`
		Manufacturer string `xml:"manufacturer"`
		ModelName    string `xml:"modelName"`
	} `xml:"device"`
}

// parseUPnPDescription reads the description document of a device.
func parseUPnPDescription(r io.Reader) (gwUPnPInfo, bool) {
	var d upnpDescription
	if err := xml.NewDecoder(io.LimitReader(r, gwUPnPMax)).Decode(&d); err != nil {
		return gwUPnPInfo{}, false
	}
	typ := d.Device.DeviceType // urn:schemas-upnp-org:device:MediaRenderer:1
	if parts := strings.Split(typ, ":"); len(parts) >= 2 {
		typ = parts[len(parts)-2]
	}
	info := gwUPnPInfo{
		Name: sanitizeBanner(d.Device.FriendlyName),
		UPnP: protocol.GatewayUPnP{Type: sanitizeBanner(typ), Model: sanitizeBanner(d.Device.ModelName), Maker: sanitizeBanner(d.Device.Manufacturer)},
	}
	return info, info.Name != "" || info.UPnP.Type != "" || info.UPnP.Model != "" || info.UPnP.Maker != ""
}

// fetchUPnP reads the description a device points to, if the address in it is the device's own.
func fetchUPnP(ctx context.Context, ip netip.Addr, location string) (gwUPnPInfo, bool) {
	u, err := url.Parse(location)
	if err != nil || u.Scheme != "http" || u.Hostname() != ip.String() || len(location) > 512 {
		return gwUPnPInfo{}, false
	}
	if p := u.Port(); p != "" {
		if n, err := strconv.Atoi(p); err != nil || n < 1 || n > 65535 {
			return gwUPnPInfo{}, false
		}
	}
	ctx, cancel := context.WithTimeout(ctx, gwUPnPWait)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return gwUPnPInfo{}, false
	}
	req.Header.Set("User-Agent", "linager-gateway")
	client := &http.Client{
		Timeout:       gwUPnPWait,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		Transport:     &http.Transport{Proxy: nil, DisableKeepAlives: true},
	}
	resp, err := client.Do(req) //nolint:bodyclose,gosec // closed below; the URL was checked to be the device's own address
	if err != nil {
		return gwUPnPInfo{}, false
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return gwUPnPInfo{}, false
	}
	return parseUPnPDescription(resp.Body)
}

// gwSSDP searches the network of one card and returns the descriptions of the hosts in `want`. A test replaces it.
var gwSSDP = func(ctx context.Context, local netip.Addr, want map[netip.Addr]bool) map[netip.Addr]gwUPnPInfo {
	lc := net.ListenConfig{Control: func(_, _ string, rc syscall.RawConn) error {
		var serr error
		if err := rc.Control(func(fd uintptr) {
			serr = unix.SetsockoptInet4Addr(int(fd), unix.IPPROTO_IP, unix.IP_MULTICAST_IF, local.As4()) //nolint:gosec // a file descriptor fits an int
		}); err != nil {
			return err
		}
		return serr
	}}
	pc, err := lc.ListenPacket(ctx, "udp4", net.JoinHostPort(local.String(), "0"))
	if err != nil {
		return nil
	}
	defer func() { _ = pc.Close() }()
	dst, err := net.ResolveUDPAddr("udp4", gwSSDPAddr)
	if err != nil {
		return nil
	}
	search := []byte("M-SEARCH * HTTP/1.1\r\nHOST: 239.255.255.250:1900\r\nMAN: \"ssdp:discover\"\r\nMX: 2\r\nST: upnp:rootdevice\r\n\r\n")
	for i := 0; i < 2; i++ {
		_, _ = pc.WriteTo(search, dst)
		time.Sleep(150 * time.Millisecond)
	}
	_ = pc.SetReadDeadline(time.Now().Add(gwSSDPWait))
	loc := map[netip.Addr]string{}
	buf := make([]byte, 4096)
	for ctx.Err() == nil {
		n, from, err := pc.ReadFrom(buf)
		if err != nil {
			break
		}
		ua, ok := from.(*net.UDPAddr)
		if !ok {
			continue
		}
		a, ok := netip.AddrFromSlice(ua.IP)
		if !ok || !want[a.Unmap()] {
			continue
		}
		if l := parseSSDPLocation(buf[:n]); l != "" && loc[a.Unmap()] == "" {
			loc[a.Unmap()] = l
		}
	}
	out := map[netip.Addr]gwUPnPInfo{}
	var mu sync.Mutex
	ips := make([]netip.Addr, 0, len(loc))
	for a := range loc {
		ips = append(ips, a)
	}
	gwEach(ctx, 8, len(ips), func(i int) {
		if info, ok := fetchUPnP(ctx, ips[i], loc[ips[i]]); ok {
			mu.Lock()
			out[ips[i]] = info
			mu.Unlock()
		}
	})
	return out
}
