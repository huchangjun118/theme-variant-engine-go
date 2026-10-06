package engine

import (
	"fmt"
	"strings"
)

// 引用语法：字符串值形如 "{tokens.color.primary}" 引用主题令牌，
// "{styles.padding}" 引用同一样式表中的其他键（可派生）。
// 未定义引用返回 ErrUndefinedReference，循环引用返回 ErrReferenceCycle，
// 任一失败都导致整体解析失败，不产出半截结果。

const (
	refPrefix = "{"
	refSuffix = "}"
	refTokens = "tokens."
	refStyles = "styles."
)

// isRef 判断值是否为引用表达式。
func isRef(v any) (string, bool) {
	s, ok := v.(string)
	if !ok {
		return "", false
	}
	if strings.HasPrefix(s, refPrefix) && strings.HasSuffix(s, refSuffix) && len(s) > 2 {
		return s[1 : len(s)-1], true
	}
	return "", false
}

// resolveRefs 解析样式表 s 中的所有引用。tokens 为主题令牌表，
// styles 为同一样式表自身（用于 styles.* 自引用派生）。
func resolveRefs(s Style, tokens map[string]any, styles Style) (Style, error) {
	if styles == nil {
		styles = s
	}
	out := make(Style, len(s))
	for k := range s {
		v, err := resolveKey(k, s, tokens, styles, nil)
		if err != nil {
			return nil, err
		}
		out[k] = v
	}
	return out, nil
}

// resolveKey 解析 styles[key]，带循环检测。chain 记录当前解析链。
func resolveKey(key string, s Style, tokens map[string]any, styles Style, chain []string) (any, error) {
	for _, c := range chain {
		if c == "styles."+key {
			return nil, &ErrReferenceCycle{Chain: append(append([]string{}, chain...), "styles."+key)}
		}
	}
	v, ok := s[key]
	if !ok {
		return nil, &ErrUndefinedReference{Ref: "styles." + key}
	}
	return resolveValue(v, tokens, styles, append(chain, "styles."+key))
}

func resolveValue(v any, tokens map[string]any, styles Style, chain []string) (any, error) {
	if ref, ok := isRef(v); ok {
		return resolveRef(ref, tokens, styles, chain)
	}
	if m, ok := asStyle(v); ok {
		out := make(Style, len(m))
		for k, sub := range m {
			rv, err := resolveValue(sub, tokens, styles, chain)
			if err != nil {
				return nil, err
			}
			out[k] = rv
		}
		return out, nil
	}
	return v, nil
}

func resolveRef(ref string, tokens map[string]any, styles Style, chain []string) (any, error) {
	switch {
	case strings.HasPrefix(ref, refTokens):
		path := strings.TrimPrefix(ref, refTokens)
		full := refTokens + path
		for _, c := range chain {
			if c == full {
				return nil, &ErrReferenceCycle{Chain: append(append([]string{}, chain...), full)}
			}
		}
		val, ok := lookupPath(tokens, path)
		if !ok {
			return nil, &ErrUndefinedReference{Ref: ref}
		}
		// 令牌值本身也可能是引用，继续解析
		return resolveValue(val, tokens, styles, append(chain, full))
	case strings.HasPrefix(ref, refStyles):
		key := strings.TrimPrefix(ref, refStyles)
		if strings.Contains(key, ".") {
			// 支持 styles.a.b 的嵌套路径
			val, ok := lookupPath(styles, key)
			if !ok {
				return nil, &ErrUndefinedReference{Ref: ref}
			}
			return resolveValue(val, tokens, styles, chain)
		}
		return resolveKey(key, styles, tokens, styles, chain)
	default:
		return nil, &ErrUndefinedReference{Ref: ref}
	}
}

// lookupPath 按 "a.b.c" 路径在嵌套 map 中查找。
func lookupPath(m map[string]any, path string) (any, bool) {
	var cur any = m
	for _, seg := range strings.Split(path, ".") {
		cm, ok := asStyle(cur)
		if !ok {
			return nil, false
		}
		cur, ok = cm[seg]
		if !ok {
			return nil, false
		}
	}
	return cur, true
}

// validateRefs 在主题加载阶段对样式表做引用校验（不产出结果）。
func validateRefs(name string, s Style, tokens map[string]any) error {
	if _, err := resolveRefs(s, tokens, s); err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	return nil
}
