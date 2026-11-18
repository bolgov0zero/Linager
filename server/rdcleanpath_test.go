package server

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/binary"
	"io"
	"math/big"
	"net"
	"testing"
	"time"
)

// chanConn is the browser side of the WebSocket: two channels of messages.
type chanConn struct{ in, out chan []byte }

func (c chanConn) Read(ctx context.Context) ([]byte, error) {
	select {
	case b, ok := <-c.in:
		if !ok {
			return nil, io.EOF
		}
		return b, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (c chanConn) Write(ctx context.Context, b []byte) error {
	select {
	case c.out <- b:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// x224CR is a connection request with RDP_NEG_REQ (TLS, CredSSP).
func x224CR() []byte {
	return []byte{3, 0, 0, 19, 14, 0xE0, 0, 0, 0, 0, 0, 1, 0, 8, 0, 3, 0, 0, 0}
}

// x224CC is the confirm of a server that selects proto.
func x224CC(proto uint32) []byte {
	b := []byte{3, 0, 0, 19, 14, 0xD0, 0, 0, 0x12, 0x34, 0, 2, 0, 8, 0, 0, 0, 0, 0}
	binary.LittleEndian.PutUint32(b[15:], proto)
	return b
}

func selfSigned(t *testing.T) tls.Certificate {
	t.Helper()
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tpl := &x509.Certificate{SerialNumber: big.NewInt(7), Subject: pkix.Name{CommonName: "rdp-test"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour)}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
}

// fakeRDP answers like a Remote Desktop service: the X.224 confirm, then (for TLS) a TLS server that echoes with a prefix.
func fakeRDP(t *testing.T, conn net.Conn, proto uint32, cert tls.Certificate) {
	defer func() { _ = conn.Close() }()
	if _, err := readTPKT(conn); err != nil {
		return
	}
	if _, err := conn.Write(x224CC(proto)); err != nil {
		return
	}
	if proto&(rdpProtoSSL|rdpProtoHybrid) == 0 {
		return
	}
	tc := tls.Server(conn, &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12})
	if tc.Handshake() != nil {
		return
	}
	b := make([]byte, 256)
	for {
		n, err := tc.Read(b)
		if err != nil {
			return
		}
		_, _ = tc.Write(append([]byte("pong:"), b[:n]...))
	}
}

func rdcpRequest(t *testing.T, version int64, x224 []byte) []byte {
	t.Helper()
	b, err := (&rdcpPDU{Version: version, Destination: "127.0.0.1:3389", ProxyAuth: "ticket", X224: x224}).marshal()
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestRDCleanPathHandshakeAndRelay(t *testing.T) {
	cert := selfSigned(t)
	clientSide, serverSide := net.Pipe()
	go fakeRDP(t, serverSide, rdpProtoHybrid, cert)
	c := chanConn{in: make(chan []byte, 4), out: make(chan []byte, 4)}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- serveRDCleanPath(ctx, c, clientSide, "127.0.0.1:3389") }()

	c.in <- rdcpRequest(t, rdCleanPathVersion, x224CR())
	resp, err := parseRDCleanPath(<-c.out)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Err.Code != 0 {
		t.Fatalf("an error answer: %+v", resp.Err)
	}
	if string(resp.X224) != string(x224CC(rdpProtoHybrid)) {
		t.Fatalf("the confirm must be handed back unchanged: %x", resp.X224)
	}
	if len(resp.Chain) != 1 || string(resp.Chain[0]) != string(cert.Certificate[0]) {
		t.Fatal("the certificate chain of the server must be handed back")
	}
	if resp.ServerAddr != "127.0.0.1:3389" {
		t.Fatalf("server address %q", resp.ServerAddr)
	}
	// after the handshake the bytes are carried both ways, decrypted on the browser side
	c.in <- []byte("hello")
	if got := string(<-c.out); got != "pong:hello" {
		t.Fatalf("relay: %q", got)
	}
	close(c.in)
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("a closed browser is not an error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the proxy must end when the browser leaves")
	}
}

func TestRDCleanPathRefusesWhatItCannotCarry(t *testing.T) {
	cases := map[string]struct {
		req   func(t *testing.T) []byte
		proto uint32
		code  int
	}{
		"standard RDP security without TLS": {func(t *testing.T) []byte { return rdcpRequest(t, rdCleanPathVersion, x224CR()) }, 0, rdcpNegotiationError},
		"a wrong version":                   {func(t *testing.T) []byte { return rdcpRequest(t, 1, x224CR()) }, rdpProtoSSL, rdcpGeneralError},
		"garbage":                           {func(*testing.T) []byte { return []byte("not asn1") }, rdpProtoSSL, rdcpGeneralError},
		"no connection request":             {func(t *testing.T) []byte { return rdcpRequest(t, rdCleanPathVersion, nil) }, rdpProtoSSL, rdcpGeneralError},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			clientSide, serverSide := net.Pipe()
			go fakeRDP(t, serverSide, tc.proto, selfSigned(t))
			c := chanConn{in: make(chan []byte, 2), out: make(chan []byte, 2)}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			done := make(chan error, 1)
			go func() { done <- serveRDCleanPath(ctx, c, clientSide, "127.0.0.1:3389") }()
			c.in <- tc.req(t)
			resp, err := parseRDCleanPath(<-c.out)
			if err != nil {
				t.Fatal(err)
			}
			if resp.Err.Code != tc.code {
				t.Fatalf("error code %d, want %d", resp.Err.Code, tc.code)
			}
			if err := <-done; err == nil {
				t.Fatal("a refused connection must end with an error")
			}
		})
	}
}

func TestRDCleanPathPDURoundTrip(t *testing.T) {
	in := &rdcpPDU{Version: rdCleanPathVersion, Destination: "host:3389", ProxyAuth: "t", X224: []byte{1, 2, 3}, Chain: [][]byte{{9}, {8, 7}}, ServerAddr: "10.0.0.1:3389"}
	b, err := in.marshal()
	if err != nil {
		t.Fatal(err)
	}
	out, err := parseRDCleanPath(b)
	if err != nil {
		t.Fatal(err)
	}
	if out.Destination != in.Destination || out.ProxyAuth != "t" || string(out.X224) != "\x01\x02\x03" || len(out.Chain) != 2 || out.ServerAddr != in.ServerAddr {
		t.Fatalf("%+v", out)
	}
	// the first bytes of a version 3390 PDU: a SEQUENCE whose first element is [0] EXPLICIT INTEGER 3390
	if b[0] != 0x30 || b[2] != 0xA0 {
		t.Fatalf("not the expected DER layout: %x", b[:8])
	}
}
