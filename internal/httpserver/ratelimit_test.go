package httpserver

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestClientIPFromProxy(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "127.0.0.1:12345"
	r.Header.Set("X-Real-IP", "203.0.113.10")
	if got := clientIP(r, true); got != "203.0.113.10" {
		t.Fatalf("got %q", got)
	}
}

func TestLoginLockout(t *testing.T) {
	lim := newSecurityLimiter(SecurityLimits{
		TrustProxyHeaders: true,
		LoginMaxAttempts:  3,
		LoginWindow:       time.Minute,
		LoginLockout:      time.Minute,
		LoginRatePerMin:   100,
		APIRatePerMin:     100,
		StaticRatePerMin:  100,
	})
	ip := "198.51.100.1"
	user := "admin"
	for i := 0; i < 3; i++ {
		lim.recordLoginFailure(ip, user)
	}
	locked, _ := lim.isLoginLocked(ip, user)
	if !locked {
		t.Fatal("expected lockout after max attempts")
	}
	lim.clearLoginFailures(ip, user)
	locked, _ = lim.isLoginLocked(ip, user)
	if locked {
		t.Fatal("expected lock cleared")
	}
}

func TestIsTrusted(t *testing.T) {
	lim := newSecurityLimiter(SecurityLimits{
		TrustProxyHeaders: true,
		TrustedIPs:        []string{"127.0.0.1/32", "192.168.0.0/16", "10.0.0.5"},
	})
	cases := map[string]bool{
		"192.168.95.1": true,  // шлюз за NAT — доверенный диапазон
		"127.0.0.1":    true,  // loopback
		"10.0.0.5":     true,  // одиночный IP → /32
		"10.0.0.6":     false, // вне списка
		"203.0.113.10": false, // публичный — лимит остаётся
		"":             false,
		"not-an-ip":    false,
	}
	for ip, want := range cases {
		if got := lim.isTrusted(ip); got != want {
			t.Fatalf("isTrusted(%q)=%v, want %v", ip, got, want)
		}
	}
}

func TestRateAllow(t *testing.T) {
	lim := newSecurityLimiter(SecurityLimits{
		TrustProxyHeaders: true,
		LoginMaxAttempts:  5,
		LoginRatePerMin:   2,
		APIRatePerMin:     2,
		StaticRatePerMin:  2,
	})
	if !lim.allow("api:1.2.3.4", 2) || !lim.allow("api:1.2.3.4", 2) {
		t.Fatal("first two should pass")
	}
	if lim.allow("api:1.2.3.4", 2) {
		t.Fatal("third should fail")
	}
}
