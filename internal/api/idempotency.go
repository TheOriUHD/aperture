package api

import (
	"bytes"
	"net/http"
	"time"

	"github.com/TheOriUHD/aperture/internal/auth"
)

const idempotencyTTL = 24 * time.Hour

// captureResponse buffers a handler's response so an idempotent create can be
// stored and replayed. The responses involved are small JSON bodies.
type captureResponse struct {
	header http.Header
	status int
	body   bytes.Buffer
}

func (c *captureResponse) Header() http.Header {
	if c.header == nil {
		c.header = http.Header{}
	}
	return c.header
}

func (c *captureResponse) WriteHeader(status int) {
	if c.status == 0 {
		c.status = status
	}
}

func (c *captureResponse) Write(b []byte) (int, error) {
	if c.status == 0 {
		c.status = http.StatusOK
	}
	return c.body.Write(b)
}

// withIdempotency wraps an expensive create so a repeated request carrying the
// same Idempotency-Key replays the first response instead of doing the work
// again. No header means normal behavior. Only successful (2xx) responses are
// stored, so a failed attempt can still be retried.
func (s *Server) withIdempotency(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		key := r.Header.Get("Idempotency-Key")
		if key == "" {
			next(w, r)
			return
		}
		// Scope the key to the caller + method + path so keys never collide across
		// users or routes.
		who := ""
		if c := auth.FromContext(r.Context()); c != nil {
			who = c.UserID
		}
		full := who + " " + r.Method + " " + r.URL.Path + " " + key

		if status, body, ok := s.st.GetIdempotent(full); ok {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Idempotent-Replay", "true")
			w.WriteHeader(status)
			_, _ = w.Write([]byte(body))
			return
		}

		rec := &captureResponse{}
		next(rec, r)
		if rec.status == 0 {
			rec.status = http.StatusOK
		}
		if rec.status >= 200 && rec.status < 300 {
			_ = s.st.SaveIdempotent(full, r.Method, r.URL.Path, rec.status, rec.body.String(), idempotencyTTL)
		}
		for k, vs := range rec.Header() {
			for _, v := range vs {
				w.Header().Add(k, v)
			}
		}
		w.WriteHeader(rec.status)
		_, _ = w.Write(rec.body.Bytes())
	}
}
