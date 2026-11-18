// Package gwnet holds the rules of the gateway module about which networks may be scanned. The server checks a network
// when the administrator saves it and the agent checks it again before it scans, so a wrong or hostile configuration
// cannot make an agent scan more than the rules allow: the one implementation is shared by both.
package gwnet

import (
	"encoding/binary"
	"errors"
	"fmt"
	"net/netip"
	"strconv"
	"strings"
)

const (
	// MaxNets is how many networks one gateway may have.
	MaxNets = 16
	// MinBits is the shortest prefix that may be scanned: a /24 (254 addresses). Larger networks are split by the caller.
	MinBits = 24
	// MaxBits is the longest prefix: /30 (two hosts). A /31 and a /32 are not networks to look for devices in.
	MaxBits = 30
)

// Kind tells whether a network is private (RFC 1918 and the carrier-grade range) or public.
type Kind int

const (
	// Private networks are scanned without a question.
	Private Kind = iota + 1
	// Public networks are scanned only after the administrator confirmed that this is allowed.
	Public
)

// AllowLoopback lets tests scan 127.0.0.0/8; the rules forbid it in production.
var AllowLoopback bool

// Errors of Parse. A network that is too large is reported as a *SizeError so the caller can offer to split it.
var (
	ErrSyntax   = errors.New("not a network such as 192.168.30.0/24")
	ErrIPv6     = errors.New("only IPv4 networks are scanned")
	ErrLoopback = errors.New("loopback cannot be scanned")
	ErrReserved = errors.New("multicast, link-local and reserved addresses cannot be scanned")
	ErrTooSmall = errors.New("a network must be from /30 to /24")
)

// SizeError is a network larger than /24.
type SizeError struct{ Bits int }

func (e *SizeError) Error() string {
	return fmt.Sprintf("a /%d is larger than /24; split it into networks of /24", e.Bits)
}

// Net is a network that passed the rules.
type Net struct {
	Prefix netip.Prefix // masked: the host bits are cleared
	Kind   Kind
}

// Parse checks a network written as an address with a prefix length and returns it masked.
func Parse(s string) (Net, error) {
	p, err := netip.ParsePrefix(s)
	if err != nil {
		return Net{}, ErrSyntax
	}
	a := p.Addr().Unmap()
	bits := p.Bits()
	if p.Addr().Is4In6() {
		// an IPv4-mapped network is counted in IPv6 bits: ::ffff:10.0.0.0/120 is 10.0.0.0/24
		if bits < 96 {
			return Net{}, ErrIPv6
		}
		bits -= 96
	}
	if !a.Is4() {
		return Net{}, ErrIPv6
	}
	p = netip.PrefixFrom(a, bits)
	switch {
	case a.IsLoopback():
		if !AllowLoopback {
			return Net{}, ErrLoopback
		}
	case a.IsMulticast(), a.IsLinkLocalUnicast(), a.IsUnspecified(), a.As4()[0] >= 240:
		return Net{}, ErrReserved
	}
	if p.Bits() > MaxBits {
		return Net{}, ErrTooSmall
	}
	if p.Bits() < MinBits {
		return Net{}, &SizeError{Bits: p.Bits()}
	}
	p = p.Masked()
	if a.IsLoopback() { // only reachable by the test switch
		return Net{Prefix: p, Kind: Private}, nil
	}
	return Net{Prefix: p, Kind: Classify(p.Addr())}, nil
}

// Classify says whether an address lies in a private range.
func Classify(a netip.Addr) Kind {
	if a.IsPrivate() || netip.MustParsePrefix("100.64.0.0/10").Contains(a) {
		return Private
	}
	return Public
}

// Split24 cuts a network of /16 to /23 into networks of /24; a larger or a smaller one is not split.
func Split24(p netip.Prefix) ([]netip.Prefix, error) {
	if p.Bits() < 16 || p.Bits() >= MinBits || !p.Addr().Is4() {
		return nil, fmt.Errorf("a network of /%d is not split", p.Bits())
	}
	p = p.Masked()
	n := 1 << (MinBits - p.Bits())
	out := make([]netip.Prefix, 0, n)
	a := p.Addr()
	for i := 0; i < n; i++ {
		out = append(out, netip.PrefixFrom(a, MinBits))
		b := a.As4()
		b[2]++
		a = netip.AddrFrom4(b)
	}
	return out, nil
}

// Hosts lists the addresses of a network that can hold a device: without the network and the broadcast address.
func Hosts(p netip.Prefix) []netip.Addr {
	p = p.Masked()
	var out []netip.Addr
	last := lastAddr(p)
	for a := p.Addr().Next(); a.IsValid() && a.Less(last); a = a.Next() {
		out = append(out, a)
	}
	return out
}

func lastAddr(p netip.Prefix) netip.Addr {
	b := p.Addr().As4()
	host := 32 - p.Bits()
	v := binary.BigEndian.Uint32(b[:]) | (1<<host - 1)
	binary.BigEndian.PutUint32(b[:], v)
	return netip.AddrFrom4(b)
}

// Overlap reports whether two networks share addresses (one contains the other).
func Overlap(a, b netip.Prefix) bool { return a.Overlaps(b) }

// VLAN names the VLAN of a network card called eth0.20: "VLAN 20"; a card without a VLAN gives "".
func VLAN(iface string) string {
	if i := strings.LastIndex(iface, "."); i > 0 {
		if _, err := strconv.Atoi(iface[i+1:]); err == nil {
			return "VLAN " + iface[i+1:]
		}
	}
	return ""
}
