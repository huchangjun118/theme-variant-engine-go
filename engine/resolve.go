package engine

import "sort"

// slotDeclared 判断组件（含继承链展开前）是否直接声明了某插槽。
func slotDeclared(c ComponentDesc, name string) bool {
	for _, s := range c.Slots {
		if s.Name == name {
			return true
		}
	}
	return false
}

// resolveInheritance 展开组件的继承链，返回从根到子的组件序列。
// 循环返回 ErrInheritanceCycle，父组件不存在返回 ErrBrokenInheritance，
// 两者是可区分的错误类型，且不会静默退回默认值。
func resolveInheritance(name string, reg map[string]ComponentDesc) ([]ComponentDesc, error) {
	var chain []ComponentDesc
	seen := map[string]int{}
	cur := name
	for {
		if idx, ok := seen[cur]; ok {
			cycle := make([]string, 0, len(chain)-idx+1)
			for _, c := range chain[idx:] {
				cycle = append(cycle, c.Name)
			}
			return nil, &ErrInheritanceCycle{Chain: append(cycle, cur)}
		}
		cd, ok := reg[cur]
		if !ok {
			if cur == name {
				return nil, &ErrUnknownComponent{Name: cur}
			}
			return nil, &ErrBrokenInheritance{Component: chain[len(chain)-1].Name, Parent: cur}
		}
		seen[cur] = len(chain)
		chain = append(chain, cd)
		if cd.Extends == "" {
			break
		}
		cur = cd.Extends
	}
	// 反转为 根 -> 子
	for i, j := 0, len(chain)-1; i < j; i, j = i+1, j-1 {
		chain[i], chain[j] = chain[j], chain[i]
	}
	return chain, nil
}

// effectiveComponent 沿继承链合并组件描述：
// 子组件继承父组件的插槽与变体，同名插槽默认样式做字段级合并，
// 同名变体按插槽做字段级合并。实例级取值优先于继承值由调用方
// 在分层合并时保证（instance 层最后合并）。
func effectiveComponent(chain []ComponentDesc) ComponentDesc {
	out := ComponentDesc{
		Name:     chain[len(chain)-1].Name,
		Slots:    nil,
		Variants: map[string]Variant{},
	}
	slotIndex := map[string]int{}
	for _, cd := range chain {
		for _, s := range cd.Slots {
			if idx, ok := slotIndex[s.Name]; ok {
				merged, _ := mergeStyle(out.Slots[idx].Default, s.Default, LayerBase, LayerBase, "parent", cd.Name)
				out.Slots[idx].Default = merged
			} else {
				slotIndex[s.Name] = len(out.Slots)
				out.Slots = append(out.Slots, Slot{Name: s.Name, Default: s.Default})
			}
		}
		for vName, v := range cd.Variants {
			if ev, ok := out.Variants[vName]; ok {
				merged := Variant{Name: vName, Slots: map[string]Style{}}
				for slotName, st := range ev.Slots {
					merged.Slots[slotName] = st
				}
				for slotName, st := range v.Slots {
					m, _ := mergeStyle(merged.Slots[slotName], st, LayerBase, LayerBase, "parent", cd.Name)
					merged.Slots[slotName] = m
				}
				out.Variants[vName] = merged
			} else {
				out.Variants[vName] = v
			}
		}
		out.DefaultVariants = append(out.DefaultVariants, cd.DefaultVariants...)
	}
	return out
}

// resolver 针对一份确定的主题快照执行解析。
type resolver struct {
	theme      *Theme
	components map[string]ComponentDesc
}

// resolve 解析组件在指定输入下的插槽样式。
//
// 合并顺序（低 -> 高优先级）：
//  1. base：继承链展开后的插槽默认样式 + 组合变体样式（变体按名称排序依次叠加，保证确定性）；
//  2. theme：主题继承链展开后的组件覆盖（父主题 -> 子主题）；
//  3. instance：实例传入的 Props（公共）与 SlotProps（按插槽）。
//
// 每个字段的来源层记录在 Result.Slots[x].Origins 中。
func (r *resolver) resolve(in Input) (*Result, error) {
	chain, err := resolveInheritance(in.Component, r.components)
	if err != nil {
		return nil, err
	}
	comp := effectiveComponent(chain)

	// 组合变体：输入变体 + 废弃的 DefaultVariants（记录警告）
	var warnings []string
	variantNames := append([]string(nil), in.Variants...)
	if len(comp.DefaultVariants) > 0 {
		warnings = append(warnings, "component "+comp.Name+": field DefaultVariants is deprecated; pass variants explicitly in Input.Variants")
		variantNames = append(variantNames, comp.DefaultVariants...)
	}
	variantNames = dedupSorted(variantNames)
	for _, vn := range variantNames {
		if _, ok := comp.Variants[vn]; !ok {
			return nil, &ErrUnknownVariant{Component: comp.Name, Variant: vn}
		}
	}

	tokens := flattenTokens(r.theme)
	overrides := flattenOverrides(r.theme, comp.Name)

	res := &Result{
		Component:          comp.Name,
		ThemeVersion:       r.theme.Version,
		Slots:              map[string]ResolvedSlot{},
		DeprecatedWarnings: warnings,
	}

	for _, slot := range comp.Slots {
		// 第 1 层：base = 默认样式 + 各变体叠加
		style, origins := mergeStyle(nil, slot.Default, LayerBase, LayerBase, "", comp.Name)
		for _, vn := range variantNames {
			v := comp.Variants[vn]
			if vs, ok := v.Slots[slot.Name]; ok {
				style, origins = mergeStyleWith(style, origins, vs, LayerBase, "variant:"+vn)
			}
		}
		// 第 2 层：theme 覆盖（含变体级覆盖）
		if ov, ok := overrides.slots[slot.Name]; ok {
			style, origins = mergeStyleWith(style, origins, ov, LayerTheme, "theme:"+r.theme.Name)
		}
		for _, vn := range variantNames {
			if vov, ok := overrides.variant(vn, slot.Name); ok {
				style, origins = mergeStyleWith(style, origins, vov, LayerTheme, "theme:"+r.theme.Name+":variant:"+vn)
			}
		}
		// 第 3 层：instance
		if in.Props != nil {
			style, origins = mergeStyleWith(style, origins, in.Props, LayerInstance, "instance:props")
		}
		if sp, ok := in.SlotProps[slot.Name]; ok {
			style, origins = mergeStyleWith(style, origins, sp, LayerInstance, "instance:slotProps")
		}
		// 引用解析：任何未定义/循环引用都导致整体失败
		resolved, err := resolveRefs(style, tokens, style)
		if err != nil {
			return nil, err
		}
		res.Slots[slot.Name] = ResolvedSlot{Style: resolved, Origins: origins}
	}
	return res, nil
}

// mergeStyleWith 在已有合并结果上叠加新一层，并合并来源记录：
// 仅 upper 实际触及的字段路径会被标记为新来源，其余保留旧来源。
func mergeStyleWith(style Style, origins map[string]FieldOrigin, upper Style, layer Layer, src string) (Style, map[string]FieldOrigin) {
	merged, _ := mergeStyle(style, upper, layer, layer, "", src)
	out := make(map[string]FieldOrigin, len(origins)+len(upper))
	for k, o := range origins {
		out[k] = o
	}
	for _, p := range leafPaths(upper, "") {
		out[p] = FieldOrigin{Layer: layer, Source: src}
	}
	return merged, out
}

// leafPaths 返回样式表中所有叶子字段的路径。
func leafPaths(s Style, prefix string) []string {
	var out []string
	for k, v := range s {
		path := joinPath(prefix, k)
		if m, ok := asStyle(v); ok {
			out = append(out, leafPaths(m, path)...)
		} else {
			out = append(out, path)
		}
	}
	return out
}

// themeOverrides 是主题继承链合并后的组件覆盖视图。
type themeOverrides struct {
	slots    map[string]Style
	variants map[string]map[string]Style
}

func (o themeOverrides) variant(variantName, slotName string) (Style, bool) {
	if o.variants == nil {
		return nil, false
	}
	slots, ok := o.variants[variantName]
	if !ok {
		return nil, false
	}
	st, ok := slots[slotName]
	return st, ok
}

// flattenOverrides 沿主题继承链合并指定组件的覆盖（父主题 -> 子主题，字段级合并）。
func flattenOverrides(t *Theme, compName string) themeOverrides {
	var out themeOverrides
	if t.Parent != nil {
		out = flattenOverrides(t.Parent, compName)
	}
	ov, ok := t.Components[compName]
	if !ok {
		return out
	}
	if out.slots == nil {
		out.slots = map[string]Style{}
	}
	for slotName, st := range ov.Slots {
		m, _ := mergeStyle(out.slots[slotName], st, LayerTheme, LayerTheme, "parent-theme", t.Name)
		out.slots[slotName] = m
	}
	if out.variants == nil {
		out.variants = map[string]map[string]Style{}
	}
	for vName, slots := range ov.Variants {
		dst, ok := out.variants[vName]
		if !ok {
			dst = map[string]Style{}
			out.variants[vName] = dst
		}
		for slotName, st := range slots {
			m, _ := mergeStyle(dst[slotName], st, LayerTheme, LayerTheme, "parent-theme", t.Name)
			dst[slotName] = m
		}
	}
	return out
}

func dedupSorted(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out
}
