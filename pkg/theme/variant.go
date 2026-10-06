package theme

import (
	"fmt"
	"slices"
)

// variantPlan is the validated, ordered application plan for one resolution.
type variantPlan struct {
	component string
	order     []string
}

// planVariants produces a deterministic application order for the requested
// variants.
//
// Default order is the variants' declaration order in the component
// descriptor. ResolveOptions.VariantOrder may pin a different order, in which
// case it must name exactly the requested set (no missing or extra keys).
//
// Whether each planned key is actually declared is enforced per slot in
// mergeLayers so the resulting error names both the offending slot and the
// variant key.
func planVariants(comp *ComponentDescriptor, input InstanceInput, opts ResolveOptions) (*variantPlan, error) {
	if comp == nil {
		return nil, fmt.Errorf("%w: nil component descriptor", ErrInvalidDescriptor)
	}

	requested := dedupe(input.Variants)

	if len(opts.VariantOrder) > 0 {
		ordered := dedupe(opts.VariantOrder)
		if len(ordered) != len(requested) {
			return nil, fmt.Errorf("%w: component %q variantOrder %v does not match requested variants %v",
				ErrInvalidDescriptor, comp.Name, ordered, requested)
		}
		for _, v := range requested {
			if !slices.Contains(ordered, v) {
				return nil, fmt.Errorf("%w: component %q variantOrder %v is missing requested variant %q",
					ErrInvalidDescriptor, comp.Name, ordered, v)
			}
		}
		return &variantPlan{component: comp.Name, order: ordered}, nil
	}

	index := make(map[string]int, len(comp.Variants))
	for i, v := range comp.Variants {
		index[v.Name] = i
	}
	ordered := make([]string, len(requested))
	copy(ordered, requested)
	slices.SortFunc(ordered, func(a, b string) int {
		return index[a] - index[b]
	})
	return &variantPlan{component: comp.Name, order: ordered}, nil
}

// descriptorVariantStyle returns the style a descriptor-declared variant
// contributes to a slot.
func descriptorVariantStyle(comp *ComponentDescriptor, variant, slot string) (StyleMap, bool) {
	for _, v := range comp.Variants {
		if v.Name != variant {
			continue
		}
		s, ok := v.Slots[slot]
		return s, ok
	}
	return nil, false
}

// variantDeclared reports whether the component declares the named variant.
func variantDeclared(comp *ComponentDescriptor, variant string) bool {
	for _, v := range comp.Variants {
		if v.Name == variant {
			return true
		}
	}
	return false
}

func dedupe(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s == "" {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}
