package reolink

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const loginOK = `[{"cmd":"Login","code":0,"value":{"Token":{"name":"tok","leaseTime":3600}}}]`

func hostOf(u string) string {
	return strings.TrimPrefix(strings.TrimPrefix(u, "https://"), "http://")
}

// A camera's TLS certificate can never validate — self-signed, for a LAN address,
// with no IP SANs. httptest.NewTLSServer serves exactly that kind of certificate, so
// this test fails outright if the client ever goes back to verifying it, which is
// what made the spotlight controls silently unavailable.
func TestAcceptsSelfSignedCameraCertificate(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.RawQuery, "Login") {
			_, _ = w.Write([]byte(loginOK))
			return
		}
		_, _ = w.Write([]byte(`[{"cmd":"GetWhiteLed","code":0,"value":{"WhiteLed":{"state":1,"mode":2,"bright":80}}}]`))
	}))
	defer srv.Close()

	c := New(hostOf(srv.URL), "u", "p", 5*time.Second)
	sp, err := c.GetSpotlight(context.Background())
	if err != nil {
		t.Fatalf("GetSpotlight over a self-signed cert: %v", err)
	}
	if sp.State != 1 || sp.Bright != 80 {
		t.Fatalf("spotlight = %+v", sp)
	}
	if c.scheme != "https" {
		t.Errorf("scheme = %q, want https", c.scheme)
	}
}

// Older firmware serves the API over plain http. The client must find it there too,
// and must remember which scheme answered so the fallback costs one request, once.
func TestFallsBackToPlainHTTP(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.RawQuery, "Login") {
			_, _ = w.Write([]byte(loginOK))
			return
		}
		_, _ = w.Write([]byte(`[{"cmd":"GetIrLights","code":0,"value":{"IrLights":{"state":"Auto"}}}]`))
	}))
	defer srv.Close()

	c := New(hostOf(srv.URL), "u", "p", 5*time.Second)
	if _, err := c.GetIR(context.Background()); err != nil {
		t.Fatalf("GetIR over http: %v", err)
	}
	if c.scheme != "http" {
		t.Fatalf("scheme = %q, want http", c.scheme)
	}
	before := hits
	if _, err := c.GetIR(context.Background()); err != nil {
		t.Fatalf("second GetIR: %v", err)
	}
	// One call on the remembered scheme; no https probe, no re-login (token cached).
	if hits-before != 1 {
		t.Errorf("second call made %d requests, want 1 — the scheme is not being remembered", hits-before)
	}
}

// Current firmware answers http with a redirect to https. Go would turn a redirected
// POST into a GET, so every command would break while looking like a malformed
// reply. A redirect must be treated as "wrong scheme", never followed.
func TestRedirectIsNotFollowed(t *testing.T) {
	var methods []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		methods = append(methods, r.Method)
		http.Redirect(w, r, "https://elsewhere.invalid/cgi-bin/api.cgi", http.StatusFound)
	}))
	defer srv.Close()

	c := New(hostOf(srv.URL), "u", "p", 3*time.Second)
	_, err := c.GetSpotlight(context.Background())
	if err == nil {
		t.Fatal("expected an error when both schemes fail")
	}
	for _, m := range methods {
		if m != http.MethodPost {
			t.Fatalf("a redirect was followed and downgraded the request to %s", m)
		}
	}
	if strings.Contains(err.Error(), "unexpected response") {
		t.Errorf("a redirect should not be reported as a malformed reply: %v", err)
	}
}
