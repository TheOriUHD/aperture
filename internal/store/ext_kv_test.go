package store

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestExtKV(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer st.Close()

	// set/get round-trip.
	if err := st.ExtKVSet("com.a", "ha_token", "secret-123"); err != nil {
		t.Fatalf("set: %v", err)
	}
	if v, ok, _ := st.ExtKVGet("com.a", "ha_token"); !ok || v != "secret-123" {
		t.Fatalf("get = %q,%v; want secret-123,true", v, ok)
	}

	// Namespace isolation: another extension cannot see com.a's key.
	if _, ok, _ := st.ExtKVGet("com.b", "ha_token"); ok {
		t.Error("com.b can read com.a's key — namespaces are not isolated")
	}
	// Same key name in a different namespace is independent.
	_ = st.ExtKVSet("com.b", "ha_token", "other")
	if v, _, _ := st.ExtKVGet("com.a", "ha_token"); v != "secret-123" {
		t.Errorf("com.b write leaked into com.a: got %q", v)
	}

	// Upsert replaces, doesn't accumulate.
	_ = st.ExtKVSet("com.a", "ha_token", "secret-456")
	if keys, _ := st.ExtKVKeys("com.a"); len(keys) != 1 || keys[0] != "ha_token" {
		t.Errorf("keys after upsert = %v; want [ha_token]", keys)
	}

	// Byte quota: a single oversized value is rejected.
	if err := st.ExtKVSet("com.a", "big", strings.Repeat("x", ExtKVMaxBytes+1)); err != ErrExtKVQuota {
		t.Errorf("oversized set err = %v; want ErrExtKVQuota", err)
	}

	// Row quota: fill to the cap, then the next distinct key is rejected.
	for i := 0; i < ExtKVMaxRows; i++ {
		if err := st.ExtKVSet("com.c", "k"+itoa(i), "v"); err != nil {
			t.Fatalf("fill row %d: %v", i, err)
		}
	}
	if err := st.ExtKVSet("com.c", "one-too-many", "v"); err != ErrExtKVQuota {
		t.Errorf("over-row-quota err = %v; want ErrExtKVQuota", err)
	}
	// ...but overwriting an existing key at the cap still works (no new row).
	if err := st.ExtKVSet("com.c", "k0", "v2"); err != nil {
		t.Errorf("overwrite at row cap should succeed, got %v", err)
	}

	// Uninstall wipes the whole namespace.
	if err := st.ExtKVDeleteAll("com.a"); err != nil {
		t.Fatalf("delete all: %v", err)
	}
	if keys, _ := st.ExtKVKeys("com.a"); len(keys) != 0 {
		t.Errorf("keys after delete-all = %v; want none", keys)
	}
	if keys, _ := st.ExtKVKeys("com.b"); len(keys) != 1 {
		t.Error("delete-all of com.a also cleared com.b")
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
