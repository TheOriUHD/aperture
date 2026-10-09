package api

import (
	"errors"
	"strings"
	"testing"

	"github.com/TheOriUHD/aperture/internal/reolink"
)

// A camera that refuses and a camera that cannot be reached need different fixes, so
// they must not be reported the same way. A refused spotlight used to surface as
// "unreachable" — and the UI then dropped even that.
func TestVendorFailureSeparatesRefusalFromUnreachable(t *testing.T) {
	code, msg := vendorFailure("turn the spotlight on", &reolink.APIError{Cmd: "SetWhiteLed", Code: -26, Detail: "ability error"})
	if code != "camera.refused" {
		t.Errorf("refusal code = %q, want camera.refused", code)
	}
	for _, want := range []string{"turn the spotlight on", "not allowed", `"ability error"`} {
		if !strings.Contains(msg, want) {
			t.Errorf("refusal message %q lacks %q", msg, want)
		}
	}

	code, msg = vendorFailure("turn the spotlight on", errors.New("dial tcp 192.168.10.38:443: i/o timeout"))
	if code != "unreachable" || !strings.Contains(msg, "i/o timeout") {
		t.Errorf("transport failure: code=%q msg=%q", code, msg)
	}
}
