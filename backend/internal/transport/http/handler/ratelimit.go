package handler

import (
	"net"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"time"
)

// rateLimiter counts attempts per key in fixed windows. The zero value is ready to use.
// shortcut: in memory and per process; counters reset on restart. Move to a shared store if there
// is ever more than one instance.
type rateLimiter struct {
	mu      sync.Mutex
	windows map[string]*rateWindow
}

type rateWindow struct {
	n     int
	reset time.Time
}

const rateLimiterPruneAt = 10_000

// allow records one attempt and reports whether it is still within max per window.
func (l *rateLimiter) allow(key string, max int, window time.Duration) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	if l.windows == nil {
		l.windows = make(map[string]*rateWindow)
	}
	if len(l.windows) >= rateLimiterPruneAt {
		for k, w := range l.windows {
			if now.After(w.reset) {
				delete(l.windows, k)
			}
		}
	}
	w := l.windows[key]
	if w == nil || now.After(w.reset) {
		w = &rateWindow{reset: now.Add(window)}
		l.windows[key] = w
	}
	if w.n >= max {
		return false
	}
	w.n++
	return true
}

// refund takes back one attempt, so that successful logins do not count.
func (l *rateLimiter) refund(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if w := l.windows[key]; w != nil && w.n > 0 {
		w.n--
	}
}

// clientIP is the key for per-client limits. X-Forwarded-For is forgeable, so it only counts behind
// a trusted proxy, and then only its last entry: the one the proxy appended itself.
func (s *Server) clientIP(r *http.Request) string {
	ip := r.RemoteAddr
	if host, _, err := net.SplitHostPort(ip); err == nil {
		ip = host
	}
	if s.Config.TrustProxy {
		if values := r.Header.Values("X-Forwarded-For"); len(values) > 0 {
			parts := strings.Split(values[len(values)-1], ",")
			ip = strings.TrimSpace(parts[len(parts)-1])
		}
	}
	// One IPv6 customer owns a whole /64.
	if a, err := netip.ParseAddr(ip); err == nil && a.Is6() && !a.Is4In6() {
		if p, err := a.Prefix(64); err == nil {
			return p.String()
		}
	}
	return ip
}

func (s *Server) tooManyRequests(w http.ResponseWriter) {
	w.Header().Set("Retry-After", "900")
	s.writeJSONError(w, http.StatusTooManyRequests, "Too many attempts. Please try again later.")
}
