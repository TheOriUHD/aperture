package api

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"net/http"
	"testing"
	"time"
)

// TestReadDeadlineDoesNotLeakAcrossKeepAlive proves the per-request read deadline
// (set mid-request by limitBody) does NOT bleed into a later request on the same
// keep-alive connection — even when the idle gap between requests exceeds the
// deadline. Go's http server resets the read deadline between requests: after a
// request finishes it arms the IdleTimeout deadline for the idle wait, clears it
// once the next request's bytes arrive, then arms ReadHeaderTimeout at the start
// of reading that request. So a mid-request SetReadDeadline is always overwritten
// before the next request is read.
//
// The test drives ONE raw TCP connection deterministically (not the client
// pool): POST, idle > readBodyTimeout, POST again on the same socket. If the
// deadline leaked, the second read would fail with a timeout/EOF.
func TestReadDeadlineDoesNotLeakAcrossKeepAlive(t *testing.T) {
	s := newTestServer(t)
	srv := newRawHTTPServer(t, s)
	defer srv.close()

	// Shrink the read deadline so a short idle gap can exceed it. IdleTimeout on
	// the test server is left at Go's default (0 → falls back), so the reset we
	// rely on is ReadHeaderTimeout at the start of the next request; we set that
	// explicitly below to mirror production and give the idle wait a bound.
	old := readBodyTimeout
	readBodyTimeout = 150 * time.Millisecond
	defer func() { readBodyTimeout = old }()

	conn, err := net.Dial("tcp", srv.addr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	br := bufio.NewReader(conn)

	writePost := func() error {
		body := `{"email":"x","password":"y"}`
		_, err := fmt.Fprintf(conn,
			"POST /api/v1/auth/login HTTP/1.1\r\nHost: t\r\nContent-Type: application/json\r\nContent-Length: %d\r\nConnection: keep-alive\r\n\r\n%s",
			len(body), body)
		return err
	}
	readResp := func() error {
		conn.SetReadDeadline(time.Now().Add(5 * time.Second)) // client-side guard only
		resp, err := http.ReadResponse(br, &http.Request{Method: "POST"})
		if err != nil {
			return err
		}
		_, _ = io.Copy(io.Discard, resp.Body) // drain so the reader is positioned for the next response
		return resp.Body.Close()
	}

	// Request 1 — opens the connection; limitBody arms the 150ms read deadline
	// while reading the body.
	if err := writePost(); err != nil {
		t.Fatalf("write POST 1: %v", err)
	}
	if err := readResp(); err != nil {
		t.Fatalf("read POST 1 response: %v", err)
	}

	// Idle well beyond the 150ms read deadline.
	time.Sleep(400 * time.Millisecond)

	// Request 2 on the SAME socket. If request 1's deadline leaked, the server
	// would have closed the connection and this read fails.
	if err := writePost(); err != nil {
		t.Fatalf("write POST 2 (same keep-alive conn): %v — connection was dropped", err)
	}
	if err := readResp(); err != nil {
		t.Fatalf("read POST 2 after a >deadline idle gap: %v — the read deadline leaked into keep-alive", err)
	}
}

// rawServer is a minimal HTTP server on a real TCP listener, configured like
// production (ReadHeaderTimeout + IdleTimeout) so the deadline-reset path under
// test matches the real one.
type rawServer struct {
	ln   net.Listener
	http *http.Server
	addr string
}

func newRawHTTPServer(t *testing.T, h http.Handler) *rawServer {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	srv := &http.Server{
		Handler:           h,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second, // same as cmd/nvrd
	}
	go func() { _ = srv.Serve(ln) }()
	return &rawServer{ln: ln, http: srv, addr: ln.Addr().String()}
}

func (r *rawServer) close() {
	_ = r.http.Close()
	_ = r.ln.Close()
}
