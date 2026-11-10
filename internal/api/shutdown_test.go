package api

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/TheOriUHD/aperture/internal/store"
)

// TestShutdownEndsEventStreams is the restart regression: every open tab of the
// web app holds /api/v1/feed and /api/v1/notify/stream open, and those handlers
// only returned when their request context ended — which http.Server.Shutdown
// never does. Shutdown therefore waited out its whole deadline on every restart
// ("graceful shutdown" err="context deadline exceeded"). The streams must end as
// soon as Shutdown starts, cleanly, so the browser's EventSource reconnects to
// the next instance.
func TestShutdownEndsEventStreams(t *testing.T) {
	for _, path := range []string{"/api/v1/feed", "/api/v1/notify/stream"} {
		t.Run(path, func(t *testing.T) {
			s := newTestServer(t)
			admin := &store.User{ID: "a1", Email: "a@t", Name: "A", Role: "admin"}
			mustDo(t, s.st.CreateUser(admin))
			adminC := cred{"admin", cookieCred(sessionCookieFor(t, s, admin))}

			ln, err := net.Listen("tcp", "127.0.0.1:0")
			mustDo(t, err)
			hs := s.NewHTTPServer("") // the same wiring main uses
			served := make(chan error, 1)
			go func() { served <- hs.Serve(ln) }()

			req, err := http.NewRequest("GET", "http://"+ln.Addr().String()+path, nil)
			mustDo(t, err)
			adminC.apply(req)
			resp, err := http.DefaultClient.Do(req)
			mustDo(t, err)
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("open %s: status %d", path, resp.StatusCode)
			}
			body := bufio.NewReader(resp.Body)
			if line, err := body.ReadString('\n'); err != nil || !strings.HasPrefix(line, ": connected") {
				t.Fatalf("stream did not open: %q, %v", line, err)
			}

			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			start := time.Now()
			if err := hs.Shutdown(ctx); err != nil {
				t.Fatalf("Shutdown with an open %s stream: %v (after %s)", path, err, time.Since(start).Round(time.Millisecond))
			}
			if err := <-served; !errors.Is(err, http.ErrServerClosed) {
				t.Fatalf("Serve returned %v", err)
			}
			// The response ends with a proper chunked terminator rather than a cut
			// connection: that is what the client's EventSource treats as "reconnect".
			if _, err := io.ReadAll(body); err != nil {
				t.Fatalf("stream was cut instead of ended: %v", err)
			}
		})
	}
}
