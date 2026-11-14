package agent

import (
	"crypto/ed25519"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/bolgov0zero/linager/internal/pki"
)

func leafOf(cert tls.Certificate) (*x509.Certificate, error) {
	if cert.Leaf != nil {
		return cert.Leaf, nil
	}
	if len(cert.Certificate) == 0 {
		return nil, errors.New("no certificate")
	}
	return x509.ParseCertificate(cert.Certificate[0])
}

// renewDue is the moment the agent starts asking for a new certificate: a third of the lifetime is left.
func renewDue(leaf *x509.Certificate) time.Time {
	return leaf.NotAfter.Add(-leaf.NotAfter.Sub(leaf.NotBefore) / 3)
}

// renewRetry is the pause between renewal requests while no new certificate has arrived (an older
// server ignores them): twelve hours, or less for short-lived certificates.
func renewRetry(leaf *x509.Certificate, limit time.Duration) time.Duration {
	return max(5*time.Second, min(limit, leaf.NotAfter.Sub(leaf.NotBefore)/6))
}

// installRenewedCert checks a certificate sent by the server and atomically replaces agent.crt.
// The certificate must belong to this agent's key and identity and must outlive the current one.
func installRenewedCert(dir string, key ed25519.PrivateKey, cur *x509.Certificate, certPEM []byte, now time.Time) (*x509.Certificate, error) {
	nc, err := pki.ParseCertPEM(certPEM)
	if err != nil {
		return nil, err
	}
	pub, ok := nc.PublicKey.(ed25519.PublicKey)
	if !ok || !pub.Equal(key.Public()) {
		return nil, errors.New("certificate is for a different key")
	}
	if nc.Subject.CommonName != cur.Subject.CommonName {
		return nil, errors.New("certificate is for a different agent")
	}
	if now.Before(nc.NotBefore.Add(-5*time.Minute)) || !nc.NotAfter.After(cur.NotAfter) {
		return nil, errors.New("certificate validity is not an extension of the current one")
	}
	p := filepath.Join(dir, certFile)
	tmp := p + ".new"
	_ = os.Remove(tmp)
	if err := os.WriteFile(tmp, certPEM, 0o600); err != nil {
		return nil, err
	}
	if err := os.Rename(tmp, p); err != nil {
		_ = os.Remove(tmp)
		return nil, err
	}
	return nc, nil
}
