package agent

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/bolgov0zero/linager/internal/pki"
	"github.com/bolgov0zero/linager/internal/protocol"
)

// EnrollOptions drive the pairing flow.
type EnrollOptions struct {
	Server       string // host:port of the server's agent port
	Fingerprint  string // expected server certificate fingerprint; empty = ask Confirm
	DataDir      string
	AgentVersion string
	// Confirm is called with the server fingerprint when none was supplied. Returning false aborts.
	Confirm func(fingerprint string) bool
	// Show is called once the request is registered; it must display the key and the check code.
	Show func(key, code, serverFP string)
	// PollEvery overrides the polling interval (tests).
	PollEvery time.Duration
}

// pinnedTLS returns a client TLS config that accepts only the certificate with fingerprint fp.
func pinnedTLS(fp string, cert *tls.Certificate) *tls.Config {
	cfg := &tls.Config{
		MinVersion:         tls.VersionTLS13,
		InsecureSkipVerify: true, //nolint:gosec // the server certificate is pinned by fingerprint below
		// VerifyConnection also runs on resumed sessions, unlike VerifyPeerCertificate.
		VerifyConnection: func(cs tls.ConnectionState) error {
			if len(cs.PeerCertificates) == 0 || pki.Fingerprint(cs.PeerCertificates[0].Raw) != fp {
				return errors.New("server certificate does not match the pinned fingerprint")
			}
			return nil
		},
	}
	if cert != nil {
		cfg.Certificates = []tls.Certificate{*cert}
	}
	return cfg
}

// probe connects once to learn the server certificate and local address.
func probe(ctx context.Context, addr string) (der []byte, local string, err error) {
	d := tls.Dialer{NetDialer: &net.Dialer{Timeout: 10 * time.Second}, Config: &tls.Config{MinVersion: tls.VersionTLS13, InsecureSkipVerify: true}} //nolint:gosec // only reads the certificate for fingerprint confirmation
	c, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, "", err
	}
	defer func() { _ = c.Close() }()
	tc := c.(*tls.Conn)
	st := tc.ConnectionState()
	if len(st.PeerCertificates) == 0 {
		return nil, "", errors.New("server presented no certificate")
	}
	return st.PeerCertificates[0].Raw, localIP(c), nil
}

// ProbeFingerprint connects to the server's agent port and returns the fingerprint of its certificate.
func ProbeFingerprint(ctx context.Context, addr string) (string, error) {
	der, _, err := probe(ctx, addr)
	if err != nil {
		return "", fmt.Errorf("cannot reach the server: %w", err)
	}
	return pki.Fingerprint(der), nil
}

// Enroll pairs this host with the server and stores the resulting identity.
func Enroll(ctx context.Context, o EnrollOptions) error {
	if o.PollEvery == 0 {
		o.PollEvery = 2 * time.Second
	}
	key, err := loadOrCreateKey(o.DataDir)
	if err != nil {
		return err
	}
	pub := key.Public().(ed25519.PublicKey)

	der, local, err := probe(ctx, o.Server)
	if err != nil {
		return fmt.Errorf("cannot reach the server: %w", err)
	}
	fp := pki.Fingerprint(der)
	switch {
	case o.Fingerprint != "" && o.Fingerprint != fp:
		return fmt.Errorf("server fingerprint mismatch: got %s, expected %s", fp, o.Fingerprint)
	case o.Fingerprint == "" && (o.Confirm == nil || !o.Confirm(fp)):
		return errors.New("server fingerprint was not confirmed")
	}

	hc := &http.Client{Timeout: 15 * time.Second, Transport: &http.Transport{TLSClientConfig: pinnedTLS(fp, nil)}}
	base := "https://" + o.Server

	host := hostname()
	ts := time.Now().Unix()
	body, _ := json.Marshal(protocol.EnrollRequest{
		Pubkey: pub, Hostname: host, Address: local, OS: osName(), AgentVersion: o.AgentVersion,
		Protocol: protocol.Version, TS: ts, Sig: ed25519.Sign(key, protocol.EnrollSigningBytes(pub, host, ts)),
	})
	var er protocol.EnrollResponse
	if err := doJSON(ctx, hc, http.MethodPost, base+"/v1/enroll", body, &er); err != nil {
		return fmt.Errorf("enrollment request: %w", err)
	}
	o.Show(pki.EnrollmentKey(pub), pki.VerificationCode(der, pub, er.Nonce), fp)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(o.PollEvery):
		}
		ts := time.Now().Unix()
		q := url.Values{"ts": {strconv.FormatInt(ts, 10)}, "sig": {base64.RawURLEncoding.EncodeToString(ed25519.Sign(key, protocol.PollSigningBytes(er.ID, ts)))}}
		var st protocol.EnrollStatus
		if err := doJSON(ctx, hc, http.MethodGet, base+"/v1/enroll/"+er.ID+"?"+q.Encode(), nil, &st); err != nil {
			continue // transient; keep polling until the request expires
		}
		switch st.Status {
		case "approved":
			return saveState(o.DataDir, &State{Server: o.Server, ServerFP: fp, ServerID: st.ServerID, CmdPubkey: st.CmdPubkey}, []byte(st.CertPEM))
		case "rejected":
			return errors.New("the request was rejected")
		case "expired":
			return errors.New("the request expired; run enroll again")
		}
	}
}

func doJSON(ctx context.Context, hc *http.Client, method, u string, body []byte, out any) error {
	req, err := http.NewRequestWithContext(ctx, method, u, bytes.NewReader(body))
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := hc.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 200))
		return fmt.Errorf("%s: %s", resp.Status, bytes.TrimSpace(msg))
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(out)
}
