package theme

import "reflect"

// styleEqual compares resolved styles treating StyleMap and
// map[string]any as equivalent and comparing numbers numerically.
func styleEqual(a, b any) bool {
	switch av := a.(type) {
	case StyleMap:
		bv, ok := b.(StyleMap)
		if !ok {
			return false
		}
		if len(av) != len(bv) {
			return false
		}
		for k, v := range av {
			if !styleEqual(v, bv[k]) {
				return false
			}
		}
		return true
	case map[string]any:
		bv, ok := b.(map[string]any)
		if !ok {
			return false
		}
		if len(av) != len(bv) {
			return false
		}
		for k, v := range av {
			if !styleEqual(v, bv[k]) {
				return false
			}
		}
		return true
	case []any:
		bv, ok := b.([]any)
		if !ok || len(av) != len(bv) {
			return false
		}
		for i := range av {
			if !styleEqual(av[i], bv[i]) {
				return false
			}
		}
		return true
	default:
		return reflect.DeepEqual(a, b)
	}
}

func asTestMap(v any) map[string]any {
	switch x := v.(type) {
	case map[string]any:
		return x
	case StyleMap:
		return map[string]any(x)
	default:
		return nil
	}
}
