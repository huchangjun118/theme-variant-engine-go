package theme

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// referenceRef is a parsed occurrence of {token:...} or {ref:...} inside a
// style value.
type referenceRef struct {
	raw  string // full match including braces
	kind string // "token" or "ref"
	path string // dotted target path
}

var referenceRE = regexp.MustCompile(`\{(token|ref):([^}]*)\}`)

// parseReferences extracts every reference embedded in a string.
func parseReferences(s string) []referenceRef {
	matches := referenceRE.FindAllStringSubmatch(s, -1)
	out := make([]referenceRef, 0, len(matches))
	for _, m := range matches {
		out = append(out, referenceRef{raw: m[0], kind: m[1], path: strings.TrimSpace(m[2])})
	}
	return out
}

// lookupPath reads a dotted path out of a nested map. It resolves only to a
// leaf (non-map) value.
func lookupPath(m map[string]any, path string) (any, bool) {
	cur := m
	parts := strings.Split(path, ".")
	for i, p := range parts {
		if p == "" {
			return nil, false
		}
		v, ok := cur[p]
		if !ok {
			return nil, false
		}
		if i == len(parts)-1 {
			if _, isMap := v.(map[string]any); isMap {
				return nil, false // referencing a subtree, not a concrete value
			}
			return v, true
		}
		next, ok := v.(map[string]any)
		if !ok {
			return nil, false
		}
		cur = next
	}
	return nil, false
}

// refNode identifies one concrete (leaf) style value across all slots.
type refNode struct {
	slot string
	path string
}

func (n refNode) String() string { return n.slot + "." + n.path }

// refContext is everything reachable during reference expansion.
type refContext struct {
	component string
	theme     string
	slots     map[string]ResolvedSlot
	tokens    map[string]any
	raw       map[refNode]any // original leaf values
	refs      map[refNode][]referenceRef
	depSrc    map[refNode][]Source // provenance of each node's targets
}

// expandReferences resolves every {token:...}/{ref:...} reference in all
// resolved slots. Both undefined targets and cyclic references are reported
// as *ReferenceError and cause the ENTIRE resolution to be rejected; slots
// are only mutated after every node has resolved successfully.
func expandReferences(slots map[string]ResolvedSlot, tokens map[string]any, component, theme string) error {
	ctx := &refContext{
		component: component,
		theme:     theme,
		slots:     slots,
		tokens:    tokens,
		raw:       map[refNode]any{},
		refs:      map[refNode][]referenceRef{},
		depSrc:    map[refNode][]Source{},
	}

	// 1. Index every leaf string and its references, in deterministic order.
	var slotNames []string
	for name := range slots {
		slotNames = append(slotNames, name)
	}
	sort.Strings(slotNames)
	for _, name := range slotNames {
		style := slots[name].Style
		var paths []string
		for p := range slots[name].Source {
			paths = append(paths, p)
		}
		sort.Strings(paths)
		collectLeaves(style, paths, func(path string, v any) {
			n := refNode{slot: name, path: path}
			ctx.raw[n] = v
			if s, ok := v.(string); ok {
				if rs := parseReferences(s); len(rs) > 0 {
					ctx.refs[n] = rs
				}
			}
		})
	}

	// 2. Resolve every node on deep copies; nothing is committed yet.
	resolved := map[refNode]any{}
	state := map[refNode]int{} // 0 unseen, 1 visiting, 2 done
	var nodes []refNode
	for n := range ctx.raw {
		nodes = append(nodes, n)
	}
	sort.Slice(nodes, func(i, j int) bool {
		if nodes[i].slot != nodes[j].slot {
			return nodes[i].slot < nodes[j].slot
		}
		return nodes[i].path < nodes[j].path
	})
	for _, n := range nodes {
		if _, err := ctx.resolve(n, state, nil, resolved); err != nil {
			return err
		}
	}

	// 3. Commit resolved values back into slot styles and annotate provenance
	// so a derived value still points at where its targets came from.
	for n, v := range resolved {
		if n.slot == "" {
			continue // synthetic token nodes are not slot leaves
		}
		setLeaf(slots[n.slot].Style, n.path, v)
		if deps, ok := ctx.depSrc[n]; ok && len(deps) > 0 {
			src := slots[n.slot].Source[n.path]
			src.Resolved = deps
			ss := slots[n.slot]
			ss.Source[n.path] = src
			slots[n.slot] = ss
		}
	}
	return nil
}

func collectLeaves(m map[string]any, knownPaths []string, f func(path string, v any)) {
	// provenance keys are exactly the resolved leaves' dotted paths, so each
	// leaf is visited exactly once.
	for _, p := range knownPaths {
		if v, ok := lookupPath(map[string]any(m), p); ok {
			f(p, v)
		}
	}
}

func setLeaf(m map[string]any, path string, v any) {
	parts := strings.Split(path, ".")
	cur := m
	for i, p := range parts {
		if i == len(parts)-1 {
			cur[p] = v
			return
		}
		next, ok := cur[p].(map[string]any)
		if !ok {
			return
		}
		cur = next
	}
}

func (ctx *refContext) resolve(n refNode, state map[refNode]int, stack []refNode, resolved map[refNode]any) (any, error) {
	switch state[n] {
	case 1:
		cyc := append(append([]refNode{}, stack...), n)
		parts := make([]string, len(cyc))
		for i, c := range cyc {
			parts[i] = c.String()
		}
		return nil, &ReferenceError{
			Component: ctx.component, Slot: n.slot, Key: n.path,
			Ref: n.String(), Cycle: true, Path: parts,
		}
	case 2:
		return resolved[n], nil
	}
	state[n] = 1
	stack = append(stack, n)

	raw := ctx.raw[n]
	out := raw
	var deps []Source
	if s, ok := raw.(string); ok {
		refs := ctx.refs[n]
		if len(refs) == 1 && refs[0].raw == strings.TrimSpace(s) {
			// Whole-string reference: preserve the target's native type.
			v, src, err := ctx.resolveTarget(refs[0], n, state, stack, resolved)
			if err != nil {
				return nil, err
			}
			out = v
			deps = append(deps, src)
		} else if len(refs) > 0 {
			// Embedded references: substitute rendered strings.
			var replaceErr error
			out = referenceRE.ReplaceAllStringFunc(s, func(match string) string {
				if replaceErr != nil {
					return match
				}
				rr := parseReferences(match)[0]
				v, src, err := ctx.resolveTarget(rr, n, state, stack, resolved)
				if err != nil {
					replaceErr = err
					return match
				}
				deps = append(deps, src)
				return renderValue(v)
			})
			if replaceErr != nil {
				return nil, replaceErr
			}
		}
	}

	state[n] = 2
	resolved[n] = out
	if n.slot != "" && len(deps) > 0 {
		ctx.depSrc[n] = deps
	}
	return out, nil
}

// targetSource returns the provenance of a referenced target node.
func (ctx *refContext) targetSource(n refNode) Source {
	if n.slot == "" {
		return Source{Layer: LayerTheme, Theme: ctx.theme, KeyPath: n.path}
	}
	return ctx.slots[n.slot].Source[n.path]
}

func (ctx *refContext) resolveTarget(r referenceRef, from refNode, state map[refNode]int, stack []refNode, resolved map[refNode]any) (any, Source, error) {
	switch r.kind {
	case "token":
		v, ok := lookupPath(ctx.tokens, r.path)
		if !ok {
			return nil, Source{}, &ReferenceError{
				Component: ctx.component, Slot: from.slot, Key: from.path,
				Ref: r.raw, Path: []string{from.String(), "token:" + r.path},
			}
		}
		src := Source{Layer: LayerTheme, Theme: ctx.theme, KeyPath: "token:" + r.path}
		// Token value may itself contain references.
		if s, ok := v.(string); ok && len(parseReferences(s)) > 0 {
			tn := refNode{slot: "", path: "token:" + r.path}
			if _, exists := ctx.raw[tn]; !exists {
				ctx.raw[tn] = v
				ctx.refs[tn] = parseReferences(s)
			}
			rv, err := ctx.resolve(tn, state, stack, resolved)
			if err != nil {
				return nil, Source{}, err
			}
			if td, ok := ctx.depSrc[tn]; ok && len(td) > 0 {
				src.Resolved = td
			}
			return rv, src, nil
		}
		return v, src, nil
	case "ref":
		target, err := parseRefPath(r.path, from.slot)
		if err != nil {
			return nil, Source{}, &ReferenceError{
				Component: ctx.component, Slot: from.slot, Key: from.path,
				Ref: r.raw, Path: []string{from.String(), r.path},
			}
		}
		if _, exists := ctx.raw[target]; !exists {
			return nil, Source{}, &ReferenceError{
				Component: ctx.component, Slot: from.slot, Key: from.path,
				Ref: r.raw, Path: []string{from.String(), target.String()},
			}
		}
		v, err := ctx.resolve(target, state, stack, resolved)
		if err != nil {
			return nil, Source{}, err
		}
		src := ctx.targetSource(target)
		if td, ok := ctx.depSrc[target]; ok && len(td) > 0 {
			src.Resolved = td
		}
		return v, src, nil
	default:
		return nil, Source{}, &ReferenceError{
			Component: ctx.component, Slot: from.slot, Key: from.path,
			Ref: r.raw,
		}
	}
}

// parseRefPath accepts "slot.key.path" or ".key.path" (leading dot means the
// referencing node's own slot).
func parseRefPath(p, currentSlot string) (refNode, error) {
	p = strings.TrimSpace(p)
	if p == "" {
		return refNode{}, fmt.Errorf("empty ref path")
	}
	if strings.HasPrefix(p, ".") {
		if currentSlot == "" {
			return refNode{}, fmt.Errorf("relative reference outside a slot")
		}
		return refNode{slot: currentSlot, path: p[1:]}, nil
	}
	dot := strings.IndexByte(p, '.')
	if dot <= 0 {
		return refNode{}, fmt.Errorf("ref must be slot.key-path, got %q", p)
	}
	return refNode{slot: p[:dot], path: p[dot+1:]}, nil
}

func renderValue(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case bool:
		return strconv.FormatBool(x)
	case float64:
		return strconv.FormatFloat(x, 'g', -1, 64)
	case int:
		return strconv.Itoa(x)
	default:
		return fmt.Sprintf("%v", x)
	}
}
