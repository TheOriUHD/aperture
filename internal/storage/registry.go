package storage

import (
	"errors"
	"fmt"
	"sync"
)

// ErrUnsupported is returned while a backend type has no implementation compiled
// in yet (network backends land in a later pass).
var ErrUnsupported = errors.New("storage: backend type not available yet")

// SpecFunc resolves a location id to its decrypted Spec. The application provides
// it so the registry needn't import store or secret (avoids an import cycle).
type SpecFunc func(locationID string) (*Spec, error)

// Registry builds and caches one Backend per storage location, sharing it across
// the recorder/uploader, janitor and API. It is safe for concurrent use.
type Registry struct {
	specFor SpecFunc
	mu      sync.Mutex
	cache   map[string]Backend
}

// NewRegistry constructs a registry backed by specFor.
func NewRegistry(specFor SpecFunc) *Registry {
	return &Registry{specFor: specFor, cache: map[string]Backend{}}
}

// For returns a cached (or freshly built) backend for a location id.
func (r *Registry) For(locationID string) (Backend, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if b, ok := r.cache[locationID]; ok {
		return b, nil
	}
	spec, err := r.specFor(locationID)
	if err != nil {
		return nil, err
	}
	b, err := build(spec)
	if err != nil {
		return nil, err
	}
	r.cache[locationID] = b
	return b, nil
}

// Invalidate drops and closes a cached backend so the next For rebuilds it — call
// after a location's config or credentials change.
func (r *Registry) Invalidate(locationID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if b, ok := r.cache[locationID]; ok {
		_ = b.Close()
		delete(r.cache, locationID)
	}
}

// Close tears down every cached backend.
func (r *Registry) Close() {
	r.mu.Lock()
	defer r.mu.Unlock()
	for id, b := range r.cache {
		_ = b.Close()
		delete(r.cache, id)
	}
}

func build(spec *Spec) (Backend, error) {
	switch spec.Type {
	case TypeLocal, "":
		return newLocal(cfgStr(spec.Config, "path")), nil
	case TypeSMB:
		return newSMB(spec)
	case TypeSFTP:
		return newSFTP(spec)
	default:
		return nil, fmt.Errorf("storage: unknown backend type %q", spec.Type)
	}
}
