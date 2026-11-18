package server

import (
	"context"
	"crypto/tls"
	"encoding/asn1"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"time"
)

// RDCleanPath is the protocol between the browser client of the remote desktop (IronRDP) and a proxy. A browser
// cannot open a TCP connection or upgrade it to TLS, so the proxy does the first steps for it: it sends the RDP
// connection request to the server, upgrades the connection to TLS, and hands the answer and the certificate chain of
// the server back. After that the proxy only carries bytes. The PDU is DER, all fields explicitly tagged.

// rdCleanPathVersion is 3389 + 1 (the first version of the protocol).
const rdCleanPathVersion = 3390

// Error codes of the protocol; the client treats an unknown one as a general error.
const (
	rdcpGeneralError     = 1
	rdcpNegotiationError = 2
)

type rdcpErr struct {
	Code int `asn1:"tag:0,explicit"`
	HTTP int `asn1:"tag:1,explicit,optional"`
	WSA  int `asn1:"tag:2,explicit,optional"`
	TLS  int `asn1:"tag:3,explicit,optional"`
}

type rdcpPDU struct {
	Version       int64    `asn1:"tag:0,explicit"`
	Err           rdcpErr  `asn1:"tag:1,explicit,optional"`
	Destination   string   `asn1:"tag:2,explicit,optional,utf8"`
	ProxyAuth     string   `asn1:"tag:3,explicit,optional,utf8"`
	ServerAuth    string   `asn1:"tag:4,explicit,optional,utf8"`
	Preconnection string   `asn1:"tag:5,explicit,optional,utf8"`
	X224          []byte   `asn1:"tag:6,explicit,optional"`
	Chain         [][]byte `asn1:"tag:7,explicit,optional"`
	ServerAddr    string   `asn1:"tag:9,explicit,optional,utf8"`
}

func parseRDCleanPath(b []byte) (*rdcpPDU, error) {
	var p rdcpPDU
	rest, err := asn1.Unmarshal(b, &p)
	if err != nil {
		return nil, fmt.Errorf("bad RDCleanPath request: %w", err)
	}
	if len(rest) != 0 {
		return nil, errors.New("bad RDCleanPath request: trailing data")
	}
	if p.Version != rdCleanPathVersion {
		return nil, fmt.Errorf("unsupported RDCleanPath version %d", p.Version)
	}
	return &p, nil
}

func (p *rdcpPDU) marshal() ([]byte, error) { return asn1.Marshal(*p) }

// msgConn carries whole messages to and from the browser (a WebSocket with binary messages).
type msgConn interface {
	Read(ctx context.Context) ([]byte, error)
	Write(ctx context.Context, b []byte) error
}

// RDP security protocols a server can select (MS-RDPBCGR 2.2.1.2.1).
const (
	rdpProtoSSL      = 0x1
	rdpProtoHybrid   = 0x2
	rdpProtoHybridEx = 0x8
)

// readTPKT reads one TPKT packet (the 4-byte header and the rest).
func readTPKT(r io.Reader) ([]byte, error) {
	h := make([]byte, 4)
	if _, err := io.ReadFull(r, h); err != nil {
		return nil, err
	}
	if h[0] != 3 {
		return nil, errors.New("the server did not answer in RDP")
	}
	n := int(binary.BigEndian.Uint16(h[2:4]))
	if n < 4 || n > 4096 {
		return nil, errors.New("bad RDP packet length")
	}
	b := make([]byte, n)
	copy(b, h)
	if _, err := io.ReadFull(r, b[4:]); err != nil {
		return nil, err
	}
	return b, nil
}

// x224Selected returns the security protocol the server selected in its X.224 connection confirm. A confirm
// without negotiation data means standard RDP security (0). failed is a negotiation failure answer.
func x224Selected(cc []byte) (proto uint32, failed bool) {
	if len(cc) < 7 || cc[5] != 0xD0 {
		return 0, true
	}
	if len(cc) >= 19 {
		switch cc[11] {
		case 0x02: // RDP_NEG_RSP
			return binary.LittleEndian.Uint32(cc[15:19]), false
		case 0x03: // RDP_NEG_FAILURE
			return 0, true
		}
	}
	return 0, false
}

func (p *rdcpPDU) errorReply(code int) []byte {
	b, _ := (&rdcpPDU{Version: rdCleanPathVersion, Err: rdcpErr{Code: code}}).marshal()
	return b
}

// serveRDCleanPath runs the proxy side: the handshake, then the carrying of bytes, until either side ends. up is the
// connection to the RDP server (here: through the agent), upAddr how the client is told its address.
func serveRDCleanPath(ctx context.Context, c msgConn, up net.Conn, upAddr string) error {
	first, err := c.Read(ctx)
	if err != nil {
		return err
	}
	fail := func(code int, e error) error {
		_ = c.Write(ctx, (&rdcpPDU{}).errorReply(code))
		return e
	}
	req, err := parseRDCleanPath(first)
	if err != nil {
		return fail(rdcpGeneralError, err)
	}
	if len(req.X224) < 4 || len(req.X224) > 4096 {
		return fail(rdcpGeneralError, errors.New("the RDCleanPath request has no connection request"))
	}
	if _, err := up.Write(req.X224); err != nil {
		return fail(rdcpGeneralError, err)
	}
	cc, err := readTPKT(up)
	if err != nil {
		return fail(rdcpGeneralError, err)
	}
	proto, failed := x224Selected(cc)
	if failed || proto&(rdpProtoSSL|rdpProtoHybrid|rdpProtoHybridEx) == 0 {
		// the browser client needs TLS: standard RDP security is not carried
		return fail(rdcpNegotiationError, errors.New("the server does not offer TLS (enable TLS or network level authentication)"))
	}
	tc := tls.Client(up, &tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS12}) //nolint:gosec // RDP servers present self-signed certificates; the client is shown the chain and the person signs in over it
	hctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	err = tc.HandshakeContext(hctx)
	cancel()
	if err != nil {
		return fail(rdcpGeneralError, fmt.Errorf("TLS handshake with the remote desktop failed: %w", err))
	}
	var chain [][]byte
	for _, cert := range tc.ConnectionState().PeerCertificates {
		chain = append(chain, cert.Raw)
	}
	resp, err := (&rdcpPDU{Version: rdCleanPathVersion, X224: cc, Chain: chain, ServerAddr: upAddr}).marshal()
	if err != nil {
		return err
	}
	if err := c.Write(ctx, resp); err != nil {
		return err
	}

	ctx, stop := context.WithCancel(ctx)
	defer stop()
	errc := make(chan error, 2)
	go func() { // browser -> server
		for {
			b, err := c.Read(ctx)
			if err != nil {
				errc <- err
				return
			}
			if _, err := tc.Write(b); err != nil {
				errc <- err
				return
			}
		}
	}()
	go func() { // server -> browser
		buf := make([]byte, 32<<10)
		for {
			n, err := tc.Read(buf)
			if n > 0 {
				if werr := c.Write(ctx, append([]byte(nil), buf[:n]...)); werr != nil {
					errc <- werr
					return
				}
			}
			if err != nil {
				errc <- err
				return
			}
		}
	}()
	err = <-errc
	_ = tc.Close()
	if errors.Is(err, io.EOF) || errors.Is(err, context.Canceled) {
		return nil
	}
	return err
}
