package main

import (
	"context"
	"log/slog"
	"sync"

	"github.com/TheOriUHD/aperture/internal/config"
	"github.com/TheOriUHD/aperture/internal/homekit"
	"github.com/TheOriUHD/aperture/internal/onvif"
	"github.com/TheOriUHD/aperture/internal/store"
)

// ONVIF and HomeKit are optional interop surfaces the operator toggles from the
// UI. Both are blocking Run(ctx) services, so each is wrapped here in a tiny
// supervisor: enabling spawns Run under a child context, disabling cancels it.
// No restart, and Running() reports what is ACTUALLY up — a subsystem that
// failed to start reads as not running rather than showing a false green light.

// onvifService toggles the ONVIF Profile S provider.
type onvifService struct {
	base context.Context
	cfg  *config.Config
	st   *store.Store
	log  *slog.Logger

	mu     sync.Mutex
	cancel context.CancelFunc
	srv    *onvif.Server
}

func newONVIFService(base context.Context, cfg *config.Config, st *store.Store, log *slog.Logger) *onvifService {
	return &onvifService{base: base, cfg: cfg, st: st, log: log}
}

func (o *onvifService) SetEnabled(on bool) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	if on == (o.cancel != nil) {
		return nil // already in the requested state
	}
	if !on {
		o.cancel()
		o.cancel = nil
		o.srv = nil
		o.log.Info("onvif stopped by operator")
		return nil
	}
	ctx, cancel := context.WithCancel(o.base)
	srv := onvif.New(o.cfg, o.st, o.log)
	o.cancel, o.srv = cancel, srv
	go srv.Run(ctx)
	return nil
}

func (o *onvifService) Running() bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.cancel != nil
}

func (o *onvifService) Info() map[string]any {
	addr := o.cfg.ONVIFAddr
	if addr == "" {
		addr = ":10101"
	}
	return map[string]any{"addr": addr}
}

// homekitService toggles the HomeKit bridge. The bridge enumerates cameras at
// construction, so it is rebuilt on each enable and therefore always reflects
// the current camera list.
type homekitService struct {
	base    context.Context
	cfg     *config.Config
	st      *store.Store
	log     *slog.Logger
	onArmed func(bool)

	mu     sync.Mutex
	cancel context.CancelFunc
	bridge *homekit.Bridge
}

func newHomeKitService(base context.Context, cfg *config.Config, st *store.Store, log *slog.Logger, onArmed func(bool)) *homekitService {
	return &homekitService{base: base, cfg: cfg, st: st, log: log, onArmed: onArmed}
}

func (h *homekitService) SetEnabled(on bool) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if on == (h.cancel != nil) {
		return nil
	}
	if !on {
		h.cancel()
		h.cancel = nil
		h.bridge = nil
		h.log.Info("homekit stopped by operator")
		return nil
	}
	b, err := homekit.New(h.cfg, h.st, h.log)
	if err != nil {
		return err // surfaced to the UI; nothing is marked enabled
	}
	if h.onArmed != nil {
		b.SetOnArmed(h.onArmed)
	}
	ctx, cancel := context.WithCancel(h.base)
	h.cancel, h.bridge = cancel, b
	go b.Run(ctx)
	return nil
}

func (h *homekitService) Running() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.cancel != nil
}

// Bridge returns the running bridge (nil when stopped), so motion plumbing can
// address it without holding its own reference.
func (h *homekitService) Bridge() *homekit.Bridge {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.bridge
}

func (h *homekitService) Info() map[string]any {
	// The pairing code is a fixed constant (documented caveat) and the operator
	// needs it to add the bridge in the Home app, so the UI shows it.
	return map[string]any{"pin": homekit.SetupPin}
}
