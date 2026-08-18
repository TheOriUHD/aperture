package api

import (
	"net/http"
	"strings"
	"testing"

	"github.com/TheOriUHD/aperture/internal/store"
)

// TestCameraDestinationsAPI pins the mirrored-destinations contract: read the
// membership set, replace it (mirror added with its own retention), reject a
// primary that isn't a member, reject unknown locations.
func TestCameraDestinationsAPI(t *testing.T) {
	s := newTestServer(t)
	admin := &store.User{ID: "a1", Email: "a@t", Name: "A", Role: store.RoleAdmin}
	mustDo(t, s.st.CreateUser(admin))
	sess := cred{name: "admin", apply: cookieCred(sessionCookieFor(t, s.am, admin))}
	mustDo(t, s.st.CreateCamera(&store.Camera{ID: "c1", Name: "Cam", RetentionDays: 7}))
	nas := &store.StorageLocation{Name: "NAS", Type: store.LocationSMB, Enabled: true}
	mustDo(t, s.st.CreateStorageLocation(nas))

	// Initial read: sole membership is the default, flagged primary.
	rec := s.reqAs("GET", "/api/v1/cameras/c1/destinations", sess)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"primary":true`) {
		t.Fatalf("GET destinations: %d %s", rec.Code, rec.Body.String())
	}

	// Add the NAS as a mirror keeping 14 days.
	body := `{"primaryLocationId":"","destinations":[{"locationId":"","retentionDays":0},{"locationId":"` + nas.ID + `","retentionDays":14}]}`
	rec = s.reqBody("PUT", "/api/v1/cameras/c1/destinations", sess, strings.NewReader(body))
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT destinations: %d %s", rec.Code, rec.Body.String())
	}
	mirrors, _ := s.st.MirrorDestinationsForCamera("c1")
	if len(mirrors) != 1 || mirrors[0].LocationID != nas.ID || mirrors[0].RetentionDays != 14 {
		t.Fatalf("mirror not persisted: %+v", mirrors)
	}

	// The primary must be one of the destinations.
	bad := `{"primaryLocationId":"nope","destinations":[{"locationId":"","retentionDays":0}]}`
	if rec := s.reqBody("PUT", "/api/v1/cameras/c1/destinations", sess, strings.NewReader(bad)); rec.Code != http.StatusBadRequest {
		t.Errorf("primary-not-member: want 400, got %d", rec.Code)
	}
	// Unknown locations are rejected.
	unk := `{"primaryLocationId":"","destinations":[{"locationId":"","retentionDays":0},{"locationId":"ghost","retentionDays":0}]}`
	if rec := s.reqBody("PUT", "/api/v1/cameras/c1/destinations", sess, strings.NewReader(unk)); rec.Code != http.StatusBadRequest {
		t.Errorf("unknown location: want 400, got %d", rec.Code)
	}
	// Empty set is rejected — a camera always records somewhere.
	empty := `{"primaryLocationId":"","destinations":[]}`
	if rec := s.reqBody("PUT", "/api/v1/cameras/c1/destinations", sess, strings.NewReader(empty)); rec.Code != http.StatusBadRequest {
		t.Errorf("empty destinations: want 400, got %d", rec.Code)
	}
}
