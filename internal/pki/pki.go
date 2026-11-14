// Package pki implements the Linager internal CA, certificates, fingerprints
// and the human-checkable codes used during agent enrollment.
package pki

import (
	"crypto/ed25519"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base32"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"strings"
	"time"
)

const agentCNPrefix = "agent:"

// CA is the internal certificate authority that issues agent certificates.
type CA struct {
	Cert    *x509.Certificate
	CertPEM []byte
	key     ed25519.PrivateKey
}

// GenerateKey creates an Ed25519 key pair.
func GenerateKey() (ed25519.PublicKey, ed25519.PrivateKey, error) {
	return ed25519.GenerateKey(rand.Reader)
}

func serial() (*big.Int, error) {
	return rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 120))
}

// NewCA creates a new self-signed CA valid for ten years.
func NewCA(name string) (*CA, error) {
	pub, priv, err := GenerateKey()
	if err != nil {
		return nil, err
	}
	sn, err := serial()
	if err != nil {
		return nil, err
	}
	tpl := &x509.Certificate{
		SerialNumber:          sn,
		Subject:               pkix.Name{CommonName: name},
		NotBefore:             time.Now().Add(-time.Minute),
		NotAfter:              time.Now().AddDate(10, 0, 0),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, pub, priv)
	if err != nil {
		return nil, err
	}
	return LoadCA(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), MarshalPrivateKey(priv))
}

// LoadCA restores a CA from PEM-encoded certificate and PKCS#8 key.
func LoadCA(certPEM, keyPEM []byte) (*CA, error) {
	cert, err := ParseCertPEM(certPEM)
	if err != nil {
		return nil, err
	}
	key, err := ParsePrivateKey(keyPEM)
	if err != nil {
		return nil, err
	}
	return &CA{Cert: cert, CertPEM: certPEM, key: key}, nil
}

// KeyPEM returns the CA private key as PKCS#8 PEM.
func (c *CA) KeyPEM() []byte { return MarshalPrivateKey(c.key) }

// IssueAgentCert signs the agent public key; the subject encodes the server id.
func (c *CA) IssueAgentCert(pub ed25519.PublicKey, serverID string, validity time.Duration) ([]byte, error) {
	sn, err := serial()
	if err != nil {
		return nil, err
	}
	tpl := &x509.Certificate{
		SerialNumber: sn,
		Subject:      pkix.Name{CommonName: agentCNPrefix + serverID},
		NotBefore:    time.Now().Add(-time.Minute),
		NotAfter:     time.Now().Add(validity),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, c.Cert, pub, c.key)
	if err != nil {
		return nil, err
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), nil
}

// VerifyAgentCert checks a presented client certificate chain and returns the server id.
func (c *CA) VerifyAgentCert(cert *x509.Certificate) (string, error) {
	pool := x509.NewCertPool()
	pool.AddCert(c.Cert)
	if _, err := cert.Verify(x509.VerifyOptions{
		Roots:     pool,
		KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}); err != nil {
		return "", err
	}
	id, ok := strings.CutPrefix(cert.Subject.CommonName, agentCNPrefix)
	if !ok || id == "" {
		return "", errors.New("certificate is not an agent certificate")
	}
	return id, nil
}

// NewServerCert creates a self-signed TLS certificate for the agent listener.
func NewServerCert(hosts []string) (certPEM, keyPEM []byte, err error) {
	pub, priv, err := GenerateKey()
	if err != nil {
		return nil, nil, err
	}
	sn, err := serial()
	if err != nil {
		return nil, nil, err
	}
	tpl := &x509.Certificate{
		SerialNumber: sn,
		Subject:      pkix.Name{CommonName: "linager-server"},
		NotBefore:    time.Now().Add(-time.Minute),
		NotAfter:     time.Now().AddDate(10, 0, 0),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	for _, h := range hosts {
		if ip := net.ParseIP(h); ip != nil {
			tpl.IPAddresses = append(tpl.IPAddresses, ip)
		} else {
			tpl.DNSNames = append(tpl.DNSNames, h)
		}
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, pub, priv)
	if err != nil {
		return nil, nil, err
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), MarshalPrivateKey(priv), nil
}

// MarshalPrivateKey encodes an Ed25519 key as PKCS#8 PEM.
func MarshalPrivateKey(k ed25519.PrivateKey) []byte {
	der, _ := x509.MarshalPKCS8PrivateKey(k) // cannot fail for Ed25519
	return pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
}

// ParsePrivateKey decodes a PKCS#8 PEM Ed25519 key.
func ParsePrivateKey(p []byte) (ed25519.PrivateKey, error) {
	blk, _ := pem.Decode(p)
	if blk == nil {
		return nil, errors.New("no PEM data")
	}
	k, err := x509.ParsePKCS8PrivateKey(blk.Bytes)
	if err != nil {
		return nil, err
	}
	ek, ok := k.(ed25519.PrivateKey)
	if !ok {
		return nil, errors.New("not an Ed25519 key")
	}
	return ek, nil
}

// ParseCertPEM decodes the first certificate in PEM data.
func ParseCertPEM(p []byte) (*x509.Certificate, error) {
	blk, _ := pem.Decode(p)
	if blk == nil {
		return nil, errors.New("no PEM data")
	}
	return x509.ParseCertificate(blk.Bytes)
}

// Fingerprint returns "SHA256:<base64>" of the DER bytes (certificates).
func Fingerprint(der []byte) string {
	h := sha256.Sum256(der)
	return "SHA256:" + strings.TrimRight(base64.StdEncoding.EncodeToString(h[:]), "=")
}

// KeyFingerprint returns the fingerprint of a raw public key.
func KeyFingerprint(pub ed25519.PublicKey) string { return Fingerprint(pub) }

// ShortFingerprint abbreviates a fingerprint for display: SHA256:Qm4x…r9Lw.
func ShortFingerprint(fp string) string {
	if len(fp) <= 18 {
		return fp
	}
	return fp[:11] + "…" + fp[len(fp)-4:]
}

var b32 = base32.NewEncoding("0123456789ABCDEFGHJKMNPQRSTVWXYZ").WithPadding(base32.NoPadding)

// EnrollmentKey derives the XXXX-XXXX key an administrator types to approve an agent.
func EnrollmentKey(pub ed25519.PublicKey) string {
	h := sha256.Sum256(pub)
	s := b32.EncodeToString(h[:5]) // 40 bits -> 8 characters
	return s[:4] + "-" + s[4:]
}

// NormalizeKey upper-cases a typed key and maps look-alike characters.
func NormalizeKey(s string) string {
	s = strings.ToUpper(strings.NewReplacer("-", "", " ", "").Replace(s))
	s = strings.NewReplacer("O", "0", "I", "1", "L", "1", "U", "V").Replace(s)
	if len(s) != 8 {
		return ""
	}
	return s[:4] + "-" + s[4:]
}

// VerificationCode is the short code shown on both sides to detect a swapped server.
// It binds the server certificate seen by the agent, the agent key and a server nonce.
func VerificationCode(serverCertDER []byte, agentPub ed25519.PublicKey, nonce []byte) string {
	cs := sha256.Sum256(serverCertDER)
	m := hmac.New(sha256.New, nonce)
	m.Write(cs[:])
	m.Write(agentPub)
	s := b32.EncodeToString(m.Sum(nil)[:4])
	return fmt.Sprintf("%s-%s", s[:3], s[3:6])
}
