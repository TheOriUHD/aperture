// Package audit records a tamper-evident, hash-chained trail of sensitive
// actions (logins, config changes, deletions, exports, shares, footage access).
package audit

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/TheOriUHD/aperture/internal/auth"
	"github.com/TheOriUHD/aperture/internal/store"
)

// Logger serializes audit writes so the hash chain stays coherent.
type Logger struct {
	st *store.Store
	mu sync.Mutex
}

func New(st *store.Store) *Logger { return &Logger{st: st} }

// Record writes an entry, deriving the actor from the request session and the
// IP/UA from the request. Best-effort — failures never block the caller.
func (l *Logger) Record(r *http.Request, action, target, detail string) {
	var id, email string
	if c := auth.FromContext(r.Context()); c != nil {
		id, email = c.UserID, c.Email
	}
	l.write(id, email, action, target, detail, clientIP(r), r.UserAgent())
}

// RecordAs writes an entry with an explicit actor email (e.g. a failed login,
// where no session exists yet).
func (l *Logger) RecordAs(r *http.Request, actorEmail, action, target, detail string) {
	l.write("", actorEmail, action, target, detail, clientIP(r), r.UserAgent())
}

func (l *Logger) write(id, email, action, target, detail, ip, ua string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	_ = l.st.AppendAudit(&store.AuditEntry{
		TS: time.Now().Unix(), ActorID: id, ActorEmail: email,
		Action: action, Target: target, Detail: detail, IP: ip, UA: ua,
	})
}

func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		return strings.TrimSpace(strings.Split(xff, ",")[0])
	}
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}
