package gwnet

import (
	"errors"
	"net/netip"
	"testing"
)

func TestParse(t *testing.T) {
	ok := map[string]struct {
		cidr string
		kind Kind
	}{
		"192.168.1.0/24":      {"192.168.1.0/24", Private},
		"192.168.1.77/24":     {"192.168.1.0/24", Private}, // the host bits are cleared
		"10.10.0.0/30":        {"10.10.0.0/30", Private},
		"172.16.5.0/25":       {"172.16.5.0/25", Private},
		"100.64.8.0/24":       {"100.64.8.0/24", Private}, // carrier-grade NAT
		"203.0.113.0/24":      {"203.0.113.0/24", Public},
		"172.32.0.0/24":       {"172.32.0.0/24", Public}, // just outside 172.16.0.0/12
		"::ffff:10.0.0.0/120": {"10.0.0.0/24", Private},  // an IPv4-mapped form
	}
	for in, want := range ok {
		n, err := Parse(in)
		if err != nil {
			t.Errorf("%s: %v", in, err)
			continue
		}
		if n.Prefix.String() != want.cidr || n.Kind != want.kind {
			t.Errorf("%s: got %s kind %d, want %s kind %d", in, n.Prefix, n.Kind, want.cidr, want.kind)
		}
	}

	bad := map[string]error{
		"":               ErrSyntax,
		"10.0.0.0":       ErrSyntax,
		"10.0.0.0/33":    ErrSyntax,
		"fe80::/64":      ErrIPv6,
		"127.0.0.0/24":   ErrLoopback,
		"224.0.0.0/24":   ErrReserved,
		"169.254.1.0/24": ErrReserved,
		"0.0.0.0/24":     ErrReserved,
		"240.0.0.0/24":   ErrReserved,
		"10.0.0.0/31":    ErrTooSmall,
		"10.0.0.1/32":    ErrTooSmall,
	}
	for in, want := range bad {
		if _, err := Parse(in); !errors.Is(err, want) {
			t.Errorf("%s: got %v, want %v", in, err, want)
		}
	}

	var se *SizeError
	if _, err := Parse("10.20.0.0/22"); !errors.As(err, &se) || se.Bits != 22 {
		t.Errorf("a /22 must be a size error: %v", err)
	}
	if _, err := Parse("10.0.0.0/8"); !errors.As(err, &se) || se.Bits != 8 {
		t.Errorf("a /8 must be a size error: %v", err)
	}

	AllowLoopback = true
	if _, err := Parse("127.0.0.0/30"); err != nil {
		t.Errorf("the test switch must allow loopback: %v", err)
	}
	AllowLoopback = false
}

func TestSplit24(t *testing.T) {
	parts, err := Split24(netip.MustParsePrefix("10.20.0.0/22"))
	if err != nil || len(parts) != 4 || parts[0].String() != "10.20.0.0/24" || parts[3].String() != "10.20.3.0/24" {
		t.Fatalf("a /22 is four /24: %v %v", parts, err)
	}
	if parts, err = Split24(netip.MustParsePrefix("10.20.7.9/23")); err != nil || parts[0].String() != "10.20.6.0/24" || len(parts) != 2 {
		t.Fatalf("the host bits are ignored: %v %v", parts, err)
	}
	for _, p := range []string{"10.0.0.0/8", "10.0.0.0/15", "10.0.0.0/24", "10.0.0.0/30"} {
		if _, err := Split24(netip.MustParsePrefix(p)); err == nil {
			t.Errorf("%s must not be split", p)
		}
	}
}

func TestHosts(t *testing.T) {
	h := Hosts(netip.MustParsePrefix("192.168.1.0/24"))
	if len(h) != 254 || h[0].String() != "192.168.1.1" || h[253].String() != "192.168.1.254" {
		t.Fatalf("a /24 has 254 hosts: %d %v %v", len(h), h[0], h[len(h)-1])
	}
	if h = Hosts(netip.MustParsePrefix("10.0.0.4/30")); len(h) != 2 || h[0].String() != "10.0.0.5" || h[1].String() != "10.0.0.6" {
		t.Fatalf("a /30 has two hosts: %v", h)
	}
	if h = Hosts(netip.MustParsePrefix("10.1.255.0/24")); len(h) != 254 {
		t.Fatalf("the end of a block: %d", len(h))
	}
}
