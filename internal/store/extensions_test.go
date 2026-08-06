package store

import (
	"path/filepath"
	"testing"
)

func TestExtensionCRUD(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer st.Close()

	e := &Extension{
		ID: "com.example.x", Name: "X", Version: "1.0.0", Source: "sideload",
		UpdateURL: "https://example.com/x/manifest.json", ManifestJSON: `{"manifest":1,"id":"com.example.x"}`,
		Scopes: []string{"cameras:read", "events:read"}, CameraGrants: []string{"cam1"},
		Enabled: false, ExtUID: 65540, InstalledBy: "u1",
	}
	if err := st.CreateExtension(e); err != nil {
		t.Fatalf("create: %v", err)
	}

	got, err := st.GetExtension("com.example.x")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Name != "X" || got.Version != "1.0.0" || got.Source != "sideload" || got.UpdateURL == "" {
		t.Fatalf("scalar fields: %+v", got)
	}
	if len(got.Scopes) != 2 || got.Scopes[0] != "cameras:read" || got.Scopes[1] != "events:read" {
		t.Fatalf("scopes round-trip: %v", got.Scopes)
	}
	if len(got.CameraGrants) != 1 || got.CameraGrants[0] != "cam1" {
		t.Fatalf("grants round-trip: %v", got.CameraGrants)
	}
	if got.Enabled {
		t.Fatal("should be disabled on install")
	}
	if got.ExtUID != 65540 || got.InstalledBy != "u1" || got.InstalledAt == 0 || got.UpdatedAt == 0 {
		t.Fatalf("uid/owner/timestamps: %+v", got)
	}

	// enable flips the flag
	if err := st.SetExtensionEnabled("com.example.x", true); err != nil {
		t.Fatalf("enable: %v", err)
	}
	if got, _ = st.GetExtension("com.example.x"); !got.Enabled {
		t.Fatal("should be enabled after SetExtensionEnabled(true)")
	}

	// update replaces mutable fields, keeps identity/uid/installed_by
	e.Version = "1.1.0"
	e.Scopes = []string{"cameras:read"}
	if err := st.UpdateExtension(e); err != nil {
		t.Fatalf("update: %v", err)
	}
	got, _ = st.GetExtension("com.example.x")
	if got.Version != "1.1.0" || len(got.Scopes) != 1 {
		t.Fatalf("update didn't take: %+v", got)
	}
	if got.ExtUID != 65540 || got.InstalledBy != "u1" || !got.Enabled {
		t.Fatalf("update clobbered preserved fields: %+v", got)
	}

	list, err := st.ListExtensions()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("list len = %d, want 1", len(list))
	}

	if err := st.DeleteExtension("com.example.x"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := st.GetExtension("com.example.x"); err == nil {
		t.Fatal("get after delete should error")
	}
}
