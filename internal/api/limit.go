package api

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/TheOriUHD/aperture/internal/auth"
)

// Request-size and rate limits (T2.6).
//
// Body caps: JSON bodies are capped at 1 MiB (a malformed/huge config can't
// exhaust memory); multipart uploads (backup restore, config import) get a large
// cap. A declared over-size body is rejected 413 up front; an undeclared one
// trips MaxBytesReader mid-read.
//
// Rate limiting: a token bucket per (class,id) key. Sessions/CLI (the web app,
// nvrctl) get a generous bucket; scoped tokens (extensions) a tighter one; and
// because the buckets are keyed by class, a burst of extension calls consumes
// the token bucket only — it can never starve the web app's session bucket.
//
// Neither the http.Server WriteTimeout nor ReadTimeout is set (both 0): a global
// write deadline would kill SSE and long media exports, and a read deadline would
// kill slow large uploads. Slowloris is bounded by ReadHeaderTimeout + the body
// caps here instead; idle keep-alives by IdleTimeout. (Plan critique C-4.)

const (
	jsonBodyLimit      = 1 << 20   // 1 MiB
	multipartBodyLimit = 256 << 20 // 256 MiB (backup restore / import)
)

// readBodyTimeout bounds how long a small (non-multipart) request body may take
// to arrive, so a slow-drip client can't hold a connection indefinitely —
// MaxBytesReader caps a body's size but not its time. 30s is enormous for a
// ≤1 MiB JSON body (≈34 KiB/s) yet cuts off a slowloris body. This is the
// missing-ReadTimeout mitigation without a GLOBAL read deadline (which would
// kill slow large uploads — plan C-4); it is applied per-request only where it
// is safe (see limitBody). A var, not a const, only so the keep-alive test can
// shrink it; never reassigned in production.
var readBodyTimeout = 30 * time.Second

// limitBody caps the request body. Multipart uploads get the large cap; anything
// else the JSON cap. A Content-Length past the cap is refused immediately; a
// chunked/undeclared body is bounded by MaxBytesReader as the handler reads it.
// For non-multipart bodies it also sets a per-request READ deadline so a slow
// body can't hold a slot forever; GET/SSE/WebSocket (no body) and multipart
// uploads (backup restore — legitimately large and slow) are exempt, so
// streaming and uploads are untouched.
func (s *Server) limitBody(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cap := int64(jsonBodyLimit)
		multipart := strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/")
		if multipart {
			cap = multipartBodyLimit
		}
		if r.ContentLength > cap {
			writeErrorCode(w, http.StatusRequestEntityTooLarge, "payload_too_large", "request body too large")
			return
		}
		if r.Body != nil {
			r.Body = http.MaxBytesReader(w, r.Body, cap)
			// Only bound methods that carry a body, and only non-multipart. On a
			// bare httptest recorder there is no connection, so SetReadDeadline is
			// unsupported and the error is ignored (a no-op in unit tests).
			if !multipart && (r.Method == http.MethodPost || r.Method == http.MethodPut || r.Method == http.MethodPatch) {
				_ = http.NewResponseController(w).SetReadDeadline(time.Now().Add(readBodyTimeout))
			}
		}
		next.ServeHTTP(w, r)
	})
}

// rateConfig is a token bucket: burst capacity and refill rate (tokens/sec).
type rateConfig struct {
	burst  float64
	refill float64
}

type bucket struct {
	tokens float64
	last   time.Time
}

// rateLimiter holds per-key buckets. Configs are fields so tests can shrink them.
type rateLimiter struct {
	mu      sync.Mutex
	buckets map[string]*bucket
	session rateConfig
	token   rateConfig
	anon    rateConfig
}

func newRateLimiter() *rateLimiter {
	return &rateLimiter{
		buckets: map[string]*bucket{},
		session: rateConfig{burst: 240, refill: 120}, // the web app is chatty
		token:   rateConfig{burst: 40, refill: 20},   // extensions
		anon:    rateConfig{burst: 30, refill: 10},   // pre-auth, keyed by IP
	}
}

// allow consumes one token from key's bucket, refilling by elapsed time first.
// now is injected so tests are deterministic.
func (l *rateLimiter) allow(key string, cfg rateConfig, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	b := l.buckets[key]
	if b == nil {
		b = &bucket{tokens: cfg.burst, last: now}
		l.buckets[key] = b
	}
	if elapsed := now.Sub(b.last).Seconds(); elapsed > 0 {
		b.tokens += elapsed * cfg.refill
		if b.tokens > cfg.burst {
			b.tokens = cfg.burst
		}
		b.last = now
	}
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// prune drops buckets idle longer than idle, bounding map growth from many
// distinct IPs/tokens over time.
func (l *rateLimiter) prune(now time.Time, idle time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for k, b := range l.buckets {
		if now.Sub(b.last) > idle {
			delete(l.buckets, k)
		}
	}
}

// rateKey returns the bucket key and config for a request: authenticated callers
// key by class+principal (session and token of the same user are SEPARATE
// buckets); anonymous callers key by IP.
func (s *Server) rateKey(r *http.Request) (string, rateConfig) {
	c := auth.FromContext(r.Context())
	if c == nil {
		return "anon:" + clientIP(r), s.limiter.anon
	}
	switch c.Class {
	case auth.ClassToken:
		return "token:" + c.UserID, s.limiter.token
	case auth.ClassCLI:
		return "cli:" + c.UserID, s.limiter.session
	default:
		return "session:" + c.UserID, s.limiter.session
	}
}

func (s *Server) rateLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key, cfg := s.rateKey(r)
		if !s.limiter.allow(key, cfg, time.Now()) {
			w.Header().Set("Retry-After", "1")
			writeErrorCode(w, http.StatusTooManyRequests, "rate_limited", "rate limit exceeded")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// StartRateLimitSweeper prunes idle buckets periodically. Called from main (not
// the constructor) so the test server spawns no background goroutine.
func (s *Server) StartRateLimitSweeper() {
	go func() {
		t := time.NewTicker(5 * time.Minute)
		defer t.Stop()
		for range t.C {
			s.limiter.prune(time.Now(), 15*time.Minute)
		}
	}()
}

func clientIP(r *http.Request) string {
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}
