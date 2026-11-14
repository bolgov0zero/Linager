package server

import (
	"testing"
	"time"

	"github.com/pquerna/otp/totp"
)

func TestPasswordHashing(t *testing.T) {
	h, err := HashPassword("correct horse battery")
	if err != nil {
		t.Fatal(err)
	}
	if !VerifyPassword("correct horse battery", h) || VerifyPassword("wrong password!!", h) {
		t.Fatal("password verification failed")
	}
	if _, err := HashPassword("short"); err == nil {
		t.Fatal("short passwords must be rejected")
	}
	if VerifyPassword("x", "garbage") {
		t.Fatal("malformed hash must not verify")
	}
}

func TestTOTPMatchAndStep(t *testing.T) {
	secret, _, err := NewTOTPSecret("admin")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	code, _ := totp.GenerateCode(secret, now)
	step, err := MatchTOTP(secret, code, now)
	if err != nil || step != now.Unix()/30 {
		t.Fatalf("step=%d err=%v", step, err)
	}
	if _, err := MatchTOTP(secret, "000000x", now); err == nil {
		t.Fatal("invalid code must fail")
	}
}

func TestLimiterAndLock(t *testing.T) {
	l := newLimiter(3, time.Minute)
	for i := 0; i < 3; i++ {
		if !l.Allow("a") {
			t.Fatal("should allow")
		}
	}
	if l.Allow("a") || !l.Allow("b") {
		t.Fatal("limiter misbehaves")
	}
	if lockDuration(4) != 0 || lockDuration(5) != time.Minute || lockDuration(50) != 15*time.Minute {
		t.Fatal("lockDuration wrong")
	}
}
