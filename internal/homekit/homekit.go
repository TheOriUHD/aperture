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
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"path/filepath"
	"sync"
	"time"

	"log/slog"

	"github.com/brutella/hap"
	"github.com/brutella/hap/accessory"
	"github.com/brutella/hap/characteristic"

	"github.com/TheOriUHD/aperture/internal/config"
	"github.com/TheOriUHD/aperture/internal/store"
)

// setupPinSetting stores this install's HomeKit pairing code. HomeKit requires a
// fixed 8-digit pin, and Apple REJECTS a list of trivial ones (all-same digits,
// 12345678, 87654321) — the hap library refuses to start with "insecure pin".
// The pin was previously the hard-coded 12345678, which is on that list, so the
// bridge could never actually start. It is now generated once per install and
// persisted, which also stops every Aperture sharing one pairing code.
const setupPinSetting = "homekit.pin"

// forbiddenPins are the codes Apple/hap reject outright.
var forbiddenPins = map[string]bool{
	"00000000": true, "11111111": true, "22222222": true, "33333333": true,
	"44444444": true, "55555555": true, "66666666": true, "77777777": true,
	"88888888": true, "99999999": true, "12345678": true, "87654321": true,
}

// SetupPin returns this install's pairing code, generating and persisting one on
// first use. Shown in the UI (formatted 123-45-678) so the operator can pair.
func SetupPin(st *store.Store) string {
	if v, err := st.GetSetting(setupPinSetting); err == nil && len(v) == 8 && !forbiddenPins[v] {
		return v
	}
	pin := randomPin()
	_ = st.SetSetting(setupPinSetting, pin)
	return pin
}

func randomPin() string {
	for {
		var b [4]byte
		if _, err := rand.Read(b[:]); err != nil {
			return "83719264" // deterministic fallback; still not a forbidden pin
		}
		n := binary.BigEndian.Uint32(b[:]) % 100_000_000
		pin := fmt.Sprintf("%08d", n)
		if !forbiddenPins[pin] {
			return pin
		}
	}
}

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

	// onExit reports an unexpected server exit to the supervisor.
	onExit func(error)

	mu        sync.Mutex
	armed     bool
	motionGen map[string]int64 // latest motion generation per camera (sliding clear-timer)
	onArmed   func(bool)       // change-feed alarm.state hook (Item 3)
}

// SetOnArmed registers a callback invoked when the HomeKit Home app toggles the
// alarm, so the change feed emits alarm.state for HomeKit-driven arms.
func (b *Bridge) SetOnArmed(fn func(bool)) { b.onArmed = fn }

// New builds the bridge and its accessories from the current camera list.
// It does not start listening — call Run for that. Zero enabled cameras is
// handled gracefully (the bridge still exposes the SecuritySystem).
func New(cfg *config.Config, st *store.Store, log *slog.Logger) (*Bridge, error) {
	b := &Bridge{
		cfg:       cfg,
		st:        st,
		log:       log,
		motion:    make(map[string]*accessory.MotionSensor),
		motionGen: make(map[string]int64),
	}

	// Restore persisted armed state. Default to armed unless explicitly "false",
	// matching the notify engine and the HA arm switch (unset == armed).
	if v, _ := st.GetSetting("armed"); v != "false" {
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
	pin := SetupPin(st)
	server.Pin = pin
	// Bind to the same interface as the HTTP server's port is irrelevant;
	// hap picks its own TCP port. Leaving Addr empty lets hap choose.
	b.server = server

	log.Info("homekit bridge configured",
		"cameras", len(b.motion), "pin", pin, "store", filepath.Join(cfg.DataDir, "homekit"))
	return b, nil
}

// Run starts the HomeKit server and blocks until ctx is cancelled. It performs
// its own graceful shutdown when ctx is done (hap.Server.ListenAndServe
// returns nil on context cancellation).
func (b *Bridge) Run(ctx context.Context) {
	b.log.Info("homekit bridge starting", "pin", SetupPin(b.st))
	go b.syncArmedLoop(ctx) // reflect HA/notify-driven arm changes into the Home app
	err := b.server.ListenAndServe(ctx)
	if err != nil && ctx.Err() == nil {
		b.log.Error("homekit server exited", "err", err)
	}
	// Tell the supervisor the bridge is down, so a server that dies on its own
	// (bad pin, port clash) reports as stopped instead of a false green light.
	if b.onExit != nil && ctx.Err() == nil {
		b.onExit(err)
	}
}

// SetOnExit registers a callback fired when Run returns other than by
// cancellation — i.e. the bridge failed or died.
func (b *Bridge) SetOnExit(fn func(error)) { b.onExit = fn }

// syncArmedLoop keeps the SecuritySystem characteristic in step with the "armed"
// setting, which other surfaces (Home Assistant, notify) can change directly.
func (b *Bridge) syncArmedLoop(ctx context.Context) {
	t := time.NewTicker(5 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			v, _ := b.st.GetSetting("armed")
			armed := v != "false"
			b.mu.Lock()
			changed := armed != b.armed
			b.armed = armed
			b.mu.Unlock()
			if changed {
				b.applyArmedToCharacteristics()
			}
		}
	}
}

// SetMotion flips the MotionSensor characteristic for the given camera. Safe to
// call for unknown/disabled camera IDs (no-op).
func (b *Bridge) SetMotion(cameraID string, active bool) {
	ms, ok := b.motion[cameraID]
	if !ok {
		return
	}
	ms.MotionSensor.MotionDetected.SetValue(active)
}

// MotionPulse marks a camera in motion and auto-clears 20s after the LATEST
// event (a sliding window), so overlapping events can't cut the sensor short.
// This is what the event pipeline calls.
func (b *Bridge) MotionPulse(cameraID string) {
	if _, ok := b.motion[cameraID]; !ok {
		return
	}
	b.SetMotion(cameraID, true)
	b.mu.Lock()
	b.motionGen[cameraID]++
	gen := b.motionGen[cameraID]
	b.mu.Unlock()
	time.AfterFunc(20*time.Second, func() {
		b.mu.Lock()
		latest := b.motionGen[cameraID] == gen
		b.mu.Unlock()
		if latest {
			b.SetMotion(cameraID, false)
		}
	})
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
	if b.onArmed != nil {
		b.onArmed(armed) // change feed alarm.state (Item 3). Only the Home-app
		// toggle reaches setArmed; the resync loop updates b.armed inline, so no
		// double-emit for HA-driven changes.
	}
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
