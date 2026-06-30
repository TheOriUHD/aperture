// Package logbuf provides a slog.Handler that mirrors recent log lines into a
// fixed-size in-memory ring, so the diagnostics bundle can include the tail of
// the daemon's logs without reading files or changing where logs are written.
package logbuf

import (
	"bytes"
	"context"
	"log/slog"
	"sync"
)

// ringState is the shared circular buffer; all handlers derived via WithAttrs /
// WithGroup point at the same state so every log line lands in one ring.
type ringState struct {
	mu   sync.Mutex
	buf  []string
	n    int
	next int
	full bool
}

func (s *ringState) append(line string) {
	s.mu.Lock()
	s.buf[s.next] = line
	s.next = (s.next + 1) % s.n
	if s.next == 0 {
		s.full = true
	}
	s.mu.Unlock()
}

// LastN returns up to the most recent n retained lines, oldest first.
func (s *ringState) LastN(n int) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	count := s.n
	if !s.full {
		count = s.next
	}
	if n > 0 && n < count {
		count = n
	}
	out := make([]string, 0, count)
	for i := 0; i < count; i++ {
		idx := (s.next - 1 - i + s.n*2) % s.n
		out = append(out, s.buf[idx])
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

// Ring wraps an inner slog.Handler and retains the last N formatted records.
type Ring struct {
	inner slog.Handler
	state *ringState
}

// New returns a Ring delegating to inner and retaining the last n lines.
func New(inner slog.Handler, n int) *Ring {
	if n <= 0 {
		n = 500
	}
	return &Ring{inner: inner, state: &ringState{buf: make([]string, n), n: n}}
}

func (r *Ring) Enabled(ctx context.Context, l slog.Level) bool { return r.inner.Enabled(ctx, l) }

func (r *Ring) Handle(ctx context.Context, rec slog.Record) error {
	var b bytes.Buffer
	b.WriteString(rec.Time.UTC().Format("2006-01-02T15:04:05.000Z"))
	b.WriteByte(' ')
	b.WriteString(rec.Level.String())
	b.WriteByte(' ')
	b.WriteString(rec.Message)
	rec.Attrs(func(a slog.Attr) bool {
		b.WriteByte(' ')
		b.WriteString(a.Key)
		b.WriteByte('=')
		b.WriteString(a.Value.String())
		return true
	})
	r.state.append(b.String())
	return r.inner.Handle(ctx, rec)
}

func (r *Ring) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &Ring{inner: r.inner.WithAttrs(attrs), state: r.state}
}

func (r *Ring) WithGroup(name string) slog.Handler {
	return &Ring{inner: r.inner.WithGroup(name), state: r.state}
}

// LastN returns up to the most recent n retained lines, oldest first.
func (r *Ring) LastN(n int) []string { return r.state.LastN(n) }
