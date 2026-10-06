package theme

import (
	"slices"
)

// themeChain is a validated parent-first theme inheritance chain.
type themeChain struct {
	themes []*Theme
	names  []string
}

// resolveInheritance walks parent pointers from the named theme and returns
// the parent-first chain. Cycles return *InheritanceError{Cycle:true}; broken
// links return *InheritanceError{Cycle:false}. Missing themes are never
// silently treated as "no parent".
func resolveInheritance(themes map[string]*Theme, name string) (*themeChain, error) {
	var traversal []*Theme // child-first while walking
	var path []string      // names in walk order, for diagnostics
	seen := map[string]int{}
	cur := name
	for {
		if _, ok := seen[cur]; ok {
			idx := seen[cur]
			cyc := slices.Concat(path[idx:], []string{cur})
			return nil, &InheritanceError{Theme: cur, Chain: cyc, Cycle: true}
		}
		t, ok := themes[cur]
		if !ok {
			chain := slices.Concat(path, []string{cur})
			referrer := cur
			if len(path) > 0 {
				referrer = path[len(path)-1]
			}
			return nil, &InheritanceError{Theme: referrer, Chain: chain, Cycle: false}
		}
		seen[cur] = len(path)
		path = append(path, cur)
		traversal = append(traversal, t)
		if t.Inherits == "" {
			break
		}
		cur = t.Inherits
	}
	slices.Reverse(traversal)
	names := make([]string, len(traversal))
	for i, t := range traversal {
		names[i] = t.Name
	}
	return &themeChain{themes: traversal, names: names}, nil
}

// chainTokens merges token maps across the chain (parent first, child wins on
// equal keys); nested maps merge field by field.
func chainTokens(chain *themeChain) map[string]any {
	out := map[string]any{}
	for _, t := range chain.themes {
		for k, v := range t.Tokens {
			if vm, ok := asMap(v); ok {
				if exist, ok := asMap(out[k]); ok {
					out[k] = mergePlain(exist, vm)
					continue
				}
			}
			out[k] = deepCopyValue(v)
		}
	}
	return out
}

// deepCopyValue returns a deep copy of decoded style values.
func deepCopyValue(v any) any {
	switch x := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, vv := range x {
			out[k] = deepCopyValue(vv)
		}
		return out
	case StyleMap:
		return StyleMap(deepCopyValue(map[string]any(x)).(map[string]any))
	case []any:
		out := make([]any, len(x))
		for i, vv := range x {
			out[i] = deepCopyValue(vv)
		}
		return out
	default:
		return v
	}
}

// mergePlain returns a new map containing src merged on top of dst; nested
// maps merge field by field, every other value replaces wholesale.
func mergePlain(dst, src map[string]any) map[string]any {
	out := make(map[string]any, len(dst)+len(src))
	for k, v := range dst {
		out[k] = deepCopyValue(v)
	}
	for k, v := range src {
		if vm, ok := asMap(v); ok {
			if exist, ok := asMap(out[k]); ok {
				out[k] = mergePlain(exist, vm)
				continue
			}
		}
		out[k] = deepCopyValue(v)
	}
	return out
}
