// Package release verifies minisign-compatible signatures and describes Linager releases.
package release

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/crypto/blake2b"
)

// PublicKey is the release signing key (minisign public key line). It can be overridden at build time
// with -ldflags "-X github.com/bolgov0zero/linager/internal/release.PublicKey=RWQ...".
var PublicKey = "RWQlhVj7fNoehSMCgQigmYrwQMjs/DyxnH1zLVEXkNXINxSTC0HDdCOU"

// Key is a parsed minisign public key.
type Key struct {
	ID  [8]byte
	Pub ed25519.PublicKey
}

// ParsePublicKey accepts the bare base64 line or a whole .pub file.
func ParsePublicKey(s string) (*Key, error) {
	line := ""
	for _, l := range strings.Split(strings.TrimSpace(s), "\n") {
		l = strings.TrimSpace(l)
		if l != "" && !strings.HasPrefix(l, "untrusted comment:") {
			line = l
			break
		}
	}
	b, err := base64.StdEncoding.DecodeString(line)
	if err != nil || len(b) != 42 || b[0] != 'E' || b[1] != 'd' {
		return nil, errors.New("invalid minisign public key")
	}
	k := &Key{Pub: ed25519.PublicKey(append([]byte(nil), b[10:]...))}
	copy(k.ID[:], b[2:10])
	return k, nil
}

// Embedded returns the key compiled into the binary.
func Embedded() (*Key, error) { return ParsePublicKey(PublicKey) }

// Verify checks a minisign signature file (.minisig) over message, including the trusted comment.
func (k *Key) Verify(message []byte, sigFile string) error {
	lines := strings.Split(strings.ReplaceAll(sigFile, "\r\n", "\n"), "\n")
	if len(lines) < 4 || !strings.HasPrefix(lines[0], "untrusted comment:") || !strings.HasPrefix(lines[2], "trusted comment:") {
		return errors.New("malformed signature file")
	}
	sig, err := base64.StdEncoding.DecodeString(strings.TrimSpace(lines[1]))
	if err != nil || len(sig) != 74 {
		return errors.New("malformed signature")
	}
	if !bytes.Equal(sig[2:10], k.ID[:]) {
		return errors.New("signature was made with a different key")
	}
	body := message
	switch string(sig[:2]) {
	case "Ed":
	case "ED":
		h := blake2b.Sum512(message)
		body = h[:]
	default:
		return errors.New("unsupported signature algorithm")
	}
	if !ed25519.Verify(k.Pub, body, sig[10:]) {
		return errors.New("signature verification failed")
	}
	trusted := strings.TrimPrefix(lines[2], "trusted comment:")
	trusted = strings.TrimPrefix(trusted, " ")
	global, err := base64.StdEncoding.DecodeString(strings.TrimSpace(lines[3]))
	if err != nil || len(global) != ed25519.SignatureSize {
		return errors.New("malformed trusted-comment signature")
	}
	if !ed25519.Verify(k.Pub, append(append([]byte(nil), sig[10:]...), trusted...), global) {
		return errors.New("trusted comment verification failed")
	}
	return nil
}

// ---- versions ----

// Version is a parsed semantic version.
type Version struct {
	Major, Minor, Patch int
	Pre                 string
	Valid               bool
}

// ParseVersion parses "v1.2.3" or "1.2.3-beta.1". Anything else (for example "dev") is invalid.
func ParseVersion(s string) Version {
	s = strings.TrimPrefix(strings.TrimSpace(s), "v")
	core, pre, _ := strings.Cut(s, "-")
	core, _, _ = strings.Cut(core, "+")
	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return Version{}
	}
	var n [3]int
	for i, p := range parts {
		v, err := strconv.Atoi(p)
		if err != nil || v < 0 {
			return Version{}
		}
		n[i] = v
	}
	return Version{n[0], n[1], n[2], pre, true}
}

// Compare returns -1, 0 or 1. Invalid versions sort below every valid one; a pre-release is below its release.
func Compare(a, b Version) int {
	switch {
	case !a.Valid && !b.Valid:
		return 0
	case !a.Valid:
		return -1
	case !b.Valid:
		return 1
	}
	for _, p := range [][2]int{{a.Major, b.Major}, {a.Minor, b.Minor}, {a.Patch, b.Patch}} {
		if p[0] != p[1] {
			if p[0] < p[1] {
				return -1
			}
			return 1
		}
	}
	switch {
	case a.Pre == b.Pre:
		return 0
	case a.Pre == "":
		return 1
	case b.Pre == "":
		return -1
	case a.Pre < b.Pre:
		return -1
	}
	return 1
}

// Newer reports whether candidate is a higher version than current.
func Newer(candidate, current string) bool {
	return Compare(ParseVersion(candidate), ParseVersion(current)) > 0
}

// String renders v.
func (v Version) String() string {
	if !v.Valid {
		return "unknown"
	}
	s := fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch)
	if v.Pre != "" {
		s += "-" + v.Pre
	}
	return s
}

// ---- manifest ----

// Manifest describes one release; it is signed as manifest.json (+ .minisig).
type Manifest struct {
	Version    string   `json:"version"`
	Channel    string   `json:"channel"` // stable | beta
	MinAgent   string   `json:"min_agent"`
	SHA256     string   `json:"sha256"` // of linager_linux_amd64
	Size       int64    `json:"size"`
	Migrations int      `json:"migrations"` // number of DB migrations the release ships
	Notes      []string `json:"notes"`
	BuiltAt    string   `json:"built_at"`
}

// BinaryAsset is the release asset name of the binary.
const BinaryAsset = "linager_linux_amd64"
