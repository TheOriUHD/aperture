// Package homekit exposes Aperture cameras to Apple HomeKit as a bridge.
//
// Each enabled camera is published as a MotionSensor accessory. A single
// SecuritySystem accessory mirrors the operator "armed" flag (persisted via
// the settings store). The whole subsystem is OFF by default and only starts
// when cfg.HomeKitEnabled is true.
//
// HomeKit Secure Video (HKSV) — actual camera streaming into the Home app — is
// intentionally NOT implemented here; see the package caveats. MotionSensors
// give Home-app automations ("when X detects motion") without the substantial
// HKSV/RTP/SRTP machinery.
package homekit

import (
	"context"
	"path/filepath"
	"sync"

	"log/slog"

	"github.com/brutella/hap"
	"github.com/brutella/hap/accessory"
	"github.com/brutella/hap/characteristic"

	"github.com/TheOriUHD/aperture/internal/config"
	"github.com/TheOriUHD/aperture/internal/store"
)

// SetupPin is the HomeKit pairing code shown to the operator. HomeKit requires
// a fixed 8-digit pin; this one is a constant (documented as a caveat — it is
// not secret-random per install). Enter it in the Home app as "123-45-678".
const SetupPin = "12345678"

// Bridge is the running HomeKit subsystem. Construct with New, then call Run.
type Bridge struct {
	cfg *config.Config
	st  *store.Store
	log *slog.Logger

	server *hap.Server

	// motion maps camera ID -> its MotionSensor accessory so SetMotion can
	// flip the right characteristic. Read-only after New (no locking needed
	// for the map itself), but characteristic writes are goroutine-safe via hap.
	motion map[string]*accessory.MotionSensor

	sec *accessory.SecuritySystem

	mu    sync.Mutex
	armed bool
}

// New builds the bridge and its accessories from the current camera list.
// It does not start listening — call Run for that. Zero enabled cameras is
// handled gracefully (the bridge still exposes the SecuritySystem).
func New(cfg *config.Config, st *store.Store, log *slog.Logger) (*Bridge, error) {
	b := &Bridge{
		cfg:    cfg,
		st:     st,
		log:    log,
		motion: make(map[string]*accessory.MotionSensor),
	}

	// Restore persisted armed state.
	if v, err := st.GetSetting("armed"); err == nil && v == "true" {
		b.armed = true
	}

	cams, err := st.ListCameras()
	if err != nil {
		return nil, err
	}

	// Bridge accessory is the "root" that HomeKit pairs with; child
	// accessories hang off it.
	bridge := accessory.NewBridge(accessory.Info{
		Name:         "Aperture",
		Manufacturer: "Aperture NVR",
		Model:        "Aperture Bridge",
		SerialNumber: "aperture-bridge",
		Firmware:     "1.0",
	})

	var children []*accessory.A

	// SecuritySystem: arm/disarm mapped to the internal armed flag.
	b.sec = accessory.NewSecuritySystem(accessory.Info{
		Name:         "Aperture Security",
		Manufacturer: "Aperture NVR",
		Model:        "Aperture Security System",
		SerialNumber: "aperture-security",
		Firmware:     "1.0",
	})
	b.applyArmedToCharacteristics()
	// When the Home app changes the target state, persist + log.
	b.sec.SecuritySystem.SecuritySystemTargetState.OnValueRemoteUpdate(func(v int) {
		armed := v != characteristic.SecuritySystemTargetStateDisarm
		b.setArmed(armed, "homekit")
	})
	children = append(children, b.sec.A)

	// One MotionSensor per enabled camera.
	for _, cam := range cams {
		if !cam.Enabled {
			continue
		}
		ms := accessory.NewMotionSensor(accessory.Info{
			Name:         cam.Name,
			Manufacturer: "Aperture NVR",
			Model:        "Aperture Camera",
			SerialNumber: "cam-" + cam.ID,
			Firmware:     "1.0",
		})
		b.motion[cam.ID] = ms
		children = append(children, ms.A)
	}

	hapStore := hap.NewFsStore(filepath.Join(cfg.DataDir, "homekit"))
	server, err := hap.NewServer(hapStore, bridge.A, children...)
	if err != nil {
		return nil, err
	}
	server.Pin = SetupPin
	// Bind to the same interface as the HTTP server's port is irrelevant;
	// hap picks its own TCP port. Leaving Addr empty lets hap choose.
	b.server = server

	log.Info("homekit bridge configured",
		"cameras", len(b.motion), "pin", SetupPin, "store", filepath.Join(cfg.DataDir, "homekit"))
	return b, nil
}

// Run starts the HomeKit server and blocks until ctx is cancelled. It performs
// its own graceful shutdown when ctx is done (hap.Server.ListenAndServe
// returns nil on context cancellation).
func (b *Bridge) Run(ctx context.Context) {
	b.log.Info("homekit bridge starting", "pin", SetupPin)
	if err := b.server.ListenAndServe(ctx); err != nil && ctx.Err() == nil {
		b.log.Error("homekit server exited", "err", err)
	}
}

// SetMotion flips the MotionSensor characteristic for the given camera. Safe to
// call for unknown/disabled camera IDs (no-op). Called by the event pipeline.
func (b *Bridge) SetMotion(cameraID string, active bool) {
	ms, ok := b.motion[cameraID]
	if !ok {
		return
	}
	ms.MotionSensor.MotionDetected.SetValue(active)
}

// Armed reports the current armed state.
func (b *Bridge) Armed() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.armed
}

// setArmed updates the flag, persists it, mirrors it to the HomeKit
// characteristics, and logs. source is a short origin tag for the log line.
func (b *Bridge) setArmed(armed bool, source string) {
	b.mu.Lock()
	b.armed = armed
	b.mu.Unlock()

	val := "false"
	if armed {
		val = "true"
	}
	if err := b.st.SetSetting("armed", val); err != nil {
		b.log.Error("homekit persist armed failed", "err", err, "armed", armed)
	}
	b.applyArmedToCharacteristics()
	b.log.Info("homekit armed state changed", "armed", armed, "source", source)
}

// applyArmedToCharacteristics reflects b.armed into the SecuritySystem's
// current + target state characteristics (AwayArm when armed, Disarmed else).
func (b *Bridge) applyArmedToCharacteristics() {
	if b.sec == nil {
		return
	}
	b.mu.Lock()
	armed := b.armed
	b.mu.Unlock()

	if armed {
		_ = b.sec.SecuritySystem.SecuritySystemTargetState.SetValue(characteristic.SecuritySystemTargetStateAwayArm)
		_ = b.sec.SecuritySystem.SecuritySystemCurrentState.SetValue(characteristic.SecuritySystemCurrentStateAwayArm)
	} else {
		_ = b.sec.SecuritySystem.SecuritySystemTargetState.SetValue(characteristic.SecuritySystemTargetStateDisarm)
		_ = b.sec.SecuritySystem.SecuritySystemCurrentState.SetValue(characteristic.SecuritySystemCurrentStateDisarmed)
	}
}
