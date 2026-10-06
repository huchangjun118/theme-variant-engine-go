package engine

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
)

func themeV(version, brand string) *Theme {
	return &Theme{
		Name: "t" + version, Version: version, SpecVersion: "1.1",
		Tokens: map[string]any{"color": map[string]any{"brand": brand}},
		Components: map[string]ComponentOverride{
			"button": {Slots: map[string]Style{"root": {"color": "{tokens.color.brand}"}}},
		},
	}
}

func TestReloadThemeSwapsAtomically(t *testing.T) {
	e := newTestEngine(t)
	if err := e.ReloadTheme(context.Background(), themeV("v2", "purple")); err != nil {
		t.Fatalf("reload: %v", err)
	}
	if e.ThemeVersion() != "v2" {
		t.Fatalf("version = %q", e.ThemeVersion())
	}
	r, err := e.Resolve(Input{Component: "button"})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if r.Slots["root"].Style["color"] != "purple" {
		t.Fatalf("color = %v, want purple", r.Slots["root"].Style["color"])
	}
	if r.ThemeVersion != "v2" {
		t.Fatalf("result theme version = %q", r.ThemeVersion)
	}
}

func TestReloadFailureKeepsCurrent(t *testing.T) {
	e := newTestEngine(t)
	before := e.ThemeVersion()

	// 校验失败：引用未定义令牌
	bad := themeV("v3", "x")
	bad.Components["button"] = ComponentOverride{
		Slots: map[string]Style{"root": {"color": "{tokens.missing}"}},
	}
	// 注意：引用在解析期才展开到组件样式，这里主题自身校验通过，
	// 因此用一个真正非法的主题：规范版本越界。
	bad.SpecVersion = "7.0"
	if err := e.ReloadTheme(context.Background(), bad); err == nil {
		t.Fatal("expected reload error")
	}
	if e.ThemeVersion() != before {
		t.Fatalf("version changed to %q after failed reload", e.ThemeVersion())
	}

	// 上下文取消：不执行替换
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := e.ReloadTheme(ctx, themeV("v4", "y")); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if e.ThemeVersion() != before {
		t.Fatalf("version changed to %q after canceled reload", e.ThemeVersion())
	}
}

func TestConcurrentResolveDuringReload(t *testing.T) {
	e := newTestEngine(t)
	in := Input{Component: "button", Variants: []string{"primary"}}

	const readers = 8
	const rounds = 200
	var wg sync.WaitGroup
	errCh := make(chan error, readers)

	// 并发解析：每次结果必须自洽地属于某一个完整版本
	for i := 0; i < readers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < rounds; j++ {
				r, err := e.Resolve(in)
				if err != nil {
					errCh <- err
					return
				}
				// 自洽性：结果的主题版本必须与实际取到的颜色一致
				got := r.Slots["root"].Style["color"]
				want := map[string]string{"v1": "green", "v2": "purple"}[r.ThemeVersion]
				if got != want {
					errCh <- fmt.Errorf("mixed version: theme=%s color=%v", r.ThemeVersion, got)
					return
				}
			}
		}()
	}
	// 慢解析不阻塞替换：替换 goroutine 直接推进
	wg.Add(1)
	go func() {
		defer wg.Done()
		for j := 0; j < 20; j++ {
			v := "v1"
			brand := "green"
			if j%2 == 1 {
				v, brand = "v2", "purple"
			}
			th := themeV(v, brand)
			th.Components["button"] = ComponentOverride{
				Slots: map[string]Style{"root": {"color": brand}},
			}
			if err := e.ReloadTheme(context.Background(), th); err != nil {
				errCh <- err
				return
			}
		}
	}()
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Fatal(err)
	}
}

func TestCacheInvalidationOnThemeChange(t *testing.T) {
	e := newTestEngine(t)
	in := Input{Component: "button"}

	r1, err := e.Resolve(in)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if e.cache.len() != 1 {
		t.Fatalf("cache len = %d, want 1", e.cache.len())
	}
	// 再次解析命中缓存（同一指针）
	r2, _ := e.Resolve(in)
	if r1 != r2 {
		t.Fatal("expected cache hit to return same result")
	}

	// 主题替换后缓存整体失效
	th := themeV("v2", "purple")
	th.Components["button"] = ComponentOverride{Slots: map[string]Style{"root": {"color": "purple"}}}
	if err := e.ReloadTheme(context.Background(), th); err != nil {
		t.Fatalf("reload: %v", err)
	}
	if e.cache.len() != 0 {
		t.Fatalf("cache not flushed, len = %d", e.cache.len())
	}
	r3, err := e.Resolve(in)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if r3 == r1 {
		t.Fatal("stale cache entry returned after theme change")
	}
	if r3.Slots["root"].Style["color"] != "purple" {
		t.Fatalf("color = %v, want purple", r3.Slots["root"].Style["color"])
	}
}

func TestCacheBounded(t *testing.T) {
	e, err := New(testComponents(), testTheme(), 8)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	// 写入远超上限的不同输入
	for i := 0; i < 100; i++ {
		if _, err := e.Resolve(Input{
			Component: "button",
			Props:     Style{"color": fmt.Sprintf("c%d", i)},
		}); err != nil {
			t.Fatalf("resolve: %v", err)
		}
	}
	if e.cache.len() > 8 {
		t.Fatalf("cache len = %d, exceeds max 8", e.cache.len())
	}
}

func TestCacheCorruptionRejected(t *testing.T) {
	c := newCache(4)
	r := &Result{Component: "x", Slots: map[string]ResolvedSlot{}}
	c.put("k", r)
	// 模拟外部损坏：直接改结果内容
	r.Component = "tampered"
	if _, ok := c.get("k"); ok {
		t.Fatal("corrupted entry accepted")
	}
	if c.len() != 0 {
		t.Fatalf("corrupted entry not evicted, len = %d", c.len())
	}
}

func TestDeterministicAcrossRepeatedResolution(t *testing.T) {
	e := newTestEngine(t)
	in := Input{Component: "iconButton", Variants: []string{"primary", "large"}}
	first, err := e.Resolve(in)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	for i := 0; i < 50; i++ {
		r, err := e.Resolve(in)
		if err != nil {
			t.Fatalf("resolve: %v", err)
		}
		for name, slot := range first.Slots {
			got := r.Slots[name]
			if fmt.Sprintf("%v", got.Style) != fmt.Sprintf("%v", slot.Style) {
				t.Fatalf("non-deterministic slot %s: %v vs %v", name, got.Style, slot.Style)
			}
		}
	}
}
