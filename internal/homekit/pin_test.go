package homekit

import (
	"path/filepath"
	"testing"

	"github.com/TheOriUHD/aperture/internal/store"
)

// TestSetupPinIsValidAndStable pins the fix for "insecure pin 12345678": the
// bridge could never start because the hard-coded pairing code is on Apple's
// forbidden list. The pin must now be generated per install, never forbidden,
// and stable across calls (a changing code would break an existing pairing).
func TestSetupPinIsValidAndStable(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "hk.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()

	pin := SetupPin(st)
	if len(pin) != 8 {
		t.Fatalf("pin must be 8 digits, got %q", pin)
	}
	if forbiddenPins[pin] {
		t.Fatalf("generated a pin Apple rejects: %q", pin)
	}
	for _, c := range pin {
		if c < '0' || c > '9' {
			t.Fatalf("pin must be digits only, got %q", pin)
		}
	}
	if again := SetupPin(st); again != pin {
		t.Errorf("pin must be stable across calls: %q then %q", pin, again)
	}

	// A previously-stored forbidden pin is replaced rather than reused.
	if err := st.SetSetting(setupPinSetting, "12345678"); err != nil {
		t.Fatal(err)
	}
	if p := SetupPin(st); p == "12345678" {
		t.Error("a stored forbidden pin must be regenerated, not returned")
	}
}

// TestRandomPinNeverForbidden exercises the generator directly.
func TestRandomPinNeverForbidden(t *testing.T) {
	for i := 0; i < 500; i++ {
		if p := randomPin(); forbiddenPins[p] || len(p) != 8 {
			t.Fatalf("randomPin produced %q", p)
		}
	}
}
