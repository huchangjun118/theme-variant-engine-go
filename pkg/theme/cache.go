package theme

import (
	"container/list"
	"crypto/sha256"
	"encoding/json"
	"sync"
)

// cacheKey identifies a resolution result. It is derived solely from the
// component descriptor, the exact theme version, and the instance input
// (variants, slot styles, outer context, variant order). AlgorithmVersion is
// mixed in so any algorithm change invalidates every entry at once; theme
// replacement is invalidated structurally because the version changes.
type cacheKey struct{ hash [sha256.Size]byte }

type cachePayload struct {
	Algorithm    string              `json:"algorithm"`
	Component    json.RawMessage     `json:"component"`
	ThemeName    string              `json:"themeName"`
	ThemeVersion string              `json:"themeVersion"`
	Input        InstanceInput       `json:"input"`
	Order        []string            `json:"order"`
	Outer        map[string]StyleMap `json:"outer,omitempty"`
	OuterName    string              `json:"outerName,omitempty"`
}

func makeCacheKey(comp *ComponentDescriptor, themeName, themeVersion string, input InstanceInput, opts ResolveOptions) (cacheKey, error) {
	raw, err := json.Marshal(comp)
	if err != nil {
		return cacheKey{}, err
	}
	p := cachePayload{
		Algorithm:    AlgorithmVersion,
		Component:    raw,
		ThemeName:    themeName,
		ThemeVersion: themeVersion,
		Input:        input,
		Order:        opts.VariantOrder,
		Outer:        opts.OuterContext,
		OuterName:    opts.OuterComponent,
	}
	buf, err := json.Marshal(p)
	if err != nil {
		return cacheKey{}, err
	}
	return cacheKey{hash: sha256.Sum256(buf)}, nil
}

// cacheEntry is one cached resolution. Entries are fully built and checksummed
// before publication; get() recomputes the checksum so a corrupted or torn
// entry is never served as a valid result.
type cacheEntry struct {
	epoch    uint64
	result   *ResolvedComponent
	checksum [sha256.Size]byte
}

func newCacheEntry(epoch uint64, r *ResolvedComponent) *cacheEntry {
	buf, err := json.Marshal(r)
	if err != nil {
		return nil
	}
	return &cacheEntry{epoch: epoch, result: r, checksum: sha256.Sum256(buf)}
}

func (e *cacheEntry) valid() bool {
	if e == nil || e.result == nil {
		return false
	}
	buf, err := json.Marshal(e.result)
	if err != nil {
		return false
	}
	return sha256.Sum256(buf) == e.checksum
}

// resultCache is a concurrency-safe, bounded LRU cache. Its size grows only
// up to maxEntries regardless of request volume; maxEntries == 0 disables it.
type resultCache struct {
	mu         sync.Mutex
	maxEntries int
	ll         *list.List
	items      map[cacheKey]*list.Element
}

type lruItem struct {
	key   cacheKey
	entry *cacheEntry
}

func newResultCache(maxEntries int) *resultCache {
	if maxEntries < 0 {
		maxEntries = 0
	}
	return &resultCache{
		maxEntries: maxEntries,
		ll:         list.New(),
		items:      make(map[cacheKey]*list.Element),
	}
}

func (c *resultCache) get(k cacheKey, epoch uint64) (*ResolvedComponent, bool) {
	if c == nil || c.maxEntries == 0 {
		return nil, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	el, ok := c.items[k]
	if !ok {
		return nil, false
	}
	it := el.Value.(*lruItem)
	if it.entry.epoch != epoch || !it.entry.valid() {
		// Stale entry from a previous theme version, or corrupted/torn data:
		// never served as a valid result.
		c.ll.Remove(el)
		delete(c.items, k)
		return nil, false
	}
	c.ll.MoveToFront(el)
	return it.entry.result, true
}

func (c *resultCache) put(k cacheKey, epoch uint64, r *ResolvedComponent) {
	if c == nil || c.maxEntries == 0 {
		return
	}
	entry := newCacheEntry(epoch, r)
	if entry == nil || !entry.valid() {
		return // never publish a partial/unserializable result
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.items[k]; ok {
		c.ll.MoveToFront(el)
		el.Value.(*lruItem).entry = entry
		return
	}
	el := c.ll.PushFront(&lruItem{key: k, entry: entry})
	c.items[k] = el
	for c.ll.Len() > c.maxEntries {
		oldest := c.ll.Back()
		if oldest == nil {
			break
		}
		c.ll.Remove(oldest)
		delete(c.items, oldest.Value.(*lruItem).key)
	}
}

// Len reports the current number of cached entries.
func (c *resultCache) Len() int {
	if c == nil {
		return 0
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.ll.Len()
}

// Clear drops every entry; used on algorithm/version invalidation.
func (c *resultCache) Clear() {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.ll.Init()
	c.items = make(map[cacheKey]*list.Element)
}
