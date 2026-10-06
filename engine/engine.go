package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"sort"
	"sync"
	"sync/atomic"
)

// algorithmVersion 解析算法版本：算法语义变化时递增，使旧缓存整体失效。
const algorithmVersion = "algo-1"

// snapshot 是一份自洽的解析上下文：主题 + 组件注册表。
// 热替换时整体原子替换，读取方永远看到单一的完整版本。
type snapshot struct {
	theme      *Theme
	components map[string]ComponentDesc
}

// Engine 是主题样式解析引擎，支持主题热替换与并发解析。
type Engine struct {
	snap  atomic.Pointer[snapshot]
	cache *cache

	// reloadMu 串行化 ReloadTheme，避免并发重载互相覆盖。
	reloadMu sync.Mutex
}

// New 创建引擎。components 与 theme 在注册时即做完整校验
// （规范版本、插槽引用、继承链、样式引用），校验失败不创建引擎。
func New(components []ComponentDesc, theme *Theme, cacheSize int) (*Engine, error) {
	reg, err := buildRegistry(components)
	if err != nil {
		return nil, err
	}
	if err := validateTheme(theme, reg); err != nil {
		return nil, err
	}
	e := &Engine{cache: newCache(cacheSize)}
	e.snap.Store(&snapshot{theme: theme, components: reg})
	return e, nil
}

// buildRegistry 校验组件描述并构建注册表。
func buildRegistry(components []ComponentDesc) (map[string]ComponentDesc, error) {
	reg := make(map[string]ComponentDesc, len(components))
	for _, c := range components {
		if err := checkSpecVersion("component", c.Name, c.SpecVersion); err != nil {
			return nil, err
		}
		if _, dup := reg[c.Name]; dup {
			return nil, fmt.Errorf("duplicate component %q", c.Name)
		}
		reg[c.Name] = c
	}
	// 校验每个组件的继承链与变体引用的插槽
	for _, c := range components {
		if _, err := resolveInheritance(c.Name, reg); err != nil {
			return nil, err
		}
		for _, v := range c.Variants {
			for slotName := range v.Slots {
				if !slotDeclared(c, slotName) {
					return nil, &ErrUnknownSlot{Component: c.Name, Variant: v.Name, Slot: slotName}
				}
			}
		}
	}
	return reg, nil
}

// validateTheme 校验主题：规范版本、组件/插槽引用、令牌引用。
func validateTheme(t *Theme, reg map[string]ComponentDesc) error {
	if t == nil {
		return fmt.Errorf("theme is nil")
	}
	if err := checkSpecVersion("theme", t.Name, t.SpecVersion); err != nil {
		return err
	}
	if t.Parent != nil {
		if err := validateTheme(t.Parent, reg); err != nil {
			return err
		}
	}
	for compName, ov := range t.Components {
		cd, ok := reg[compName]
		if !ok {
			return &ErrUnknownComponent{Name: compName}
		}
		for slotName := range ov.Slots {
			if !slotDeclared(cd, slotName) {
				return &ErrUnknownSlot{Component: compName, Slot: slotName}
			}
		}
		for vName, slots := range ov.Variants {
			if _, ok := cd.Variants[vName]; !ok {
				return &ErrUnknownVariant{Component: compName, Variant: vName}
			}
			for slotName := range slots {
				if !slotDeclared(cd, slotName) {
					return &ErrUnknownSlot{Component: compName, Variant: vName, Slot: slotName}
				}
			}
		}
	}
	// 校验令牌内部引用（循环/未定义在加载期即拒绝）
	tokens := flattenTokens(t)
	if _, err := resolveRefs(Style(tokens), tokens, nil); err != nil {
		return err
	}
	return nil
}

// flattenTokens 沿主题继承链合并令牌（子主题覆盖父主题）。
func flattenTokens(t *Theme) map[string]any {
	var base map[string]any
	if t.Parent != nil {
		base = flattenTokens(t.Parent)
	}
	merged, _ := mergeStyle(Style(base), Style(t.Tokens), LayerTheme, LayerTheme, "parent-theme", t.Name)
	return map[string]any(merged)
}

// Resolve 解析一次组件样式。结果对相同（组件描述, 主题版本, 输入）确定。
func (e *Engine) Resolve(in Input) (*Result, error) {
	snap := e.snap.Load() // 原子读取：自洽的单一版本
	key := cacheKey(snap, in)
	if r, ok := e.cache.get(key); ok {
		return r, nil
	}
	r := &resolver{theme: snap.theme, components: snap.components}
	res, err := r.resolve(in)
	if err != nil {
		return nil, err
	}
	e.cache.put(key, res)
	return res, nil
}

// cacheKey 由组件描述、主题版本、算法版本与输入取值决定。
func cacheKey(snap *snapshot, in Input) string {
	h := fnv.New128a()
	h.Write([]byte(algorithmVersion))
	h.Write([]byte{0})
	h.Write([]byte(snap.theme.Version))
	h.Write([]byte{0})
	// 输入取值规范化：变体排序，Props/SlotProps 用 json（键有序）
	vars := append([]string(nil), in.Variants...)
	sort.Strings(vars)
	enc, _ := json.Marshal(struct {
		C string
		V []string
		P Style
		S map[string]Style
	}{in.Component, vars, in.Props, in.SlotProps})
	h.Write(enc)
	return fmt.Sprintf("%x", h.Sum(nil))
}

// ReloadTheme 校验并无缝替换主题。
//
//   - 新主题先完整校验，失败时返回错误且当前主题不受影响；
//   - ctx 被取消时不执行替换，当前主题不受影响；
//   - 替换是单次原子指针写入，进行中的解析要么看到旧版本、
//     要么看到新版本，不会看到新旧混合；
//   - 替换成功后缓存整体失效（键中已含主题版本，flush 释放旧条目）。
func (e *Engine) ReloadTheme(ctx context.Context, theme *Theme) error {
	e.reloadMu.Lock()
	defer e.reloadMu.Unlock()

	snap := e.snap.Load()
	if err := validateTheme(theme, snap.components); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	e.snap.Store(&snapshot{theme: theme, components: snap.components})
	e.cache.flush()
	return nil
}

// ThemeVersion 返回当前生效主题版本。
func (e *Engine) ThemeVersion() string {
	return e.snap.Load().theme.Version
}
