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
