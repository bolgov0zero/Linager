package pki

import (
	"crypto/tls"
	"strings"
	"testing"
	"time"
)

func TestIssueAndVerifyAgentCert(t *testing.T) {
	ca, err := NewCA("test-ca")
	if err != nil {
		t.Fatal(err)
	}
	pub, _, _ := GenerateKey()
	certPEM, err := ca.IssueAgentCert(pub, "srv-1", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	cert, _ := ParseCertPEM(certPEM)
	id, err := ca.VerifyAgentCert(cert)
	if err != nil || id != "srv-1" {
		t.Fatalf("id=%q err=%v", id, err)
	}
	other, _ := NewCA("other")
	if _, err := other.VerifyAgentCert(cert); err == nil {
		t.Fatal("certificate from a foreign CA must not verify")
	}
}

func TestCARoundTrip(t *testing.T) {
	ca, _ := NewCA("x")
	ca2, err := LoadCA(ca.CertPEM, ca.KeyPEM())
	if err != nil {
		t.Fatal(err)
	}
	pub, _, _ := GenerateKey()
	if _, err := ca2.IssueAgentCert(pub, "a", time.Hour); err != nil {
		t.Fatal(err)
	}
}

func TestServerCertUsableForTLS(t *testing.T) {
	c, k, err := NewServerCert([]string{"127.0.0.1", "linager.local"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tls.X509KeyPair(c, k); err != nil {
		t.Fatal(err)
	}
}

func TestEnrollmentKeyFormat(t *testing.T) {
	pub, _, _ := GenerateKey()
	k := EnrollmentKey(pub)
	if len(k) != 9 || k[4] != '-' {
		t.Fatalf("bad key %q", k)
	}
	if NormalizeKey(strings.ToLower(k)) != k {
		t.Fatal("normalization must accept lower case")
	}
	if NormalizeKey("abc") != "" {
		t.Fatal("short keys must be rejected")
	}
}

func TestVerificationCodeBindsCertificate(t *testing.T) {
	pub, _, _ := GenerateKey()
	nonce := []byte("0123456789abcdef")
	a := VerificationCode([]byte("cert-A"), pub, nonce)
	b := VerificationCode([]byte("cert-B"), pub, nonce)
	if a == b {
		t.Fatal("different server certificates must give different codes")
	}
	if a != VerificationCode([]byte("cert-A"), pub, nonce) {
		t.Fatal("code must be deterministic")
	}
}
