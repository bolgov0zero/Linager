package server_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/cookiejar"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/pquerna/otp/totp"

	"github.com/bolgov0zero/linager/internal/config"
	"github.com/bolgov0zero/linager/server"
	"github.com/bolgov0zero/linager/server/store"
)

// startAccountsServer runs a panel on a throw-away database and returns its base URL (host "localhost",
// so passkeys are allowed) and the database. The admin is "admin" / "a-long-test-password".
func startAccountsServer(t *testing.T) (base string, db *store.Store) {
	t.Helper()
	dsn := os.Getenv("LINAGER_TEST_DSN")
	if dsn == "" {
		t.Skip("LINAGER_TEST_DSN is not set")
	}
	if !strings.Contains(dsn, "test") {
		t.Fatal("refusing to wipe a database whose DSN does not contain 'test'")
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx, `DROP SCHEMA public CASCADE; CREATE SCHEMA public`); err != nil {
		t.Fatal(err)
	}
	_ = conn.Close(ctx)
	dir := t.TempDir()
	cfg := &config.Server{DataDir: dir}
	cfg.Database.DSN = dsn
	cfg.Admin.Listen, cfg.Agents.Listen = freeAddr(t), freeAddr(t)
	cfg.Agents.Hosts = []string{"127.0.0.1"}
	cfg.Admin.CookieSecure = "never"
	cfg.Admin.LoginRate = 1000 // the tests sign in many times a minute
	if err := server.InitKeys(dir, cfg.Agents.Hosts); err != nil {
		t.Fatal(err)
	}
	db, err = store.Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	if err := server.CreateAdmin(ctx, db, "admin", "a-long-test-password"); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		_ = server.Run(ctx, cfg, nil, slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn})))
		close(done)
	}()
	t.Cleanup(func() { // the next test wipes the database: the server must be gone by then
		cancel()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
		}
	})
	waitFor(t, "server", func() bool {
		c, e := net.Dial("tcp", cfg.Admin.Listen)
		if e == nil {
			_ = c.Close()
		}
		return e == nil
	})
	_, port, _ := net.SplitHostPort(cfg.Admin.Listen)
	return "http://localhost:" + port, db
}

func newClient(t *testing.T, base string) *apiClient {
	jar, _ := cookiejar.New(nil)
	return &apiClient{t: t, base: base, hc: &http.Client{Jar: jar}}
}

func (c *apiClient) login(login, password string, extra map[string]string) (int, map[string]any) {
	body := map[string]string{"login": login, "password": password}
	for k, v := range extra {
		body[k] = v
	}
	var out map[string]any
	code := c.do("POST", "/api/login", body, &out)
	if s, ok := out["csrf"].(string); ok {
		c.csrf = s
	}
	return code, out
}

func mustLogin(t *testing.T, base, login, password string) *apiClient {
	t.Helper()
	c := newClient(t, base)
	if code, _ := c.login(login, password, nil); code != 200 {
		t.Fatalf("login %s: %d", login, code)
	}
	return c
}

func TestUsersPasswordRecoveryAndSnippets(t *testing.T) {
	base, _ := startAccountsServer(t)
	admin := mustLogin(t, base, "admin", "a-long-test-password")

	// --- users ---
	if c := admin.do("POST", "/api/users", map[string]string{"login": "Second", "password": "second-long-password"}, nil); c != 200 {
		t.Fatalf("create user: %d", c)
	}
	if c := admin.do("POST", "/api/users", map[string]string{"login": "second", "password": "second-long-password"}, nil); c != http.StatusConflict {
		t.Fatalf("duplicate login must be refused: %d", c)
	}
	if c := admin.do("POST", "/api/users", map[string]string{"login": "third", "password": "short"}, nil); c != http.StatusUnprocessableEntity {
		t.Fatalf("short password: %d", c)
	}
	if c := admin.do("POST", "/api/users", map[string]string{"login": "x y", "password": "third-long-password"}, nil); c != http.StatusUnprocessableEntity {
		t.Fatalf("bad login: %d", c)
	}
	var list struct {
		Users []struct {
			ID      int64  `json:"id"`
			Login   string `json:"login"`
			Current bool   `json:"current"`
		}
	}
	admin.do("GET", "/api/users", nil, &list)
	if len(list.Users) != 2 {
		t.Fatalf("users: %+v", list)
	}
	var selfID, secondID int64
	for _, u := range list.Users {
		if u.Current {
			selfID = u.ID
		}
		if u.Login == "second" {
			secondID = u.ID
		}
	}
	if c := admin.do("DELETE", fmt.Sprintf("/api/users/%d", selfID), map[string]bool{"confirm": true}, nil); c != http.StatusConflict {
		t.Fatalf("deleting yourself must be refused: %d", c)
	}
	if c := admin.do("DELETE", fmt.Sprintf("/api/users/%d", secondID), map[string]bool{}, nil); c != http.StatusConflict {
		t.Fatalf("deleting needs a confirmation: %d", c)
	}

	// --- own password: wrong current refused, other sessions end ---
	second := mustLogin(t, base, "second", "second-long-password")
	other := mustLogin(t, base, "second", "second-long-password")
	if c := second.do("POST", "/api/me/password", map[string]string{"current": "wrong-wrong-wrong", "password": "brand-new-password"}, nil); c != http.StatusForbidden {
		t.Fatalf("wrong current password: %d", c)
	}
	if c := second.do("POST", "/api/me/password", map[string]string{"current": "second-long-password", "password": "brand-new-password"}, nil); c != 200 {
		t.Fatalf("change password: %d", c)
	}
	if c := second.do("GET", "/api/me", nil, nil); c != 200 {
		t.Fatalf("the session that changed the password stays: %d", c)
	}
	if c := other.do("GET", "/api/me", nil, nil); c != http.StatusUnauthorized {
		t.Fatalf("the other session must end: %d", c)
	}
	if code, _ := newClient(t, base).login("second", "second-long-password", nil); code != http.StatusUnauthorized {
		t.Fatalf("the old password must stop working: %d", code)
	}

	// --- reset by an administrator ends the sessions of that user ---
	if c := admin.do("POST", fmt.Sprintf("/api/users/%d/password", secondID), map[string]any{"password": "reset-by-admin-pass"}, nil); c != http.StatusConflict {
		t.Fatalf("reset needs a confirmation: %d", c)
	}
	if c := admin.do("POST", fmt.Sprintf("/api/users/%d/password", secondID), map[string]any{"password": "reset-by-admin-pass", "confirm": true}, nil); c != 200 {
		t.Fatalf("reset: %d", c)
	}
	if c := second.do("GET", "/api/me", nil, nil); c != http.StatusUnauthorized {
		t.Fatalf("a reset must end the sessions of the user: %d", c)
	}
	second = mustLogin(t, base, "second", "reset-by-admin-pass")

	// --- 2FA with recovery codes ---
	var setup struct{ Secret string }
	second.do("POST", "/api/me/totp/setup", nil, &setup)
	code, _ := totp.GenerateCode(setup.Secret, time.Now())
	var en struct {
		Codes []string `json:"recovery_codes"`
	}
	if c := second.do("POST", "/api/me/totp/enable", map[string]string{"code": code}, &en); c != 200 || len(en.Codes) != 10 {
		t.Fatalf("enable 2FA must return 10 recovery codes: %d %v", c, en.Codes)
	}
	var me struct {
		Left int `json:"recovery_left"`
	}
	second.do("GET", "/api/me", nil, &me)
	if me.Left != 10 {
		t.Fatalf("recovery_left: %d", me.Left)
	}
	var first struct {
		Error string `json:"error"`
	}
	c1 := newClient(t, base)
	if code, out := c1.login("second", "reset-by-admin-pass", nil); code != http.StatusUnauthorized || out["error"] != server.ErrTOTPRequired {
		t.Fatalf("password alone must ask for the second factor: %d %v", code, out)
	}
	_ = first
	// a recovery code works once, typed in lower case without the dash
	typed := strings.ToLower(strings.ReplaceAll(en.Codes[0], "-", ""))
	if code, _ := c1.login("second", "reset-by-admin-pass", map[string]string{"totp": typed}); code != 200 {
		t.Fatalf("recovery code: %d", code)
	}
	if code, _ := newClient(t, base).login("second", "reset-by-admin-pass", map[string]string{"totp": en.Codes[0]}); code != http.StatusUnauthorized {
		t.Fatalf("a used recovery code must be refused: %d", code)
	}
	if code, _ := newClient(t, base).login("second", "reset-by-admin-pass", map[string]string{"totp": "AAAAA-AAAAA"}); code != http.StatusUnauthorized {
		t.Fatalf("an unknown recovery code must be refused: %d", code)
	}
	// regenerate: needs the password, replaces all codes
	if c := second.do("POST", "/api/me/recovery-codes", map[string]string{"password": "nope-nope-nope"}, nil); c != http.StatusForbidden {
		t.Fatalf("regenerate needs the password: %d", c)
	}
	var re struct {
		Codes []string `json:"recovery_codes"`
	}
	if c := second.do("POST", "/api/me/recovery-codes", map[string]string{"password": "reset-by-admin-pass"}, &re); c != 200 || len(re.Codes) != 10 {
		t.Fatalf("regenerate: %d", c)
	}
	if code, _ := newClient(t, base).login("second", "reset-by-admin-pass", map[string]string{"totp": en.Codes[1]}); code != http.StatusUnauthorized {
		t.Fatalf("old codes must stop working after a regeneration: %d", code)
	}
	if code, _ := newClient(t, base).login("second", "reset-by-admin-pass", map[string]string{"totp": re.Codes[0]}); code != 200 {
		t.Fatalf("a new code must work: %d", code)
	}
	// an administrator resets the 2FA of a user who lost the phone and the codes
	if c := admin.do("POST", fmt.Sprintf("/api/users/%d/2fa/reset", secondID), map[string]bool{"confirm": true}, nil); c != 200 {
		t.Fatalf("2FA reset: %d", c)
	}
	if code, _ := newClient(t, base).login("second", "reset-by-admin-pass", nil); code != 200 {
		t.Fatalf("after a 2FA reset the password alone signs in: %d", code)
	}
	if c := admin.do("DELETE", fmt.Sprintf("/api/users/%d", secondID), map[string]bool{"confirm": true}, nil); c != 200 {
		t.Fatalf("delete user: %d", c)
	}
	if code, _ := newClient(t, base).login("second", "reset-by-admin-pass", nil); code != http.StatusUnauthorized {
		t.Fatalf("a deleted user cannot sign in: %d", code)
	}

	// --- snippets ---
	var sn struct {
		ID int64 `json:"id"`
	}
	if c := admin.do("POST", "/api/snippets", map[string]string{"name": "", "command": "ls"}, nil); c != http.StatusUnprocessableEntity {
		t.Fatalf("empty name: %d", c)
	}
	if c := admin.do("POST", "/api/snippets", map[string]string{"name": "Disk", "command": "df -h\n"}, &sn); c != 200 || sn.ID == 0 {
		t.Fatalf("create snippet: %d", c)
	}
	var sn2 struct {
		ID int64 `json:"id"`
	}
	admin.do("POST", "/api/snippets", map[string]string{"name": "Mem", "command": "free -m"}, &sn2)
	if c := admin.do("PUT", fmt.Sprintf("/api/snippets/%d", sn.ID), map[string]string{"name": "Disk usage", "command": "df -h /"}, nil); c != 200 {
		t.Fatalf("update snippet: %d", c)
	}
	if c := admin.do("PUT", "/api/snippets/order", map[string]any{"ids": []int64{sn2.ID, sn.ID}}, nil); c != 200 {
		t.Fatalf("order: %d", c)
	}
	var sl struct {
		Snippets []struct {
			ID      int64
			Name    string
			Command string
		}
	}
	admin.do("GET", "/api/snippets", nil, &sl)
	if len(sl.Snippets) != 2 || sl.Snippets[0].ID != sn2.ID || sl.Snippets[1].Name != "Disk usage" || sl.Snippets[1].Command != "df -h /" {
		t.Fatalf("snippets: %+v", sl)
	}
	if c := admin.do("POST", fmt.Sprintf("/api/snippets/%d/ran", sn.ID), map[string]string{"server_id": "x"}, nil); c != 200 {
		t.Fatalf("ran: %d", c)
	}
	if c := admin.do("DELETE", fmt.Sprintf("/api/snippets/%d", sn.ID), nil, nil); c != 200 {
		t.Fatalf("delete snippet: %d", c)
	}
	if c := admin.do("DELETE", fmt.Sprintf("/api/snippets/%d", sn.ID), nil, nil); c != http.StatusNotFound {
		t.Fatalf("deleting a deleted snippet: %d", c)
	}
}

// ---- a software authenticator for the passkey tests ----

type softKey struct {
	priv   *ecdsa.PrivateKey
	credID []byte
	signs  uint32
}

func b64(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

// cbor helpers: just what an attestation object needs.
func cborHead(major byte, n int) []byte {
	switch {
	case n < 24:
		return []byte{major<<5 | byte(n)}
	case n < 256:
		return []byte{major<<5 | 24, byte(n)}
	default:
		return []byte{major<<5 | 25, byte(n >> 8), byte(n)}
	}
}
func cborInt(n int) []byte {
	if n >= 0 {
		return cborHead(0, n)
	}
	return cborHead(1, -1-n)
}
func cborBytes(b []byte) []byte { return append(cborHead(2, len(b)), b...) }
func cborText(s string) []byte  { return append(cborHead(3, len(s)), s...) }
func cborMap(pairs ...[]byte) []byte {
	out := cborHead(5, len(pairs)/2)
	for _, p := range pairs {
		out = append(out, p...)
	}
	return out
}

func (k *softKey) authData(rpID string, flags byte, withCred bool) []byte {
	h := sha256.Sum256([]byte(rpID))
	out := append([]byte{}, h[:]...)
	out = append(out, flags)
	k.signs++
	cnt := make([]byte, 4)
	binary.BigEndian.PutUint32(cnt, k.signs)
	out = append(out, cnt...)
	if withCred {
		out = append(out, make([]byte, 16)...) // aaguid
		out = append(out, byte(len(k.credID)>>8), byte(len(k.credID)))
		out = append(out, k.credID...)
		x, y := k.priv.PublicKey.X.FillBytes(make([]byte, 32)), k.priv.PublicKey.Y.FillBytes(make([]byte, 32))
		out = append(out, cborMap(cborInt(1), cborInt(2), cborInt(3), cborInt(-7), cborInt(-1), cborInt(1), cborInt(-2), cborBytes(x), cborInt(-3), cborBytes(y))...)
	}
	return out
}

type waOptions struct {
	ID      string `json:"id"`
	Options struct {
		PublicKey struct {
			Challenge string `json:"challenge"`
			RPID      string `json:"rpId"` // sign-in options
			RP        struct{ ID string }
			User      struct{ ID string }
		} `json:"publicKey"`
	} `json:"options"`
}

// register answers a registration challenge like an authenticator with user verification would.
func (k *softKey) register(origin string, o waOptions) map[string]any {
	cd, _ := json.Marshal(map[string]any{"type": "webauthn.create", "challenge": o.Options.PublicKey.Challenge, "origin": origin, "crossOrigin": false})
	att := cborMap(cborText("fmt"), cborText("none"), cborText("attStmt"), cborMap(), cborText("authData"), cborBytes(k.authData(o.Options.PublicKey.RP.ID, 0x45, true)))
	return map[string]any{"id": b64(k.credID), "rawId": b64(k.credID), "type": "public-key",
		"response":               map[string]any{"clientDataJSON": b64(cd), "attestationObject": b64(att)},
		"clientExtensionResults": map[string]any{}}
}

// assert answers a sign-in challenge; userHandle is what the authenticator stored at registration.
func (k *softKey) assert(origin string, o waOptions, userHandle string, key *ecdsa.PrivateKey) map[string]any {
	cd, _ := json.Marshal(map[string]any{"type": "webauthn.get", "challenge": o.Options.PublicKey.Challenge, "origin": origin, "crossOrigin": false})
	ad := k.authData(o.Options.PublicKey.RPID, 0x05, false)
	cdh := sha256.Sum256(cd)
	sum := sha256.Sum256(append(append([]byte{}, ad...), cdh[:]...))
	sig, _ := ecdsa.SignASN1(rand.Reader, key, sum[:])
	return map[string]any{"id": b64(k.credID), "rawId": b64(k.credID), "type": "public-key",
		"response":               map[string]any{"clientDataJSON": b64(cd), "authenticatorData": b64(ad), "signature": b64(sig), "userHandle": userHandle},
		"clientExtensionResults": map[string]any{}}
}

func TestPasskeySignIn(t *testing.T) {
	base, _ := startAccountsServer(t)
	admin := mustLogin(t, base, "admin", "a-long-test-password")

	priv, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	key := &softKey{priv: priv, credID: make([]byte, 24)}
	_, _ = rand.Read(key.credID)

	// registration (signed in)
	var reg waOptions
	if c := admin.do("POST", "/api/me/passkeys/begin", map[string]string{}, &reg); c != 200 || reg.ID == "" {
		t.Fatalf("register begin: %d", c)
	}
	if reg.Options.PublicKey.RP.ID != "localhost" {
		t.Fatalf("the relying party id must be the host: %q", reg.Options.PublicKey.RP.ID)
	}
	userHandle := reg.Options.PublicKey.User.ID
	if c := admin.do("POST", "/api/me/passkeys/finish", map[string]any{"id": reg.ID, "name": "Test key", "response": key.register(base, reg)}, nil); c != 200 {
		t.Fatalf("register finish: %d", c)
	}
	if c := admin.do("POST", "/api/me/passkeys/finish", map[string]any{"id": reg.ID, "name": "again", "response": key.register(base, reg)}, nil); c != http.StatusBadRequest {
		t.Fatalf("a challenge is used once: %d", c)
	}
	var pl struct {
		Passkeys []struct {
			ID   string
			Name string
		}
	}
	admin.do("GET", "/api/me/passkeys", nil, &pl)
	if len(pl.Passkeys) != 1 || pl.Passkeys[0].Name != "Test key" {
		t.Fatalf("passkeys: %+v", pl)
	}

	// sign in with the passkey: no login, no password
	signIn := func(sign func(o waOptions) map[string]any) (int, *apiClient) {
		c := newClient(t, base)
		var o waOptions
		if code := c.do("POST", "/api/login/passkey/begin", map[string]string{}, &o); code != 200 {
			t.Fatalf("login begin: %d", code)
		}
		var out map[string]any
		code := c.do("POST", "/api/login/passkey/finish", map[string]any{"id": o.ID, "response": sign(o)}, &out)
		if s, ok := out["csrf"].(string); ok {
			c.csrf = s
		}
		return code, c
	}
	code, c := signIn(func(o waOptions) map[string]any { return key.assert(base, o, userHandle, priv) })
	if code != 200 {
		t.Fatalf("passkey sign-in: %d", code)
	}
	var me struct {
		Login    string `json:"login"`
		Passkeys int    `json:"passkeys"`
	}
	if c.do("GET", "/api/me", nil, &me); me.Login != "admin" || me.Passkeys != 1 {
		t.Fatalf("session of the passkey sign-in: %+v", me)
	}
	// a signature of another key is refused
	other, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if code, _ := signIn(func(o waOptions) map[string]any { return key.assert(base, o, userHandle, other) }); code != http.StatusUnauthorized {
		t.Fatalf("a wrong signature must be refused: %d", code)
	}
	// an unknown user handle is refused
	if code, _ := signIn(func(o waOptions) map[string]any { return key.assert(base, o, b64([]byte("nobody")), priv) }); code != http.StatusUnauthorized {
		t.Fatalf("an unknown user handle must be refused: %d", code)
	}
	// a replayed finish (same challenge id) is refused
	cc := newClient(t, base)
	var o waOptions
	cc.do("POST", "/api/login/passkey/begin", map[string]string{}, &o)
	resp := key.assert(base, o, userHandle, priv)
	if code := cc.do("POST", "/api/login/passkey/finish", map[string]any{"id": o.ID, "response": resp}, nil); code != 200 {
		t.Fatalf("first finish: %d", code)
	}
	if code := cc.do("POST", "/api/login/passkey/finish", map[string]any{"id": o.ID, "response": resp}, nil); code != http.StatusBadRequest {
		t.Fatalf("a replay must be refused: %d", code)
	}
	// another origin is refused
	if code, _ := signIn(func(o waOptions) map[string]any { return key.assert("http://evil.example", o, userHandle, priv) }); code != http.StatusUnauthorized {
		t.Fatalf("a foreign origin must be refused: %d", code)
	}

	// removing the passkey needs the password and stops the sign-in
	if c := admin.do("DELETE", "/api/me/passkeys/"+pl.Passkeys[0].ID, map[string]string{"password": "wrong-wrong-wrong"}, nil); c != http.StatusForbidden {
		t.Fatalf("delete needs the password: %d", c)
	}
	if c := admin.do("DELETE", "/api/me/passkeys/"+pl.Passkeys[0].ID, map[string]string{"password": "a-long-test-password"}, nil); c != 200 {
		t.Fatalf("delete passkey: %d", c)
	}
	if code, _ := signIn(func(o waOptions) map[string]any { return key.assert(base, o, userHandle, priv) }); code != http.StatusUnauthorized {
		t.Fatalf("a removed passkey must not sign in: %d", code)
	}

	// an IP address host cannot be a relying party
	ipc := newClient(t, strings.Replace(base, "localhost", "127.0.0.1", 1))
	if code := ipc.do("POST", "/api/login/passkey/begin", map[string]string{}, nil); code != http.StatusConflict {
		t.Fatalf("passkeys must be refused on an IP host: %d", code)
	}
}
