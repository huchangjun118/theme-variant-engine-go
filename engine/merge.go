package engine

// mergeStyle 将 upper 合并到 lower 上，返回合并结果与每个顶层字段的来源。
//
// 合并语义（确定性）：
//   - 两边都是 map 的值：递归做字段级合并；
//   - 其余情况（标量、类型不一致、upper 为 nil）：upper 整体替换 lower；
//   - lower 中未被 upper 覆盖的字段保持原值与原来源。
//
// 返回的 origins 记录合并后每个顶层字段的来源层；嵌套字段的来源以
// "a.b.c" 路径形式记录。
func mergeStyle(lower, upper Style, lowerLayer, upperLayer Layer, lowerSrc, upperSrc string) (Style, map[string]FieldOrigin) {
	out := make(Style, len(lower)+len(upper))
	origins := make(map[string]FieldOrigin)
	mergeInto(out, origins, "", lower, upper, lowerLayer, upperLayer, lowerSrc, upperSrc)
	return out, origins
}

func mergeInto(out Style, origins map[string]FieldOrigin, prefix string, lower, upper Style, lowerLayer, upperLayer Layer, lowerSrc, upperSrc string) {
	for k, v := range lower {
		path := joinPath(prefix, k)
		if lv, ok := v.(Style); ok {
			merged := make(Style, len(lv))
			mergeInto(merged, origins, path, lv, nil, lowerLayer, upperLayer, lowerSrc, upperSrc)
			out[k] = merged
		} else if lv, ok := v.(map[string]any); ok {
			merged := make(Style, len(lv))
			mergeInto(merged, origins, path, Style(lv), nil, lowerLayer, upperLayer, lowerSrc, upperSrc)
			out[k] = merged
		} else {
			out[k] = v
			origins[path] = FieldOrigin{Layer: lowerLayer, Source: lowerSrc}
		}
	}
	for k, uv := range upper {
		path := joinPath(prefix, k)
		lv, exists := out[k]
		uvStyle, uvIsMap := asStyle(uv)
		lvStyle, lvIsMap := asStyle(lv)
		switch {
		case exists && uvIsMap && lvIsMap:
			// 字段级递归合并
			merged := make(Style, len(lvStyle)+len(uvStyle))
			mergeInto(merged, origins, path, lvStyle, uvStyle, lowerLayer, upperLayer, lowerSrc, upperSrc)
			out[k] = merged
		default:
			// 整体替换（含 uv == nil 的显式清除）
			if uvIsMap {
				merged := make(Style, len(uvStyle))
				mergeInto(merged, origins, path, nil, uvStyle, lowerLayer, upperLayer, lowerSrc, upperSrc)
				out[k] = merged
			} else {
				out[k] = uv
				origins[path] = FieldOrigin{Layer: upperLayer, Source: upperSrc}
			}
		}
	}
}

func asStyle(v any) (Style, bool) {
	switch t := v.(type) {
	case Style:
		return t, true
	case map[string]any:
		return Style(t), true
	}
	return nil, false
}

func joinPath(prefix, key string) string {
	if prefix == "" {
		return key
	}
	return prefix + "." + key
}
