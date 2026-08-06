package main

import "testing"

// TestApiV1 covers the Phase 5.3 path upgrade: legacy /api paths become /api/v1,
// idempotently, and non-/api paths are untouched.
func TestApiV1(t *testing.T) {
	cases := map[string]string{
		"/api/cameras":     "/api/v1/cameras",
		"/api/auth/login":  "/api/v1/auth/login",
		"/api/v1/cameras":  "/api/v1/cameras", // already v1 — idempotent
		"/health":          "/health",         // non-/api — untouched
		"/api/v1/auth/me":  "/api/v1/auth/me",
	}
	for in, want := range cases {
		if got := apiV1(in); got != want {
			t.Errorf("apiV1(%q) = %q, want %q", in, got, want)
		}
	}
}
