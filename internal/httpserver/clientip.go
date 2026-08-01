package httpserver

import (
	"net"
	"net/http"
	"strings"
)

// clientIP returns the client address behind Caddy (X-Real-IP / X-Forwarded-For)
// or RemoteAddr when not trusting proxy headers.
func clientIP(r *http.Request, trustProxy bool) string {
	if trustProxy {
		if ip := headerIP(r.Header.Get("X-Real-IP")); ip != "" {
			return ip
		}
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			parts := strings.Split(xff, ",")
			if ip := headerIP(parts[0]); ip != "" {
				return ip
			}
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func headerIP(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return ""
	}
	if host, _, err := net.SplitHostPort(v); err == nil {
		return host
	}
	if net.ParseIP(v) == nil {
		return ""
	}
	return v
}
