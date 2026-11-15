package server

import (
	"crypto/rand"
	"crypto/sha256"
	"strings"
)

// Recovery codes: ten one-time codes that replace a TOTP code when the phone is lost. They are shown
// once; only the SHA-256 of the normalized code is stored (the codes are random with 50 bits of entropy,
// so a fast hash is enough).
const (
	recoveryCount = 10
	recoveryLen   = 10
	// no 0/O/1/I/L: the codes are read from paper
	recoveryAlphabet = "ABCDEFGHJKMNPQRSTUVWXYZ23456789"
)

// NewRecoveryCodes returns the codes for display ("ABCDE-FGHJK") and the hashes to store.
func NewRecoveryCodes() (codes []string, hashes [][]byte) {
	for i := 0; i < recoveryCount; i++ {
		b := make([]byte, recoveryLen)
		_, _ = rand.Read(b)
		var sb strings.Builder
		for j, c := range b {
			if j == recoveryLen/2 {
				sb.WriteByte('-')
			}
			sb.WriteByte(recoveryAlphabet[int(c)%len(recoveryAlphabet)])
		}
		codes = append(codes, sb.String())
		hashes = append(hashes, HashRecoveryCode(sb.String()))
	}
	return codes, hashes
}

// normalizeRecoveryCode upper-cases the code and drops dashes and spaces.
func normalizeRecoveryCode(s string) string {
	var sb strings.Builder
	for _, r := range strings.ToUpper(s) {
		if r == '-' || r == ' ' {
			continue
		}
		sb.WriteRune(r)
	}
	return sb.String()
}

// HashRecoveryCode hashes a code the way it is stored.
func HashRecoveryCode(code string) []byte {
	h := sha256.Sum256([]byte(normalizeRecoveryCode(code)))
	return h[:]
}

// isTOTPCode reports whether the input looks like a 6-digit TOTP code (anything else is tried as a recovery code).
func isTOTPCode(s string) bool {
	s = strings.TrimSpace(s)
	if len(s) != 6 {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
