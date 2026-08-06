package api

import (
	"testing"

	"github.com/TheOriUHD/aperture/internal/ext"
)

// TestExtScopesAreKnown pins the extension manifest→scope mapping (internal/ext,
// which cannot import this package) against the authoritative knownScopes set:
// every scope a manifest can be granted must be a real, assignable scope the
// enforce layer understands. If someone adds a permission mapping in ext without a
// matching scope here (or renames a scope), this fails instead of minting an
// extension token the server can never satisfy.
func TestExtScopesAreKnown(t *testing.T) {
	for _, s := range ext.AllMappableScopes() {
		if !knownScopes[s] {
			t.Errorf("ext maps to scope %q which is not in knownScopes", s)
		}
	}
}
