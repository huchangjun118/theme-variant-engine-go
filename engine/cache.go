package engine

import (
	"container/list"
	"encoding/json"
	"hash/fnv"
	"sync"
)

// cache 是有界 LRU 缓存。
//
//   - 键由调用方根据（组件描述、主题版本、算法版本、输入取值）计算；
//   - 主题或算法版本变化时通过 flush 整体失效；
//   - 每个条目带校验和，损坏或部分写入的条目不会被当成有效结果；
//   - 条目数上限固定，超出时淘汰最久未使用项，占用不随请求量无界增长。
type cache struct {
	mu         sync.Mutex
	maxEntries int
	ll         *list.List // front = 最近使用
	items      map[string]*list.Element
}

type cacheEntry struct {
	key    string
	result *Result
	sum    uint64 // result 内容的校验和
}

func newCache(maxEntries int) *cache {
	if maxEntries <= 0 {
		maxEntries = 256
	}
	return &cache{
		maxEntries: maxEntries,
		ll:         list.New(),
		items:      make(map[string]*list.Element),
	}
}

// checksum 计算结果的完整性校验和。
func checksum(r *Result) uint64 {
	h := fnv.New64a()
	// json 序列化对 map 键排序，输出确定
	b, err := json.Marshal(r)
	if err != nil {
		return 0
	}
	h.Write(b)
	return h.Sum64()
}

func (c *cache) get(key string) (*Result, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	el, ok := c.items[key]
	if !ok {
		return nil, false
	}
	ent := el.Value.(*cacheEntry)
	if ent.result == nil || checksum(ent.result) != ent.sum {
		// 损坏或部分写入：移除并按未命中处理
		c.removeElement(el)
		return nil, false
	}
	c.ll.MoveToFront(el)
	return ent.result, true
}

func (c *cache) put(key string, r *Result) {
	if r == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.items[key]; ok {
		el.Value = &cacheEntry{key: key, result: r, sum: checksum(r)}
		c.ll.MoveToFront(el)
		return
	}
	el := c.ll.PushFront(&cacheEntry{key: key, result: r, sum: checksum(r)})
	c.items[key] = el
	for c.ll.Len() > c.maxEntries {
		c.removeElement(c.ll.Back())
	}
}

func (c *cache) flush() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.ll.Init()
	c.items = make(map[string]*list.Element)
}

func (c *cache) removeElement(el *list.Element) {
	if el == nil {
		return
	}
	c.ll.Remove(el)
	delete(c.items, el.Value.(*cacheEntry).key)
}

// len 返回当前条目数（供测试与监控）。
func (c *cache) len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.ll.Len()
}
