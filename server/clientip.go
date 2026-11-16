package server

import (
	"net"
	"net/http"
	"net/netip"
	"strings"
	"sync/atomic"
)

// ProxyTrust decides which peers may set X-Forwarded-* headers. The list can be replaced while the server runs
// (the admin edits it in the settings).
type ProxyTrust struct {
	nets atomic.Pointer[[]netip.Prefix]
}

// NewProxyTrust parses CIDR strings.
func NewProxyTrust(cidrs []string) (*ProxyTrust, error) {
	var nets []netip.Prefix
	for _, c := range cidrs {
		pre, err := netip.ParsePrefix(c)
		if err != nil {
			return nil, err
		}
		nets = append(nets, pre)
	}
	p := &ProxyTrust{}
	p.nets.Store(&nets)
	return p, nil
}

// Set replaces the trusted networks.
func (p *ProxyTrust) Set(nets []netip.Prefix) { p.nets.Store(&nets) }

func (p *ProxyTrust) list() []netip.Prefix {
	if n := p.nets.Load(); n != nil {
		return *n
	}
	return nil
}

func (p *ProxyTrust) trusted(a netip.Addr) bool { return inNets(p.list(), a) }

func inNets(nets []netip.Prefix, a netip.Addr) bool {
	a = a.Unmap()
	for _, n := range nets {
		if n.Contains(a) {
			return true
		}
	}
	return false
}

func remoteAddr(r *http.Request) netip.Addr {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	a, _ := netip.ParseAddr(host)
	return a
}

// ClientIP returns the effective client address: walk X-Forwarded-For from the right,
// skipping trusted proxies. If every hop is trusted, the left-most entry is used.
// Callers should also store RemoteAddr, because with a broad trust list the header is spoofable.
func (p *ProxyTrust) ClientIP(r *http.Request) string {
	return effectiveIP(p.list(), remoteAddr(r), r.Header.Get("X-Forwarded-For"))
}

// effectiveIP is ClientIP for a given trust list, peer and X-Forwarded-For value.
func effectiveIP(nets []netip.Prefix, peer netip.Addr, xff string) string {
	peer = peer.Unmap()
	if !peer.IsValid() || !inNets(nets, peer) || xff == "" {
		return peer.String()
	}
	parts := strings.Split(xff, ",")
	for i := len(parts) - 1; i >= 0; i-- {
		a, err := netip.ParseAddr(strings.TrimSpace(parts[i]))
		if err != nil {
			continue
		}
		if !inNets(nets, a) || i == 0 {
			return a.Unmap().String()
		}
	}
	return peer.String()
}

// IsHTTPS reports whether the original request used HTTPS.
func (p *ProxyTrust) IsHTTPS(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	if peer := remoteAddr(r); peer.IsValid() && p.trusted(peer) {
		return strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
	}
	return false
}
