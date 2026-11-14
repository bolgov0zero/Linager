package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func req(remote, xff string) *http.Request {
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = remote
	if xff != "" {
		r.Header.Set("X-Forwarded-For", xff)
	}
	return r
}

func TestClientIP(t *testing.T) {
	trust, _ := NewProxyTrust([]string{"10.0.0.0/8"})
	cases := []struct{ remote, xff, want string }{
		{"203.0.113.5:1234", "", "203.0.113.5"},
		{"203.0.113.5:1234", "1.1.1.1", "203.0.113.5"}, // untrusted peer: header ignored
		{"10.0.0.2:80", "198.51.100.7", "198.51.100.7"},
		{"10.0.0.2:80", "6.6.6.6, 198.51.100.7, 10.0.0.9", "198.51.100.7"},
		{"10.0.0.2:80", "", "10.0.0.2"},
	}
	for _, c := range cases {
		if got := trust.ClientIP(req(c.remote, c.xff)); got != c.want {
			t.Errorf("remote=%s xff=%q got %s want %s", c.remote, c.xff, got, c.want)
		}
	}
	all, _ := NewProxyTrust([]string{"0.0.0.0/0"})
	if got := all.ClientIP(req("10.1.1.1:1", "8.8.8.8, 9.9.9.9")); got != "8.8.8.8" {
		t.Errorf("trust-all should fall back to left-most, got %s", got)
	}
}
