package agent

import (
	"context"
	"encoding/binary"
	"net"
	"net/netip"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

// More ways to learn what a device is called and what it announces, after ping and the ports have found it:
// mDNS (Apple, Linux, printers, speakers), NetBIOS (Windows, Samba). Both are one small UDP datagram to the host
// itself, so they work through a router where UDP 5353 and 137 are allowed; neither is sent to a public network.

// Ports and waits; tests point the probes at a responder on loopback.
var (
	gwMDNSPort    = 5353
	gwNetBIOSPort = 137
	gwMDNSWait    = 700 * time.Millisecond
	gwNetBIOSWait = 500 * time.Millisecond
)

// ---- DNS messages (RFC 1035), only what mDNS needs ----

const (
	dnsTypePTR     = 12
	dnsTypeNBSTAT  = 0x21
	dnsClassIN     = 1
	dnsUnicastBit  = 0x8000 // QU: please answer to my own address
	dnsMaxRecords  = 64
	dnsMaxNameJump = 10
)

func dnsEncodeName(name string) []byte {
	var out []byte
	for _, label := range strings.Split(strings.TrimSuffix(name, "."), ".") {
		if label == "" || len(label) > 63 {
			continue
		}
		out = append(out, byte(len(label)&0x3f)) // a label is 1 to 63 bytes
		out = append(out, label...)
	}
	return append(out, 0)
}

// dnsQuestion builds a query for one name.
func dnsQuestion(id uint16, name string, qtype uint16, unicast bool) []byte {
	b := make([]byte, 12)
	binary.BigEndian.PutUint16(b[0:], id)
	binary.BigEndian.PutUint16(b[4:], 1) // one question
	b = append(b, dnsEncodeName(name)...)
	class := uint16(dnsClassIN)
	if unicast {
		class |= dnsUnicastBit
	}
	b = binary.BigEndian.AppendUint16(b, qtype)
	return binary.BigEndian.AppendUint16(b, class)
}

// dnsReadName reads a name that may be compressed; it stops at a malformed or looping message.
func dnsReadName(msg []byte, off int) (string, int, bool) {
	var labels []string
	next, jumped, jumps := 0, false, 0
	for {
		if off < 0 || off >= len(msg) {
			return "", 0, false
		}
		l := int(msg[off])
		switch {
		case l == 0:
			if !jumped {
				next = off + 1
			}
			return strings.Join(labels, "."), next, true
		case l&0xc0 == 0xc0:
			if off+1 >= len(msg) {
				return "", 0, false
			}
			if !jumped {
				next = off + 2
			}
			jumped = true
			if jumps++; jumps > dnsMaxNameJump {
				return "", 0, false
			}
			off = (l&0x3f)<<8 | int(msg[off+1])
		case l&0xc0 != 0:
			return "", 0, false
		default:
			if off+1+l > len(msg) {
				return "", 0, false
			}
			labels = append(labels, string(msg[off+1:off+1+l]))
			off += 1 + l
		}
	}
}

type dnsPTR struct{ Name, Target string }

// dnsParsePTR returns the PTR records of the answer, authority and additional sections.
func dnsParsePTR(msg []byte) []dnsPTR {
	if len(msg) < 12 {
		return nil
	}
	qd := int(binary.BigEndian.Uint16(msg[4:]))
	rrs := int(binary.BigEndian.Uint16(msg[6:])) + int(binary.BigEndian.Uint16(msg[8:])) + int(binary.BigEndian.Uint16(msg[10:]))
	off := 12
	for i := 0; i < qd && i < dnsMaxRecords; i++ {
		_, n, ok := dnsReadName(msg, off)
		if !ok || n+4 > len(msg) {
			return nil
		}
		off = n + 4
	}
	var out []dnsPTR
	for i := 0; i < rrs && i < dnsMaxRecords; i++ {
		name, n, ok := dnsReadName(msg, off)
		if !ok || n+10 > len(msg) {
			return out
		}
		typ := binary.BigEndian.Uint16(msg[n:])
		rdlen := int(binary.BigEndian.Uint16(msg[n+8:]))
		rd := n + 10
		if rd+rdlen > len(msg) {
			return out
		}
		if typ == dnsTypePTR {
			if target, _, ok := dnsReadName(msg, rd); ok {
				out = append(out, dnsPTR{Name: name, Target: target})
			}
		}
		off = rd + rdlen
	}
	return out
}

func reverseName(ip netip.Addr) string {
	b := ip.As4()
	return strconv.Itoa(int(b[3])) + "." + strconv.Itoa(int(b[2])) + "." + strconv.Itoa(int(b[1])) + "." + strconv.Itoa(int(b[0])) + ".in-addr.arpa"
}

var svcTypeRe = regexp.MustCompile(`^_[A-Za-z0-9-]{1,40}\._(tcp|udp)$`)

// stripLocal turns "Anna-iPhone.local" into "Anna-iPhone".
func stripLocal(s string) string {
	return strings.TrimSuffix(strings.TrimSuffix(s, "."), ".local")
}

// gwMDNS asks a host over unicast mDNS what it is called (the reverse name of its own address) and which kinds of
// services it announces. An ephemeral source port makes it a "legacy" query that every responder answers directly.
var gwMDNS = func(ctx context.Context, ip netip.Addr) (string, []string) {
	c, err := net.DialUDP("udp4", nil, &net.UDPAddr{IP: ip.AsSlice(), Port: gwMDNSPort})
	if err != nil {
		return "", nil
	}
	defer func() { _ = c.Close() }()
	_ = c.SetDeadline(time.Now().Add(gwMDNSWait))
	if _, err := c.Write(dnsQuestion(1, reverseName(ip), dnsTypePTR, false)); err != nil {
		return "", nil
	}
	_, _ = c.Write(dnsQuestion(2, "_services._dns-sd._udp.local", dnsTypePTR, false))
	var name string
	var svc []string
	buf := make([]byte, 4096)
	for ctx.Err() == nil {
		n, err := c.Read(buf)
		if err != nil {
			break
		}
		for _, r := range dnsParsePTR(buf[:n]) {
			switch {
			case strings.HasSuffix(r.Name, ".in-addr.arpa") && name == "":
				name = sanitizeBanner(stripLocal(r.Target))
			case strings.EqualFold(r.Name, "_services._dns-sd._udp.local"):
				if s := stripLocal(r.Target); svcTypeRe.MatchString(s) && len(svc) < 32 && !slices.Contains(svc, s) {
					svc = append(svc, s)
				}
			}
		}
	}
	return name, svc
}

// ---- NetBIOS ----

// nbstatRequest asks for the name table of a host ("node status", the question for the wildcard name).
func nbstatRequest(id uint16) []byte {
	b := make([]byte, 12)
	binary.BigEndian.PutUint16(b[0:], id)
	binary.BigEndian.PutUint16(b[4:], 1)
	b = append(b, 0x20)
	b = append(b, "CKAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"...) // "*" and fifteen zero bytes in the first-level encoding
	b = append(b, 0)
	b = binary.BigEndian.AppendUint16(b, dnsTypeNBSTAT)
	return binary.BigEndian.AppendUint16(b, dnsClassIN)
}

// parseNBSTAT returns the computer name (the first unique name of the workstation service) from an answer.
func parseNBSTAT(msg []byte) string {
	if len(msg) < 12 || binary.BigEndian.Uint16(msg[6:]) == 0 {
		return ""
	}
	off := 12
	for i := 0; i < int(binary.BigEndian.Uint16(msg[4:])); i++ {
		_, n, ok := dnsReadName(msg, off)
		if !ok {
			return ""
		}
		off = n + 4
	}
	_, n, ok := dnsReadName(msg, off)
	if !ok || n+11 > len(msg) || binary.BigEndian.Uint16(msg[n:]) != dnsTypeNBSTAT {
		return ""
	}
	rd := n + 10
	count := int(msg[rd])
	rd++
	for i := 0; i < count && rd+18 <= len(msg); i++ {
		entry := msg[rd : rd+18]
		rd += 18
		flags := binary.BigEndian.Uint16(entry[16:])
		if entry[15] == 0x00 && flags&0x8000 == 0 { // workstation, not a group
			return sanitizeBanner(strings.TrimRight(string(entry[:15]), " \x00"))
		}
	}
	return ""
}

// gwNetBIOS asks a host for its NetBIOS computer name.
var gwNetBIOS = func(ctx context.Context, ip netip.Addr) string {
	c, err := net.DialUDP("udp4", nil, &net.UDPAddr{IP: ip.AsSlice(), Port: gwNetBIOSPort})
	if err != nil {
		return ""
	}
	defer func() { _ = c.Close() }()
	_ = c.SetDeadline(time.Now().Add(gwNetBIOSWait))
	if _, err := c.Write(nbstatRequest(1)); err != nil {
		return ""
	}
	buf := make([]byte, 1500)
	for ctx.Err() == nil {
		n, err := c.Read(buf)
		if err != nil {
			return ""
		}
		if name := parseNBSTAT(buf[:n]); name != "" {
			return name
		}
	}
	return ""
}
