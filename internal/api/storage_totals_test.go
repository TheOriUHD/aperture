package api

import (
	"testing"

	"github.com/TheOriUHD/aperture/internal/store"
)

// TestStorageTotals pins the fix for the global storage display: usage has always
// been counted across ALL destinations, but the allocation shown next to it was the
// default (local) location only — so once any camera recorded to a network share the
// UI read as far over 100%. storageTotals sums allocation across every ENABLED
// destination; a disabled one contributes nothing, and 0 anywhere means unlimited.
func TestStorageTotals(t *testing.T) {
	s := newTestServer(t)
	mustDo(t, s.st.SetSetting("storage_cap_bytes", "1000"))

	// Local only.
	if got, _, _ := s.storageTotals(); got != 1000 {
		t.Errorf("local only: allocated = %d, want 1000", got)
	}

	// A network location adds its allocation to the total.
	nas := &store.StorageLocation{Name: "NAS", Type: "smb", AllocationBytes: 4000, Enabled: true}
	mustDo(t, s.st.CreateStorageLocation(nas))
	if got, _, _ := s.storageTotals(); got != 5000 {
		t.Errorf("local+NAS: allocated = %d, want 5000", got)
	}

	// A DISABLED destination must not count toward capacity.
	off := &store.StorageLocation{Name: "Off", Type: "sftp", AllocationBytes: 9000, Enabled: false}
	mustDo(t, s.st.CreateStorageLocation(off))
	if got, _, _ := s.storageTotals(); got != 5000 {
		t.Errorf("disabled location must not count: allocated = %d, want 5000", got)
	}

	// 0 on any enabled destination = unlimited overall, reported as 0 (existing
	// convention) rather than silently undercounting.
	unl := &store.StorageLocation{Name: "Unl", Type: "smb", AllocationBytes: 0, Enabled: true}
	mustDo(t, s.st.CreateStorageLocation(unl))
	got, used, free := s.storageTotals()
	if got != 0 || free != 0 {
		t.Errorf("unlimited: allocated = %d free = %d, want 0/0", got, free)
	}
	if used < 0 {
		t.Errorf("used should never be negative, got %d", used)
	}
}
