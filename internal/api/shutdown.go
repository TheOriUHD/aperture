package api

import (
	"net/http"
	"sync"
	"time"
)

// NewHTTPServer returns the http.Server that serves this API on addr, wired so
// that Shutdown can actually finish.
//
// http.Server.Shutdown waits for every active connection to go idle, and never
// cancels a request's context. The change feed and the notification stream run
// until their request context ends, and every open tab of the web app holds
// both, so before this hook each restart waited out the whole shutdown deadline
// and logged "graceful shutdown: context deadline exceeded". RegisterOnShutdown
// ends those streams the moment Shutdown starts; ordinary requests still finish.
func (s *Server) NewHTTPServer(addr string) *http.Server {
	hs := &http.Server{
		Addr:              addr,
		Handler:           s,
		ReadHeaderTimeout: 10 * time.Second, // slowloris guard on headers
		IdleTimeout:       120 * time.Second,
		// ReadTimeout and WriteTimeout are deliberately left 0: a write deadline
		// would kill SSE and long media exports, a read deadline slow large
		// uploads. Body size is bounded by limitBody (MaxBytesReader) instead.
	}
	hs.RegisterOnShutdown(s.CloseStreams)
	return hs
}

// CloseStreams ends every open event stream (/feed, /notify/stream) and makes
// any stream opened afterwards end at once. Each ends with a proper end of
// response, which a browser's EventSource treats as "reconnect", so the web app
// picks the next instance up by itself. Hijacked connections (the live-view
// WebSocket proxy) are not tracked by Shutdown and need nothing here.
func (s *Server) CloseStreams() { s.streams.close() }

// closeSignal is a channel closed once, made on first use so that its zero value
// works: tests build Server as a struct literal.
type closeSignal struct {
	initOnce, closeOnce sync.Once
	ch                  chan struct{}
}

func (c *closeSignal) done() <-chan struct{} {
	c.initOnce.Do(func() { c.ch = make(chan struct{}) })
	return c.ch
}

func (c *closeSignal) close() {
	c.done() // make the channel if nothing has asked for it yet
	c.closeOnce.Do(func() { close(c.ch) })
}
