package theme

// mergedSlot tracks a slot style together with per-leaf provenance while
// layers are applied in ascending priority.
type mergedSlot struct {
	style  StyleMap
	source map[string]Source
}

func newMergedSlot() *mergedSlot {
	return &mergedSlot{
		style:  StyleMap{},
		source: map[string]Source{},
	}
}

// mergeField applies one incoming style map as the next higher-priority layer
// using FIELD-LEVEL merge semantics:
//   - nested maps are merged key by key (recursively);
//   - scalars, booleans, null and arrays replace the leaf wholesale;
//   - every overwritten leaf records src under its dotted path.
func (m *mergedSlot) mergeField(in StyleMap, src Source) {
	mergeMap(m.style, in, "", src, m.source)
}

func mergeMap(dst, src map[string]any, prefix string, s Source, provenance map[string]Source) {
	for k, v := range src {
		path := k
		if prefix != "" {
			path = prefix + "." + k
		}
		if vm, ok := asMap(v); ok {
			if exist, ok := asMap(dst[k]); ok {
				mergeMap(exist, vm, path, s, provenance)
				continue
			}
			cp := deepCopyValue(vm).(map[string]any)
			dst[k] = cp
			recordLeaves(cp, path, s, provenance)
			continue
		}
		dst[k] = deepCopyValue(v)
		provenance[path] = s
	}
}

// asMap accepts both map[string]any and StyleMap (named type, same underlying
// type) so nested style maps merge field by field regardless of concrete type.
func asMap(v any) (map[string]any, bool) {
	switch x := v.(type) {
	case map[string]any:
		return x, true
	case StyleMap:
		return map[string]any(x), true
	default:
		return nil, false
	}
}

func recordLeaves(v map[string]any, prefix string, s Source, provenance map[string]Source) {
	for k, vv := range v {
		path := k
		if prefix != "" {
			path = prefix + "." + k
		}
		if vm, ok := asMap(vv); ok {
			recordLeaves(vm, path, s, provenance)
		} else {
			provenance[path] = s
		}
	}
}

// replaceAll implements WHOLESALE replacement semantics: every style
// accumulated from lower-priority layers is discarded and in starts afresh.
func (m *mergedSlot) replaceAll(in StyleMap, src Source) {
	m.style = StyleMap{}
	m.source = map[string]Source{}
	m.mergeField(in, src)
}

// applyOverride honors the Replace marker carried by a theme layer.
func (m *mergedSlot) applyOverride(o SlotOverride, src Source) {
	if o.Replace {
		m.replaceAll(o.Style, src)
	} else {
		m.mergeField(o.Style, src)
	}
}

// mergeLayers applies every layer for one slot in deterministic order:
//
//	base < base variants < theme base < theme variants < inherited < instance
//
// within the theme sub-layers, parent themes precede child themes and child
// values win; within a layer, later variants in the planned order win.
//
// Referencing an undeclared variant yields a VariantError naming this slot.
func mergeLayers(
	comp *ComponentDescriptor,
	chain *themeChain,
	plan *variantPlan,
	slot SlotDecl,
	input InstanceInput,
	opts ResolveOptions,
) (*mergedSlot, error) {
	m := newMergedSlot()

	// Layer 1: component base defaults.
	base := slot.Default
	if len(base) == 0 && slot.Name == "root" && len(comp.DefaultStyles) > 0 {
		base = comp.DefaultStyles // deprecated field, see ValidateComponent
	}
	m.mergeField(base, Source{Layer: LayerBase, Slot: slot.Name})

	// Layer 2: descriptor-declared variants, in planned order.
	for _, variant := range plan.order {
		if st, ok := descriptorVariantStyle(comp, variant, slot.Name); ok {
			m.mergeField(st, Source{
				Layer: LayerBaseVariant, Slot: slot.Name, Variant: variant,
			})
		}
	}

	// A requested variant that the component never declares is an error;
	// attribute it to the slot currently being resolved (slots are iterated
	// in declaration order, so the first affected slot is deterministic).
	for _, variant := range plan.order {
		if !variantDeclared(comp, variant) {
			return nil, &VariantError{
				Component: comp.Name,
				Slot:      slot.Name,
				Variant:   variant,
				Layer:     LayerBaseVariant,
			}
		}
	}

	// Theme layers exist only when the component resolves against a theme.
	if chain != nil {
		// Layer 3: theme base overrides, parent first.
		for _, t := range chain.themes {
			ct := t.Components[comp.Name]
			if ct == nil {
				continue
			}
			if o, ok := ct.Slots[slot.Name]; ok {
				m.applyOverride(o, Source{
					Layer: LayerTheme, Theme: t.Name, Slot: slot.Name,
				})
			}
		}

		// Layer 4: theme variant overrides, parent first, then plan order.
		for _, t := range chain.themes {
			ct := t.Components[comp.Name]
			if ct == nil {
				continue
			}
			for _, variant := range plan.order {
				vs, ok := ct.Variants[variant]
				if !ok {
					continue
				}
				if o, ok := vs[slot.Name]; ok {
					m.applyOverride(o, Source{
						Layer: LayerThemeVariant, Theme: t.Name,
						Slot: slot.Name, Variant: variant,
					})
				}
			}
		}
	}

	// Layer 5: values inherited from an outer component context.
	if outer, ok := opts.OuterContext[slot.Name]; ok {
		m.mergeField(outer, Source{
			Layer: LayerInherit,
			Theme: opts.OuterComponent,
			Slot:  slot.Name,
		})
	}

	// Layer 6: instance-supplied styles always win.
	if in, ok := input.Slots[slot.Name]; ok {
		m.mergeField(in, Source{Layer: LayerInstance, Slot: slot.Name})
	}

	return m, nil
}

// validateSlotTargets rejects theme and instance styles that name slots the
// component does not declare.
func validateSlotTargets(comp *ComponentDescriptor, chain *themeChain, input InstanceInput, opts ResolveOptions) error {
	declared := make(map[string]struct{}, len(comp.Slots))
	for _, s := range comp.Slots {
		declared[s.Name] = struct{}{}
	}
	if chain != nil {
		for _, t := range chain.themes {
			ct := t.Components[comp.Name]
			if ct == nil {
				continue
			}
			for name := range ct.Slots {
				if _, ok := declared[name]; !ok {
					return &SlotError{Component: comp.Name, Slot: name, Layer: LayerTheme}
				}
			}
			for variant, slots := range ct.Variants {
				for name := range slots {
					if _, ok := declared[name]; !ok {
						return &SlotError{Component: comp.Name, Slot: name, Layer: LayerThemeVariant}
					}
				}
				if !variantDeclared(comp, variant) {
					for _, s := range comp.Slots {
						if _, ok := slots[s.Name]; ok {
							return &VariantError{
								Component: comp.Name, Slot: s.Name,
								Variant: variant, Layer: LayerThemeVariant,
							}
						}
					}
					return &VariantError{
						Component: comp.Name,
						Slot:      firstSlotName(comp),
						Variant:   variant,
						Layer:     LayerThemeVariant,
					}
				}
			}
		}
	}
	for name := range input.Slots {
		if _, ok := declared[name]; !ok {
			return &SlotError{Component: comp.Name, Slot: name, Layer: LayerInstance}
		}
	}
	for name := range opts.OuterContext {
		if _, ok := declared[name]; !ok {
			return &SlotError{Component: comp.Name, Slot: name, Layer: LayerInherit}
		}
	}
	return nil
}

func firstSlotName(comp *ComponentDescriptor) string {
	if len(comp.Slots) > 0 {
		return comp.Slots[0].Name
	}
	return ""
}

func deepCopyStyle(in StyleMap) StyleMap {
	if in == nil {
		return nil
	}
	return StyleMap(deepCopyValue(map[string]any(in)).(map[string]any))
}
