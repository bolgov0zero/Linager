package server

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/tls"
	"testing"

	"github.com/bolgov0zero/linager/internal/pki"
)

// A client that cannot verify Ed25519 (Windows Schannel) must be given the ECDSA certificate, one that can gets the Ed25519 one.
func TestAgentPortOffersEcdsaToClientsWithoutEd25519(t *testing.T) {
	dir := t.TempDir()
	if err := InitKeys(dir, []string{"127.0.0.1", "panel.example.com"}); err != nil {
		t.Fatal(err)
	}
	keys, err := LoadKeys(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := keys.TLS.PrivateKey.(ed25519.PrivateKey); !ok {
		t.Fatal("the main certificate is Ed25519")
	}
	if _, ok := keys.TLSAlt.PrivateKey.(*ecdsa.PrivateKey); !ok {
		t.Fatal("the second certificate is ECDSA")
	}
	if pki.Fingerprint(keys.TLSDER) == pki.Fingerprint(keys.TLSAltDER) {
		t.Fatal("two certificates, two fingerprints")
	}
	cfg := (&AgentPort{Keys: keys}).TLSConfig()
	pick := func(schemes []tls.SignatureScheme, versions []uint16) *tls.Certificate {
		hi := &tls.ClientHelloInfo{SignatureSchemes: schemes, SupportedVersions: versions, SupportedCurves: []tls.CurveID{tls.CurveP256, tls.X25519}, SupportedPoints: []uint8{0}, CipherSuites: []uint16{tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256, tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256, tls.TLS_AES_128_GCM_SHA256}}
		c := cfg
		if g, err := cfg.GetConfigForClient(hi); err == nil && g != nil {
			c = g
		}
		for i := range c.Certificates {
			if hi.SupportsCertificate(&c.Certificates[i]) == nil {
				return &c.Certificates[i]
			}
		}
		return nil
	}
	schannel := []tls.SignatureScheme{tls.ECDSAWithP256AndSHA256, tls.ECDSAWithP384AndSHA384, tls.PSSWithSHA256, tls.PKCS1WithSHA256}
	if c := pick(schannel, []uint16{tls.VersionTLS12}); c == nil || string(c.Certificate[0]) != string(keys.TLSAltDER) {
		t.Fatal("a client without Ed25519 must get the ECDSA certificate")
	}
	modern := append([]tls.SignatureScheme{tls.Ed25519}, schannel...)
	if c := pick(modern, []uint16{tls.VersionTLS13, tls.VersionTLS12}); c == nil || string(c.Certificate[0]) != string(keys.TLSDER) {
		t.Fatal("an agent (Ed25519) must keep getting the Ed25519 certificate")
	}
	// a server of an earlier version has no ECDSA certificate yet: it is made on load, for the same hosts
	loaded, err := LoadKeys(dir)
	if err != nil || loaded.TLSAltDER == nil {
		t.Fatalf("reload: %v", err)
	}
}
