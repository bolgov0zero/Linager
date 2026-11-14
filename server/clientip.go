package server

import (
	"net"
	"net/http"
	"net/netip"
	"strings"
)

// ProxyTrust decides which peers may set X-Forwarded-* headers.
type ProxyTrust struct{ nets []netip.Prefix }

// NewProxyTrust parses CIDR strings.
func NewProxyTrust(cidrs []string) (*ProxyTrust, error) {
	var p ProxyTrust
	for _, c := range cidrs {
		pre, err := netip.ParsePrefix(c)
		if err != nil {
			return nil, err
		}
		p.nets = append(p.nets, pre)
	}
	return &p, nil
}

func (p *ProxyTrust) trusted(a netip.Addr) bool {
	a = a.Unmap()
	for _, n := range p.nets {
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
	peer := remoteAddr(r)
	if !peer.IsValid() || !p.trusted(peer) {
		return peer.String()
	}
	xff := r.Header.Get("X-Forwarded-For")
	if xff == "" {
		return peer.String()
	}
	parts := strings.Split(xff, ",")
	for i := len(parts) - 1; i >= 0; i-- {
		a, err := netip.ParseAddr(strings.TrimSpace(parts[i]))
		if err != nil {
			continue
		}
		if !p.trusted(a) || i == 0 {
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
