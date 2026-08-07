package httpserver

import (
	"encoding/json"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// SecurityLimits holds tunable anti-abuse settings.
type SecurityLimits struct {
	TrustProxyHeaders bool
	LoginMaxAttempts  int
	LoginWindow       time.Duration
	LoginLockout      time.Duration
	LoginRatePerMin   int
	APIRatePerMin     int
	StaticRatePerMin  int
	// TrustedIPs — IP/CIDR, освобождённые от rate-limit и login-lockout.
	TrustedIPs []string
}

// parseTrustedNets превращает список IP/CIDR в сети. Одиночный IP трактуется как /32 (или /128).
func parseTrustedNets(entries []string) []*net.IPNet {
	nets := make([]*net.IPNet, 0, len(entries))
	for _, raw := range entries {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		if _, n, err := net.ParseCIDR(raw); err == nil {
			nets = append(nets, n)
			continue
		}
		if ip := net.ParseIP(raw); ip != nil {
			bits := 32
			if ip.To4() == nil {
				bits = 128
			}
			nets = append(nets, &net.IPNet{IP: ip, Mask: net.CIDRMask(bits, bits)})
		}
	}
	return nets
}

func defaultSecurityLimits() SecurityLimits {
	return SecurityLimits{
		TrustProxyHeaders: true,
		LoginMaxAttempts:  5,
		LoginWindow:       15 * time.Minute,
		LoginLockout:      15 * time.Minute,
		LoginRatePerMin:   20,
		APIRatePerMin:     180,
		StaticRatePerMin:  600,
	}
}

type windowCounter struct {
	times []time.Time
}

type lockState struct {
	until time.Time
	fails []time.Time
}

// SecurityLimiter in-memory rate limits + login lockout (per process).
type SecurityLimiter struct {
	lim         SecurityLimits
	trustedNets []*net.IPNet

	mu      sync.Mutex
	windows map[string]*windowCounter
	locks   map[string]*lockState
}

// isTrusted returns true when the client IP is exempt from rate limiting / lockout.
func (s *SecurityLimiter) isTrusted(ip string) bool {
	if ip == "" {
		return false
	}
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return false
	}
	for _, n := range s.trustedNets {
		if n.Contains(parsed) {
			return true
		}
	}
	return false
}

func newSecurityLimiter(lim SecurityLimits) *SecurityLimiter {
	d := defaultSecurityLimits()
	if lim.LoginMaxAttempts > 0 {
		d.LoginMaxAttempts = lim.LoginMaxAttempts
	}
	if lim.LoginWindow > 0 {
		d.LoginWindow = lim.LoginWindow
	}
	if lim.LoginLockout > 0 {
		d.LoginLockout = lim.LoginLockout
	}
	if lim.LoginRatePerMin > 0 {
		d.LoginRatePerMin = lim.LoginRatePerMin
	}
	if lim.APIRatePerMin > 0 {
		d.APIRatePerMin = lim.APIRatePerMin
	}
	if lim.StaticRatePerMin > 0 {
		d.StaticRatePerMin = lim.StaticRatePerMin
	}
	d.TrustProxyHeaders = lim.TrustProxyHeaders

	sl := &SecurityLimiter{
		lim:         d,
		trustedNets: parseTrustedNets(lim.TrustedIPs),
		windows:     make(map[string]*windowCounter),
		locks:       make(map[string]*lockState),
	}
	go sl.cleanupLoop()
	return sl
}

func (s *SecurityLimiter) cleanupLoop() {
	t := time.NewTicker(5 * time.Minute)
	defer t.Stop()
	for range t.C {
		s.cleanup()
	}
}

func (s *SecurityLimiter) cleanup() {
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	for k, w := range s.windows {
		w.prune(now.Add(-2 * time.Minute))
		if len(w.times) == 0 {
			delete(s.windows, k)
		}
	}
	for k, l := range s.locks {
		l.pruneFails(now.Add(-s.lim.LoginWindow))
		if l.until.Before(now) && len(l.fails) == 0 {
			delete(s.locks, k)
		}
	}
}

func (w *windowCounter) prune(cutoff time.Time) {
	i := 0
	for i < len(w.times) && w.times[i].Before(cutoff) {
		i++
	}
	if i > 0 {
		w.times = append([]time.Time(nil), w.times[i:]...)
	}
}

func (l *lockState) pruneFails(cutoff time.Time) {
	i := 0
	for i < len(l.fails) && l.fails[i].Before(cutoff) {
		i++
	}
	if i > 0 {
		l.fails = append([]time.Time(nil), l.fails[i:]...)
	}
}

func (s *SecurityLimiter) allow(key string, maxPerMin int) bool {
	if maxPerMin <= 0 {
		return true
	}
	now := time.Now()
	cutoff := now.Add(-time.Minute)
	s.mu.Lock()
	defer s.mu.Unlock()
	w := s.windows[key]
	if w == nil {
		w = &windowCounter{}
		s.windows[key] = w
	}
	w.prune(cutoff)
	if len(w.times) >= maxPerMin {
		return false
	}
	w.times = append(w.times, now)
	return true
}

func (s *SecurityLimiter) isLoginLocked(ip, username string) (bool, time.Duration) {
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	keys := []string{"ip:" + ip}
	if username != "" {
		keys = append(keys, "user:"+strings.ToLower(username))
	}
	for _, key := range keys {
		if st, ok := s.locks[key]; ok && st.until.After(now) {
			return true, st.until.Sub(now)
		}
	}
	return false, 0
}

func (s *SecurityLimiter) recordLoginFailure(ip, username string) {
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	keys := []string{"ip:" + ip}
	if username != "" {
		keys = append(keys, "user:"+strings.ToLower(username))
	}
	for _, key := range keys {
		st := s.locks[key]
		if st == nil {
			st = &lockState{}
			s.locks[key] = st
		}
		st.pruneFails(now.Add(-s.lim.LoginWindow))
		st.fails = append(st.fails, now)
		if len(st.fails) >= s.lim.LoginMaxAttempts {
			st.until = now.Add(s.lim.LoginLockout)
			st.fails = nil
		}
	}
}

func (s *SecurityLimiter) clearLoginFailures(ip, username string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.locks, "ip:"+ip)
	if username != "" {
		delete(s.locks, "user:"+strings.ToLower(username))
	}
}

func writeRateLimited(w http.ResponseWriter, retryAfter time.Duration) {
	w.Header().Set("Content-Type", "application/json")
	sec := int(retryAfter.Seconds())
	if sec < 1 {
		sec = 1
	}
	w.Header().Set("Retry-After", strconv.Itoa(sec))
	w.WriteHeader(http.StatusTooManyRequests)
	_ = json.NewEncoder(w).Encode(ErrorResponse{Error: "Слишком много запросов. Попробуйте позже."})
}

func (s *Server) securityMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.limiter == nil {
			next.ServeHTTP(w, r)
			return
		}
		ip := clientIP(r, s.limiter.lim.TrustProxyHeaders)
		path := r.URL.Path

		// За NAT/reverse-proxy реальные клиенты приходят под одним адресом (шлюз),
		// поэтому доверенные IP освобождаем от общего per-IP лимита.
		if s.limiter.isTrusted(ip) {
			next.ServeHTTP(w, r)
			return
		}

		var max int
		var bucket string
		switch {
		case path == "/auth/login":
			max = s.limiter.lim.LoginRatePerMin
			bucket = "login:" + ip
		case strings.HasPrefix(path, "/api/") || path == "/health" || strings.HasPrefix(path, "/swagger"):
			max = s.limiter.lim.APIRatePerMin
			bucket = "api:" + ip
		default:
			max = s.limiter.lim.StaticRatePerMin
			bucket = "static:" + ip
		}

		if !s.limiter.allow(bucket, max) {
			if s.fileLogger != nil {
				s.fileLogger.Warn("rate limit hit path=%s ip=%s", path, ip)
			}
			writeRateLimited(w, time.Minute)
			return
		}
		next.ServeHTTP(w, r)
	})
}
