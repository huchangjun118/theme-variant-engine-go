package theme

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func cacheComp() *ComponentDescriptor {
	return &ComponentDescriptor{
		Name:        "C",
		SpecVersion: "1.0",
		Slots:       []SlotDecl{{Name: "root", Default: StyleMap{"v": "base"}}},
	}
}

func themeVersioned(name, version, color string) *Theme {
	return &Theme{
		Name: name, Version: version, SpecVersion: "1.0",
		Components: map[string]*ComponentTheme{"C": {Slots: map[string]SlotOverride{
			"root": {Style: StyleMap{"v": color}},
		}}},
	}
}

func TestCacheHitAndBounded(t *testing.T) {
	reg := NewRegistry(WithCacheSize(4))
	mustPublish(t, reg, themeVersioned("t", "1", "x"))
	eng := NewEngine(reg)
	if _, err := eng.Resolve(context.Background(), cacheComp(), InstanceInput{}, ResolveOptions{}); err != nil {
		t.Fatal(err)
	}
	if eng.cache.Len() != 1 {
		t.Fatalf("cache len = %d, want 1", eng.cache.Len())
	}
	for i := 0; i < 20; i++ {
		_, _ = eng.Resolve(context.Background(), cacheComp(),
			InstanceInput{Slots: map[string]StyleMap{"root": {"uniq": i}}},
			ResolveOptions{})
	}
	if eng.cache.Len() > 4 {
		t.Fatalf("cache grew unbounded: %d", eng.cache.Len())
	}
}

func TestCacheInvalidatedOnThemeReplace(t *testing.T) {
	reg := NewRegistry(WithCacheSize(256))
	mustPublish(t, reg, themeVersioned("t", "v1", "one"))
	eng := NewEngine(reg)

	r1, err := eng.Resolve(context.Background(), cacheComp(), InstanceInput{}, ResolveOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if r1.Slots["root"].Style["v"] != "one" {
		t.Fatalf("v1 wrong: %v", r1.Slots["root"].Style["v"])
	}
	if eng.cache.Len() != 1 {
		t.Fatal("expected cached entry")
	}

	mustPublish(t, reg, themeVersioned("t", "v2", "two"))
	r2, err := eng.Resolve(context.Background(), cacheComp(), InstanceInput{}, ResolveOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if r2.ThemeVersion != "v2" || r2.Slots["root"].Style["v"] != "two" {
		t.Fatalf("stale cached result after replace: %#v", r2)
	}
}

func TestCacheKeyIncludesInput(t *testing.T) {
	reg := NewRegistry(WithCacheSize(256))
	mustPublish(t, reg, &Theme{Name: "t", Version: "1", SpecVersion: "1.0"})
	eng := NewEngine(reg)
	rA, _ := eng.Resolve(context.Background(), cacheComp(),
		InstanceInput{Slots: map[string]StyleMap{"root": {"v": "A"}}}, ResolveOptions{})
	rB, _ := eng.Resolve(context.Background(), cacheComp(),
		InstanceInput{Slots: map[string]StyleMap{"root": {"v": "B"}}}, ResolveOptions{})
	if rA.Slots["root"].Style["v"] == rB.Slots["root"].Style["v"] {
		t.Fatal("different inputs collapsed to one cache entry")
	}
	if eng.cache.Len() != 2 {
		t.Fatalf("cache len = %d want 2", eng.cache.Len())
	}
}

func TestCorruptCacheEntryNeverServed(t *testing.T) {
	reg := NewRegistry(WithCacheSize(256))
	mustPublish(t, reg, &Theme{Name: "t", Version: "1", SpecVersion: "1.0"})
	eng := NewEngine(reg)
	r, err := eng.Resolve(context.Background(), cacheComp(), InstanceInput{}, ResolveOptions{})
	if err != nil {
		t.Fatal(err)
	}
	key, _ := makeCacheKey(cacheComp(), "t", "1", InstanceInput{}, ResolveOptions{})
	// Simulate a torn/corrupted entry: tamper with the cached payload.
	eng.cache.mu.Lock()
	el := eng.cache.items[key]
	tampered := *el.Value.(*lruItem)
	tampered.entry = &cacheEntry{epoch: el.Value.(*lruItem).entry.epoch, result: r}
	tampered.entry.result.Slots["root"].Style["v"] = "CORRUPT"
	eng.cache.items[key] = eng.cache.ll.PushFront(&tampered)
	eng.cache.ll.Remove(el)
	eng.cache.mu.Unlock()

	r2, ok := eng.cache.get(key, reg.Epoch())
	if ok || r2 != nil {
		t.Fatal("corrupt entry must not be served")
	}
}

func TestCancelledPublishDoesNotSwap(t *testing.T) {
	reg := NewRegistry()
	mustPublish(t, reg, themeVersioned("t", "v1", "one"))

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled at swap time
	err := reg.Publish(ctx, []*Theme{themeVersioned("t", "v2", "two")}, "t")
	if err == nil {
		t.Fatal("cancelled publish must error")
	}
	if reg.Epoch() != 1 {
		t.Fatalf("epoch advanced on cancelled publish: %d", reg.Epoch())
	}
}

func TestConcurrentResolveAndReplace(t *testing.T) {
	reg := NewRegistry(WithCacheSize(64))
	mustPublish(t, reg, themeVersioned("t", "v0", "v0"))
	eng := NewEngine(reg)

	stop := make(chan struct{})
	var wg sync.WaitGroup
	var failures atomic.Int64

	// Readers: every result must be self-consistent for its version.
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				r, err := eng.Resolve(context.Background(), cacheComp(), InstanceInput{}, ResolveOptions{})
				if err != nil {
					failures.Add(1)
					return
				}
				v := r.Slots["root"].Style["v"].(string)
				if v != r.ThemeVersion {
					failures.Add(1) // mixed old/new snapshot observed
					return
				}
			}
		}()
	}

	// Replacer: publishes versions vN with matching color vN.
	for i := 1; i <= 30; i++ {
		ver := fmt.Sprintf("v%d", i)
		if err := reg.Publish(context.Background(), []*Theme{themeVersioned("t", ver, ver)}, "t"); err != nil {
			t.Fatalf("publish %s: %v", ver, err)
		}
		time.Sleep(time.Millisecond)
	}
	close(stop)
	wg.Wait()
	if n := failures.Load(); n != 0 {
		t.Fatalf("%d readers observed mixed/inconsistent snapshots or errors", n)
	}
	if reg.Epoch() != 31 {
		t.Fatalf("epoch = %d want 31", reg.Epoch())
	}
}
