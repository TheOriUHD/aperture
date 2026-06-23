// Package oidc adds optional OpenID Connect / SSO login on top of local
// accounts. Configuration lives in the settings store; the provider is
// discovered lazily and rebuilt when settings change.
package oidc

import (
	"context"
	"fmt"
	"strings"
	"sync"

	gooidc "github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"

	"github.com/TheOriUHD/aperture/internal/config"
	"github.com/TheOriUHD/aperture/internal/store"
)

// Settings is the resolved OIDC configuration.
type Settings struct {
	Enabled       bool
	Issuer        string
	ClientID      string
	ClientSecret  string
	AllowedDomain string
	AutoCreate    bool
	DisplayName   string
}

// Authenticator drives the OIDC login flow.
type Authenticator struct {
	cfg *config.Config
	st  *store.Store

	mu       sync.Mutex
	current  Settings
	provider *gooidc.Provider
	verifier *gooidc.IDTokenVerifier
	oauth    *oauth2.Config
}

// New constructs an Authenticator.
func New(cfg *config.Config, st *store.Store) *Authenticator {
	return &Authenticator{cfg: cfg, st: st}
}

// LoadSettings reads OIDC settings from the store.
func (a *Authenticator) LoadSettings() Settings {
	get := func(k, def string) string {
		if v, err := a.st.GetSetting(k); err == nil && v != "" {
			return v
		}
		return def
	}
	return Settings{
		Enabled:       get("oidc_enabled", "false") == "true",
		Issuer:        get("oidc_issuer", ""),
		ClientID:      get("oidc_client_id", ""),
		ClientSecret:  get("oidc_client_secret", ""),
		AllowedDomain: strings.ToLower(get("oidc_domain", "")),
		AutoCreate:    get("oidc_autocreate", "false") == "true",
		DisplayName:   get("oidc_name", "Single sign-on"),
	}
}

// RedirectURL is the callback URL the provider must be configured with.
func (a *Authenticator) RedirectURL() string {
	return strings.TrimRight(a.cfg.PublicURL, "/") + "/api/auth/oidc/callback"
}

func (a *Authenticator) Enabled() bool { return a.LoadSettings().Enabled }
func (a *Authenticator) DisplayName() string {
	if n := a.LoadSettings().DisplayName; n != "" {
		return n
	}
	return "Single sign-on"
}

// ensure builds the provider/verifier/oauth config, rebuilding on config change.
func (a *Authenticator) ensure(ctx context.Context) error {
	s := a.LoadSettings()
	if !s.Enabled || s.Issuer == "" || s.ClientID == "" {
		return fmt.Errorf("oidc not configured")
	}
	a.mu.Lock()
	ready := a.provider != nil && s == a.current
	a.mu.Unlock()
	if ready {
		return nil
	}

	provider, err := gooidc.NewProvider(ctx, s.Issuer)
	if err != nil {
		return fmt.Errorf("oidc discovery: %w", err)
	}
	a.mu.Lock()
	a.provider = provider
	a.verifier = provider.Verifier(&gooidc.Config{ClientID: s.ClientID})
	a.oauth = &oauth2.Config{
		ClientID:     s.ClientID,
		ClientSecret: s.ClientSecret,
		Endpoint:     provider.Endpoint(),
		RedirectURL:  a.RedirectURL(),
		Scopes:       []string{gooidc.ScopeOpenID, "email", "profile"},
	}
	a.current = s
	a.mu.Unlock()
	return nil
}

// AuthCodeURL returns the provider authorization URL for the given state.
func (a *Authenticator) AuthCodeURL(ctx context.Context, state string) (string, error) {
	if err := a.ensure(ctx); err != nil {
		return "", err
	}
	a.mu.Lock()
	o := a.oauth
	a.mu.Unlock()
	return o.AuthCodeURL(state), nil
}

// Claims are the identity fields extracted from the verified ID token.
type Claims struct {
	Email string
	Sub   string
	Name  string
}

// Exchange completes the flow and returns the verified identity.
func (a *Authenticator) Exchange(ctx context.Context, code string) (*Claims, error) {
	if err := a.ensure(ctx); err != nil {
		return nil, err
	}
	a.mu.Lock()
	o, v := a.oauth, a.verifier
	a.mu.Unlock()

	tok, err := o.Exchange(ctx, code)
	if err != nil {
		return nil, fmt.Errorf("token exchange: %w", err)
	}
	rawID, ok := tok.Extra("id_token").(string)
	if !ok {
		return nil, fmt.Errorf("no id_token in response")
	}
	idTok, err := v.Verify(ctx, rawID)
	if err != nil {
		return nil, fmt.Errorf("verify id_token: %w", err)
	}
	var c struct {
		Email             string `json:"email"`
		Name              string `json:"name"`
		PreferredUsername string `json:"preferred_username"`
	}
	if err := idTok.Claims(&c); err != nil {
		return nil, err
	}
	name := c.Name
	if name == "" {
		name = c.PreferredUsername
	}
	return &Claims{Email: strings.ToLower(c.Email), Sub: idTok.Subject, Name: name}, nil
}
