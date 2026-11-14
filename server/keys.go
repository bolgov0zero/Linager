package server

import (
	"crypto/ed25519"
	"crypto/tls"
	"errors"
	"os"
	"path/filepath"

	"github.com/bolgov0zero/linager/internal/pki"
)

// Keys is the server key material kept in the data directory.
type Keys struct {
	CA        *pki.CA
	TLS       tls.Certificate
	TLSDER    []byte
	CmdKey    ed25519.PrivateKey
	CmdPubkey ed25519.PublicKey
}

func writeSecret(path string, data []byte) error {
	return os.WriteFile(path, data, 0o600)
}

// InitKeys creates missing keys (CA, agent-port TLS certificate, command signing key).
// Existing files are never overwritten.
func InitKeys(dir string, hosts []string) error {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	if _, err := os.Stat(filepath.Join(dir, "ca.key")); errors.Is(err, os.ErrNotExist) {
		ca, err := pki.NewCA("Linager CA")
		if err != nil {
			return err
		}
		if err := writeSecret(filepath.Join(dir, "ca.crt"), ca.CertPEM); err != nil {
			return err
		}
		if err := writeSecret(filepath.Join(dir, "ca.key"), ca.KeyPEM()); err != nil {
			return err
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "agents.key")); errors.Is(err, os.ErrNotExist) {
		c, k, err := pki.NewServerCert(hosts)
		if err != nil {
			return err
		}
		if err := writeSecret(filepath.Join(dir, "agents.crt"), c); err != nil {
			return err
		}
		if err := writeSecret(filepath.Join(dir, "agents.key"), k); err != nil {
			return err
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "cmd.key")); errors.Is(err, os.ErrNotExist) {
		_, priv, err := pki.GenerateKey()
		if err != nil {
			return err
		}
		return writeSecret(filepath.Join(dir, "cmd.key"), pki.MarshalPrivateKey(priv))
	}
	return nil
}

// LoadKeys reads the key material created by InitKeys.
func LoadKeys(dir string) (*Keys, error) {
	read := func(n string) ([]byte, error) { return os.ReadFile(filepath.Join(dir, n)) } //nolint:gosec // fixed names in the data dir
	caCrt, err := read("ca.crt")
	if err != nil {
		return nil, err
	}
	caKey, err := read("ca.key")
	if err != nil {
		return nil, err
	}
	ca, err := pki.LoadCA(caCrt, caKey)
	if err != nil {
		return nil, err
	}
	crt, err := read("agents.crt")
	if err != nil {
		return nil, err
	}
	key, err := read("agents.key")
	if err != nil {
		return nil, err
	}
	pair, err := tls.X509KeyPair(crt, key)
	if err != nil {
		return nil, err
	}
	cmdPEM, err := read("cmd.key")
	if err != nil {
		return nil, err
	}
	cmd, err := pki.ParsePrivateKey(cmdPEM)
	if err != nil {
		return nil, err
	}
	return &Keys{CA: ca, TLS: pair, TLSDER: pair.Certificate[0], CmdKey: cmd, CmdPubkey: cmd.Public().(ed25519.PublicKey)}, nil
}
