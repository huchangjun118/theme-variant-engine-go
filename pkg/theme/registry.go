package theme

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
)

// snapshot is an immutable view of all themes active at one instant. Readers
// always observe a single self-consistent version: a replacement publishes a
// freshly built snapshot in one pointer swap, so reads can never observe a
// mixture of old and new themes.
type snapshot struct {
	themes   map[string]*Theme
	defaults string
	epoch    uint64
	warnings map[string][]Warning
}

// Registry holds named themes and supports atomic, context-cancellable hot
// replacement. Validation and inheritance-chain resolution run WITHOUT
// blocking in-flight reads; the active snapshot only changes after the whole
// new set validated successfully. A cancelled or failed Publish therefore can
// never partially mutate or pollute the active themes.
type Registry struct {
	mu      sync.RWMutex
	current *snapshot
	epoch   atomic.Uint64

	maxCacheEntries int
}

// NewRegistry builds an empty registry.
func NewRegistry(opts ...RegistryOption) *Registry {
	cfg := registryConfig{maxCacheEntries: 256}
	for _, o := range opts {
		o(&cfg)
	}
	r := &Registry{maxCacheEntries: cfg.maxCacheEntries}
	r.current = &snapshot{
		themes:   map[string]*Theme{},
		warnings: map[string][]Warning{},
	}
	return r
}

// RegistryOption customises a Registry.
type RegistryOption func(*registryConfig)

type registryConfig struct {
	maxCacheEntries int
}

// WithCacheSize bounds the resolution result cache shared by an Engine
// created for this registry (default 256, 0 disables caching).
func WithCacheSize(n int) RegistryOption {
	return func(c *registryConfig) { c.maxCacheEntries = n }
}

// CacheSize reports the configured cache bound.
func (r *Registry) CacheSize() int { return r.maxCacheEntries }

// Publish validates and atomically installs a new full theme set. The swap is
// the only mutation performed under lock; ctx cancellation before the swap
// aborts publication and leaves the previous snapshot fully in effect.
func (r *Registry) Publish(ctx context.Context, themes []*Theme, defaultName string) error {
	set := make(map[string]*Theme, len(themes))
	warnings := make(map[string][]Warning, len(themes))
	for _, t := range themes {
		if t == nil {
			return fmt.Errorf("%w: nil theme in publish set", ErrInvalidDescriptor)
		}
		if _, dup := set[t.Name]; dup {
			return fmt.Errorf("%w: duplicate theme name %q", ErrInvalidDescriptor, t.Name)
		}
		ws, err := ValidateTheme(t)
		if err != nil {
			return err
		}
		set[t.Name] = t
		warnings[t.Name] = ws
	}
	if defaultName == "" {
		return fmt.Errorf("%w: default theme name is required", ErrInvalidDescriptor)
	}
	if _, ok := set[defaultName]; !ok {
		return fmt.Errorf("%w: default theme %q is not in the publish set",
			ErrInvalidDescriptor, defaultName)
	}

	// Resolve every inheritance chain up front: cycles and broken links must
	// fail the whole publish, never silently degrade to a missing parent.
	for name := range set {
		if _, err := resolveInheritance(set, name); err != nil {
			return err
		}
	}

	if err := ctx.Err(); err != nil {
		return err
	}

	next := &snapshot{
		themes:   set,
		defaults: defaultName,
		warnings: warnings,
	}

	r.mu.Lock()
	next.epoch = r.epoch.Add(1)
	r.current = next
	r.mu.Unlock()
	return nil
}

// current returns the active immutable snapshot.
func (r *Registry) currentSnapshot() *snapshot {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.current
}

// Epoch returns the monotonic version counter; it increments exactly once per
// successful Publish and lets consumers detect and invalidate stale caches.
func (r *Registry) Epoch() uint64 {
	return r.epoch.Load()
}

// DefaultThemeName returns the snapshot's default theme name.
func (r *Registry) DefaultThemeName() string {
	return r.currentSnapshot().defaults
}
