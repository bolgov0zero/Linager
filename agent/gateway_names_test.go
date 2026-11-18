package agent

import (
	"context"
	"encoding/binary"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"slices"
	"strings"
	"testing"
	"time"
)

// dnsRecord builds a PTR record for a test answer.
func dnsRecord(name, target string) []byte {
	b := dnsEncodeName(name)
	b = binary.BigEndian.AppendUint16(b, dnsTypePTR)
	b = binary.BigEndian.AppendUint16(b, dnsClassIN)
	b = binary.BigEndian.AppendUint32(b, 120)
	rd := dnsEncodeName(target)
	b = binary.BigEndian.AppendUint16(b, uint16(len(rd)&0xffff))
	return append(b, rd...)
}

func dnsAnswer(id uint16, records ...[]byte) []byte {
	b := make([]byte, 12)
	binary.BigEndian.PutUint16(b[0:], id)
	binary.BigEndian.PutUint16(b[2:], 0x8400)
	binary.BigEndian.PutUint16(b[6:], uint16(len(records)&0xffff))
	for _, r := range records {
		b = append(b, r...)
	}
	return b
}

func TestDNSWire(t *testing.T) {
	q := dnsQuestion(7, "_services._dns-sd._udp.local", dnsTypePTR, true)
	if binary.BigEndian.Uint16(q[0:]) != 7 || binary.BigEndian.Uint16(q[4:]) != 1 {
		t.Errorf("header: %v", q[:12])
	}
	if name, _, ok := dnsReadName(q, 12); !ok || name != "_services._dns-sd._udp.local" {
		t.Errorf("the question name: %q %v", name, ok)
	}
	if binary.BigEndian.Uint16(q[len(q)-2:]) != dnsClassIN|dnsUnicastBit {
		t.Errorf("the unicast-response bit is set in the class")
	}
	if reverseName(netip.MustParseAddr("192.168.1.41")) != "41.1.168.192.in-addr.arpa" {
		t.Errorf("reverse name")
	}

	msg := dnsAnswer(1,
		dnsRecord("41.1.168.192.in-addr.arpa", "nas-dacha.local"),
		dnsRecord("_services._dns-sd._udp.local", "_smb._tcp.local"),
		dnsRecord("_services._dns-sd._udp.local", "_ipp._tcp.local"))
	got := dnsParsePTR(msg)
	if len(got) != 3 || got[0].Target != "nas-dacha.local" || got[1].Target != "_smb._tcp.local" || stripLocal(got[2].Target) != "_ipp._tcp" {
		t.Fatalf("records: %+v", got)
	}

	// a compressed name: the target points back into the message
	comp := dnsAnswer(1, dnsRecord("a.b.local", "x"))
	_ = comp
	pointer := append(dnsAnswer(1), 0x01, 'z', 0xc0, 0x0c) // "z" followed by a pointer to offset 12 (itself): a loop
	if _, _, ok := dnsReadName(pointer, 12); ok {
		t.Errorf("a name that points to itself must be refused")
	}
	if _, _, ok := dnsReadName([]byte{0xc0}, 0); ok {
		t.Errorf("a cut pointer must be refused")
	}
	for i := range msg { // every truncation of a message is survived
		_ = dnsParsePTR(msg[:i])
	}
	if len(dnsParsePTR(nil)) != 0 || len(dnsParsePTR([]byte("junk"))) != 0 {
		t.Errorf("garbage gives no records")
	}
}

func nbstatAnswer(entries ...[3]any) []byte {
	b := make([]byte, 12)
	binary.BigEndian.PutUint16(b[6:], 1)
	b = append(b, 0x20)
	b = append(b, "CKAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"...)
	b = append(b, 0)
	b = binary.BigEndian.AppendUint16(b, dnsTypeNBSTAT)
	b = binary.BigEndian.AppendUint16(b, dnsClassIN)
	b = binary.BigEndian.AppendUint32(b, 0)
	rd := []byte{byte(len(entries) & 0xff)}
	for _, e := range entries {
		name := []byte(e[0].(string) + strings.Repeat(" ", 15))[:15]
		rd = append(rd, name...)
		rd = append(rd, e[1].(byte))
		rd = binary.BigEndian.AppendUint16(rd, e[2].(uint16))
	}
	b = binary.BigEndian.AppendUint16(b, uint16(len(rd)&0xffff))
	return append(b, rd...)
}

func TestNBSTAT(t *testing.T) {
	req := nbstatRequest(9)
	if len(req) != 50 || binary.BigEndian.Uint16(req[46:]) != dnsTypeNBSTAT {
		t.Errorf("request: %d bytes", len(req))
	}
	// the group name (WORKGROUP) comes first: the computer name is the first unique name of the workstation service
	ans := nbstatAnswer([3]any{"WORKGROUP", byte(0x00), uint16(0x8400)}, [3]any{"OLD-LAPTOP", byte(0x00), uint16(0x0400)}, [3]any{"OLD-LAPTOP", byte(0x20), uint16(0x0400)})
	if got := parseNBSTAT(ans); got != "OLD-LAPTOP" {
		t.Errorf("computer name: %q", got)
	}
	if parseNBSTAT(nbstatAnswer([3]any{"WORKGROUP", byte(0x00), uint16(0x8400)})) != "" {
		t.Errorf("only a group name: no computer name")
	}
	for i := range ans {
		_ = parseNBSTAT(ans[:i])
	}
	if parseNBSTAT([]byte("junk")) != "" {
		t.Errorf("junk")
	}
}

// udpResponder answers every datagram with what reply makes of it, on 127.0.0.1.
func udpResponder(t *testing.T, reply func(req []byte) [][]byte) int {
	t.Helper()
	c, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Skip("no loopback UDP")
	}
	t.Cleanup(func() { _ = c.Close() })
	go func() {
		buf := make([]byte, 1500)
		for {
			n, from, err := c.ReadFromUDP(buf)
			if err != nil {
				return
			}
			for _, r := range reply(append([]byte(nil), buf[:n]...)) {
				_, _ = c.WriteToUDP(r, from)
			}
		}
	}()
	return c.LocalAddr().(*net.UDPAddr).Port
}

func TestMDNSAndNetBIOSOverRealSockets(t *testing.T) {
	oldM, oldN, oldMW, oldNW := gwMDNSPort, gwNetBIOSPort, gwMDNSWait, gwNetBIOSWait
	t.Cleanup(func() { gwMDNSPort, gwNetBIOSPort, gwMDNSWait, gwNetBIOSWait = oldM, oldN, oldMW, oldNW })
	gwMDNSWait, gwNetBIOSWait = 400*time.Millisecond, 400*time.Millisecond

	gwMDNSPort = udpResponder(t, func(req []byte) [][]byte {
		name, _, _ := dnsReadName(req, 12)
		switch {
		case strings.HasSuffix(name, ".in-addr.arpa"):
			return [][]byte{dnsAnswer(1, dnsRecord(name, "Annas-MacBook.local"))}
		case name == "_services._dns-sd._udp.local":
			return [][]byte{dnsAnswer(2, dnsRecord(name, "_airplay._tcp.local"), dnsRecord(name, "not a service"), dnsRecord(name, "_companion-link._tcp.local"))}
		}
		return nil
	})
	name, svc := gwMDNS(context.Background(), netip.MustParseAddr("127.0.0.1"))
	if name != "Annas-MacBook" || !slices.Equal(svc, []string{"_airplay._tcp", "_companion-link._tcp"}) {
		t.Errorf("mDNS: %q %v", name, svc)
	}

	gwNetBIOSPort = udpResponder(t, func([]byte) [][]byte {
		return [][]byte{nbstatAnswer([3]any{"DESKTOP-7", byte(0x00), uint16(0x0400)})}
	})
	if got := gwNetBIOS(context.Background(), netip.MustParseAddr("127.0.0.1")); got != "DESKTOP-7" {
		t.Errorf("NetBIOS: %q", got)
	}

	// a host that answers nothing costs the wait and gives nothing
	gwMDNSPort = udpResponder(t, func([]byte) [][]byte { return nil })
	start := time.Now()
	if name, svc := gwMDNS(context.Background(), netip.MustParseAddr("127.0.0.1")); name != "" || len(svc) != 0 || time.Since(start) > 2*time.Second {
		t.Errorf("silence: %q %v after %v", name, svc, time.Since(start))
	}
}

const upnpXML = `<?xml version="1.0"?>
<root xmlns="urn:schemas-upnp-org:device-1-0"><specVersion><major>1</major><minor>0</minor></specVersion>
<device><deviceType>urn:schemas-upnp-org:device:MediaRenderer:1</deviceType><friendlyName>[TV] Living room</friendlyName>
<manufacturer>Samsung Electronics</manufacturer><modelName>UE55AU7100</modelName></device></root>`

func TestUPnP(t *testing.T) {
	if got := parseSSDPLocation([]byte("HTTP/1.1 200 OK\r\nCACHE-CONTROL: max-age=1800\r\nLocation: http://192.168.1.110:9197/dmr\r\nST: upnp:rootdevice\r\n\r\n")); got != "http://192.168.1.110:9197/dmr" {
		t.Errorf("location: %q", got)
	}
	info, ok := parseUPnPDescription(strings.NewReader(upnpXML))
	if !ok || info.Name != "[TV] Living room" || info.UPnP.Type != "MediaRenderer" || info.UPnP.Model != "UE55AU7100" || info.UPnP.Maker != "Samsung Electronics" {
		t.Errorf("description: %+v %v", info, ok)
	}
	if _, ok := parseUPnPDescription(strings.NewReader("<html>not upnp</html>")); ok {
		t.Errorf("not a description")
	}
	if _, ok := parseUPnPDescription(strings.NewReader("<root><device>")); ok {
		t.Errorf("a cut document")
	}

	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		switch r.URL.Path {
		case "/desc.xml":
			_, _ = w.Write([]byte(upnpXML))
		case "/redirect":
			http.Redirect(w, r, "http://127.0.0.9/elsewhere", http.StatusFound)
		case "/missing":
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	ip := netip.MustParseAddr("127.0.0.1")
	ctx := context.Background()
	if info, ok := fetchUPnP(ctx, ip, srv.URL+"/desc.xml"); !ok || info.UPnP.Model != "UE55AU7100" {
		t.Errorf("the description of the device itself is read: %+v %v", info, ok)
	}
	before := hits
	for name, loc := range map[string]string{
		"another host":   strings.Replace(srv.URL, "127.0.0.1", "127.0.0.9", 1) + "/desc.xml",
		"https":          strings.Replace(srv.URL, "http://", "https://", 1) + "/desc.xml",
		"a redirect":     srv.URL + "/redirect",
		"not found":      srv.URL + "/missing",
		"a bad port":     "http://127.0.0.1:99999/desc.xml",
		"a file":         "file:///etc/passwd",
		"a name":         "http://localhost:80/desc.xml",
		"a long address": srv.URL + "/" + strings.Repeat("a", 600),
	} {
		if _, ok := fetchUPnP(ctx, ip, loc); ok {
			t.Errorf("%s must not be read", name)
		}
	}
	if hits-before > 2 { // only the redirect and the missing page reach the device itself
		t.Errorf("the agent called a host it was not told about: %d requests", hits-before)
	}
}
