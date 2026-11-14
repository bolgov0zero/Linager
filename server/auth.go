package server

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
	"golang.org/x/crypto/argon2"
)

// argon2id parameters (OWASP-recommended minimum profile).
const (
	argonTime    = 3
	argonMemory  = 64 * 1024
	argonThreads = 2
	argonKeyLen  = 32
	minPassword  = 12
)

// HashPassword returns an encoded argon2id hash.
func HashPassword(pw string) (string, error) {
	if len(pw) < minPassword {
		return "", fmt.Errorf("password must be at least %d characters", minPassword)
	}
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := argon2.IDKey([]byte(pw), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	return fmt.Sprintf("$argon2id$v=19$m=%d,t=%d,p=%d$%s$%s", argonMemory, argonTime, argonThreads,
		base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(key)), nil
}

// VerifyPassword checks a password against an encoded hash in constant time.
func VerifyPassword(pw, encoded string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false
	}
	var m, t, p uint64
	for _, kv := range strings.Split(parts[3], ",") {
		k, v, ok := strings.Cut(kv, "=")
		if !ok {
			return false
		}
		n, err := strconv.ParseUint(v, 10, 32)
		if err != nil {
			return false
		}
		switch k {
		case "m":
			m = n
		case "t":
			t = n
		case "p":
			p = n
		}
	}
	salt, err1 := base64.RawStdEncoding.DecodeString(parts[4])
	want, err2 := base64.RawStdEncoding.DecodeString(parts[5])
	if err1 != nil || err2 != nil || m == 0 || t == 0 || p == 0 || m > 1<<20 {
		return false
	}
	got := argon2.IDKey([]byte(pw), salt, uint32(t), uint32(m), uint8(p), uint32(len(want))) //nolint:gosec // bounded above
	return subtle.ConstantTimeCompare(got, want) == 1
}

// dummyHash is verified when the login does not exist, to equalize timing.
var dummyHash, _ = HashPassword("dummy-password-for-timing")

// NewToken returns a random session token and the hash stored in the database.
func NewToken() (token string, hash []byte) {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	token = base64.RawURLEncoding.EncodeToString(b)
	return token, HashToken(token)
}

// HashToken hashes a session token for lookup.
func HashToken(t string) []byte {
	h := sha256.Sum256([]byte(t))
	return h[:]
}

// NewCSRF returns a random CSRF token.
func NewCSRF() string {
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

// NewTOTPSecret generates a TOTP key for the given account.
func NewTOTPSecret(account string) (secret, uri string, err error) {
	k, err := totp.Generate(totp.GenerateOpts{Issuer: "Linager", AccountName: account})
	if err != nil {
		return "", "", err
	}
	return k.Secret(), k.URL(), nil
}

// MatchTOTP returns the time step the code matches (checking ±1 step), or an error.
func MatchTOTP(secret, code string, now time.Time) (int64, error) {
	code = strings.TrimSpace(code)
	for _, d := range []int64{0, -1, 1} {
		step := now.Unix()/30 + d
		want, err := totp.GenerateCodeCustom(secret, time.Unix(step*30, 0), totp.ValidateOpts{Period: 30, Digits: otp.DigitsSix, Algorithm: otp.AlgorithmSHA1})
		if err != nil {
			return 0, err
		}
		if subtle.ConstantTimeCompare([]byte(want), []byte(code)) == 1 {
			return step, nil
		}
	}
	return 0, errors.New("invalid code")
}

// ipLimiter is a small in-memory sliding-window limiter keyed by string.
type ipLimiter struct {
	mu     sync.Mutex
	max    int
	window time.Duration
	hits   map[string][]time.Time
}

func newLimiter(max int, window time.Duration) *ipLimiter {
	return &ipLimiter{max: max, window: window, hits: map[string][]time.Time{}}
}

// Allow records a hit and reports whether the key is still within its budget.
func (l *ipLimiter) Allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	kept := l.hits[key][:0]
	for _, t := range l.hits[key] {
		if now.Sub(t) < l.window {
			kept = append(kept, t)
		}
	}
	if len(kept) >= l.max {
		l.hits[key] = kept
		return false
	}
	l.hits[key] = append(kept, now)
	if len(l.hits) > 10000 { // opportunistic cleanup
		for k, v := range l.hits {
			if len(v) == 0 || now.Sub(v[len(v)-1]) > l.window {
				delete(l.hits, k)
			}
		}
	}
	return true
}

// lockDuration is the lock-out after n consecutive failures: none below 5, then doubling up to 15 minutes.
func lockDuration(n int) time.Duration {
	if n < 5 {
		return 0
	}
	d := time.Minute << (n - 5)
	if d > 15*time.Minute || d <= 0 {
		return 15 * time.Minute
	}
	return d
}
