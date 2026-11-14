package release

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"os"
	"testing"

	"golang.org/x/crypto/blake2b"
)

func TestVerifyRealMinisignFixture(t *testing.T) {
	pub, _ := os.ReadFile("testdata/release.pub")
	msg, _ := os.ReadFile("testdata/message.txt")
	sig, _ := os.ReadFile("testdata/message.txt.minisig")
	k, err := ParsePublicKey(string(pub))
	if err != nil {
		t.Fatal(err)
	}
	if err := k.Verify(msg, string(sig)); err != nil {
		t.Fatalf("a signature made by the minisign tool must verify: %v", err)
	}
	if k.Verify(append(msg, '!'), string(sig)) == nil {
		t.Fatal("a modified message must not verify")
	}
	if e, err := Embedded(); err != nil || e.ID != k.ID {
		t.Fatal("the embedded key must be the release key used for the fixture")
	}
}

// signFile builds a minisign signature file for tests; prehash selects the "ED" algorithm.
func signFile(priv ed25519.PrivateKey, id [8]byte, msg []byte, prehash bool, trusted string) string {
	alg, body := "Ed", msg
	if prehash {
		h := blake2b.Sum512(msg)
		alg, body = "ED", h[:]
	}
	sig := ed25519.Sign(priv, body)
	raw := append(append([]byte(alg), id[:]...), sig...)
	global := ed25519.Sign(priv, append(append([]byte(nil), sig...), trusted...))
	return "untrusted comment: test\n" + base64.StdEncoding.EncodeToString(raw) + "\ntrusted comment: " + trusted + "\n" + base64.StdEncoding.EncodeToString(global) + "\n"
}

func testKey(t *testing.T) (*Key, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	k := &Key{Pub: pub}
	copy(k.ID[:], "testkey1")
	return k, priv
}

func TestVerifyBothAlgorithmsAndTampering(t *testing.T) {
	k, priv := testKey(t)
	msg := []byte("manifest")
	for _, pre := range []bool{false, true} {
		sig := signFile(priv, k.ID, msg, pre, "timestamp:1")
		if err := k.Verify(msg, sig); err != nil {
			t.Fatalf("prehash=%v: %v", pre, err)
		}
		if k.Verify([]byte("other"), sig) == nil {
			t.Fatal("wrong message must fail")
		}
	}
	good := signFile(priv, k.ID, msg, false, "timestamp:1")
	tampered := []byte(good)
	copy(tampered[len(tampered)-120:], "X") // damage the trusted-comment signature area
	if k.Verify(msg, string(tampered)) == nil {
		t.Fatal("a damaged signature file must fail")
	}
	other, _ := testKey(t)
	copy(other.ID[:], "otherkey")
	if other.Verify(msg, good) == nil {
		t.Fatal("a different key must not verify")
	}
	if k.Verify(msg, "garbage") == nil {
		t.Fatal("garbage must fail")
	}
}

func TestVersions(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"v0.1.0", "v0.0.9", 1},
		{"0.1.0", "v0.1.0", 0},
		{"v0.2.0-beta.1", "v0.2.0", -1},
		{"v0.2.0-beta.2", "v0.2.0-beta.1", 1},
		{"v1.0.0", "v0.99.99", 1},
		{"dev", "v0.0.1", -1},
		{"v0.0.5-stage5", "v0.1.0", -1},
	}
	for _, c := range cases {
		if got := Compare(ParseVersion(c.a), ParseVersion(c.b)); got != c.want {
			t.Errorf("Compare(%s,%s)=%d want %d", c.a, c.b, got, c.want)
		}
	}
	if !Newer("v0.1.0", "dev") || Newer("v0.1.0", "v0.1.0") {
		t.Fatal("Newer misbehaves")
	}
}
