package protocol

import (
	"crypto/ed25519"
	"crypto/rand"
	"testing"
	"time"
)

func keys(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return pub, priv
}

func TestSignVerify(t *testing.T) {
	pub, priv := keys(t)
	e, err := Sign(priv, TypePing, 1, map[string]string{"a": "b"})
	if err != nil {
		t.Fatal(err)
	}
	if err := Verify(pub, e, time.Now()); err != nil {
		t.Fatal(err)
	}
}

func TestTamperedPayloadRejected(t *testing.T) {
	pub, priv := keys(t)
	e, _ := Sign(priv, TypeAgentRemove, 1, Uninstall{Purge: false})
	e.Payload = []byte(`{"purge":true}`)
	if Verify(pub, e, time.Now()) == nil {
		t.Fatal("tampered payload must fail verification")
	}
}

func TestWrongKeyRejected(t *testing.T) {
	_, priv := keys(t)
	otherPub, _ := keys(t)
	e, _ := Sign(priv, TypePing, 1, nil)
	if Verify(otherPub, e, time.Now()) == nil {
		t.Fatal("signature from another key must fail")
	}
}

func TestStaleMessageRejected(t *testing.T) {
	pub, priv := keys(t)
	e, _ := Sign(priv, TypePing, 1, nil)
	if Verify(pub, e, time.Now().Add(10*time.Minute)) == nil {
		t.Fatal("stale message must fail")
	}
}

func TestSeqGuard(t *testing.T) {
	var g SeqGuard
	if g.Accept(1) != nil || g.Accept(2) != nil {
		t.Fatal("increasing seq must pass")
	}
	if g.Accept(2) == nil || g.Accept(1) == nil {
		t.Fatal("replay must be rejected")
	}
}
