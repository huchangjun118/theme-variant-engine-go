package theme

import (
	"context"
	"fmt"
	"maps"
)

// Engine resolves components against a Registry. All resolutions use an
// immutable theme snapshot, so concurrent Resolve calls are safe and a hot
// replacement can never make one resolution observe two theme versions.
type Engine struct {
	reg   *Registry
	cache *resultCache
}

// NewEngine builds an Engine backed by the given registry.
func NewEngine(reg *Registry) *Engine {
	if reg == nil {
		reg = NewRegistry()
	}
	return &Engine{
		reg:   reg,
		cache: newResultCache(reg.CacheSize()),
	}
}

// Resolve deterministically resolves one component: it validates variants and
// slots, merges the base/base-variant/theme/theme-variant/inherit/instance
// layers, expands references and returns per-field provenance.
//
// The same descriptor + theme version + input always yields an identical
// result. Errors reject the whole resolution; no partial result is returned.
func (e *Engine) Resolve(ctx context.Context, comp *ComponentDescriptor, input InstanceInput, opts ResolveOptions) (*ResolvedComponent, error) {
	if comp == nil {
		return nil, fmt.Errorf("%w: nil component descriptor", ErrInvalidDescriptor)
	}
	compWarnings, err := ValidateComponent(comp)
	if err != nil {
		return nil, err
	}
	if _, err := planVariants(comp, input, opts); err != nil {
		return nil, err
	}

	// Pin one immutable snapshot for the entire resolution.
	snap := e.reg.currentSnapshot()
	themeName := opts.ThemeName
	if themeName == "" {
		themeName = snap.defaults
	}
	var chain *themeChain
	var themeVersion string
	if themeName != "" {
		var err error
		chain, err = resolveInheritance(snap.themes, themeName)
		if err != nil {
			return nil, err
		}
		themeVersion = chain.themes[len(chain.themes)-1].Version
	}

	key, err := makeCacheKey(comp, themeName, themeVersion, input, opts)
	if err != nil {
		return nil, err
	}
	if r, ok := e.cache.get(key, snap.epoch); ok {
		return cloneResult(r), nil
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// Defensive copies: the caller must not be able to mutate a published
	// descriptor or theme and affect results.
	comp = cloneComponent(comp)
	input = cloneInput(input)
	opts = cloneOptions(opts)

	if err := validateSlotTargets(comp, chain, input, opts); err != nil {
		return nil, err
	}

	plan, err := planVariants(comp, input, opts)
	if err != nil {
		return nil, err
	}

	result := &ResolvedComponent{
		Component:    comp.Name,
		Theme:        themeName,
		ThemeVersion: themeVersion,
		Slots:        map[string]ResolvedSlot{},
	}
	result.Warnings = append(result.Warnings, compWarnings...)
	if themeName != "" {
		result.Warnings = append(result.Warnings, snap.warnings[themeName]...)
	}

	// Merge every slot; any error aborts before reference expansion, so no
	// half-built result is produced.
	for _, slot := range comp.Slots {
		ms, err := mergeLayers(comp, chain, plan, slot, input, opts)
		if err != nil {
			return nil, err
		}
		result.Slots[slot.Name] = ResolvedSlot{
			Name:   slot.Name,
			Style:  ms.style,
			Source: maps.Clone(ms.source),
		}
	}

	tokens := map[string]any{}
	if chain != nil {
		tokens = chainTokens(chain)
	}
	if err := expandReferences(result.Slots, tokens, comp.Name, themeName); err != nil {
		return nil, err
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	e.cache.put(key, snap.epoch, cloneResult(result))
	return result, nil
}

func cloneComponent(c *ComponentDescriptor) *ComponentDescriptor {
	out := &ComponentDescriptor{
		Name:          c.Name,
		SpecVersion:   c.SpecVersion,
		DefaultStyles: deepCopyStyle(c.DefaultStyles),
		Slots:         make([]SlotDecl, len(c.Slots)),
		Variants:      make([]VariantDecl, len(c.Variants)),
	}
	for i, s := range c.Slots {
		out.Slots[i] = SlotDecl{Name: s.Name, Default: deepCopyStyle(s.Default)}
	}
	for i, v := range c.Variants {
		out.Variants[i] = VariantDecl{Name: v.Name, Slots: map[string]StyleMap{}}
		for slotName, st := range v.Slots {
			out.Variants[i].Slots[slotName] = deepCopyStyle(st)
		}
	}
	return out
}

func cloneInput(in InstanceInput) InstanceInput {
	out := InstanceInput{Variants: append([]string(nil), in.Variants...)}
	if in.Slots != nil {
		out.Slots = make(map[string]StyleMap, len(in.Slots))
		for k, v := range in.Slots {
			out.Slots[k] = deepCopyStyle(v)
		}
	}
	return out
}

func cloneOptions(o ResolveOptions) ResolveOptions {
	out := o
	if len(o.VariantOrder) > 0 {
		out.VariantOrder = append([]string(nil), o.VariantOrder...)
	}
	if o.OuterContext != nil {
		out.OuterContext = make(map[string]StyleMap, len(o.OuterContext))
		for k, v := range o.OuterContext {
			out.OuterContext[k] = deepCopyStyle(v)
		}
	}
	return out
}

// cloneResult returns a detached copy of a cached result so callers can mutate
// their result without corrupting the cache or another concurrent resolution.
func cloneResult(r *ResolvedComponent) *ResolvedComponent {
	out := &ResolvedComponent{
		Component:    r.Component,
		Theme:        r.Theme,
		ThemeVersion: r.ThemeVersion,
		Slots:        make(map[string]ResolvedSlot, len(r.Slots)),
		Warnings:     append([]Warning(nil), r.Warnings...),
	}
	for name, slot := range r.Slots {
		cp := ResolvedSlot{
			Name:   slot.Name,
			Style:  deepCopyStyle(slot.Style),
			Source: make(map[string]Source, len(slot.Source)),
		}
		for k, src := range slot.Source {
			cp.Source[k] = cloneSource(src)
		}
		out.Slots[name] = cp
	}
	return out
}

func cloneSource(s Source) Source {
	if len(s.Resolved) > 0 {
		s.Resolved = append([]Source(nil), s.Resolved...)
		for i := range s.Resolved {
			s.Resolved[i] = cloneSource(s.Resolved[i])
		}
	}
	return s
}

// ValidateComponent checks a standalone component descriptor: spec version
// range, duplicate slots/variants and deprecated-field semantics.
func ValidateComponent(comp *ComponentDescriptor) ([]Warning, error) {
	if comp == nil {
		return nil, fmt.Errorf("%w: nil component descriptor", ErrInvalidDescriptor)
	}
	var warnings []Warning
	if err := CheckSpecVersion("component", comp.Name, comp.SpecVersion); err != nil {
		return nil, err
	}
	if comp.Name == "" {
		return nil, fmt.Errorf("%w: component has no name", ErrInvalidDescriptor)
	}
	if len(comp.Slots) == 0 {
		return nil, fmt.Errorf("%w: component %q declares no slots", ErrInvalidDescriptor, comp.Name)
	}
	slots := map[string]struct{}{}
	for _, s := range comp.Slots {
		if s.Name == "" {
			return nil, fmt.Errorf("%w: component %q has a slot with empty name", ErrInvalidDescriptor, comp.Name)
		}
		if _, dup := slots[s.Name]; dup {
			return nil, fmt.Errorf("%w: component %q declares duplicate slot %q",
				ErrInvalidDescriptor, comp.Name, s.Name)
		}
		slots[s.Name] = struct{}{}
	}
	variants := map[string]struct{}{}
	for _, v := range comp.Variants {
		if v.Name == "" {
			return nil, fmt.Errorf("%w: component %q has a variant with empty name", ErrInvalidDescriptor, comp.Name)
		}
		if _, dup := variants[v.Name]; dup {
			return nil, fmt.Errorf("%w: component %q declares duplicate variant %q",
				ErrInvalidDescriptor, comp.Name, v.Name)
		}
		variants[v.Name] = struct{}{}
		for slotName := range v.Slots {
			if _, ok := slots[slotName]; !ok {
				return nil, &SlotError{Component: comp.Name, Slot: slotName, Layer: LayerBaseVariant}
			}
		}
	}
	if comp.DefaultStyles != nil {
		// Deprecated field: DEFINED semantics — it seeds the "root" slot
		// default (only when that slot has no explicit default). It is never
		// silently ignored.
		w := Warning{
			Kind:    "deprecated",
			Path:    "defaultStyles",
			Message: "defaultStyles is deprecated (removed after spec 1.1); declare a \"root\" slot default instead",
		}
		warnings = append(warnings, w)
	}
	return warnings, nil
}

// ValidateTheme checks a standalone theme descriptor before publication.
func ValidateTheme(t *Theme) ([]Warning, error) {
	if t == nil {
		return nil, fmt.Errorf("%w: nil theme", ErrInvalidDescriptor)
	}
	if err := CheckSpecVersion("theme", t.Name, t.SpecVersion); err != nil {
		return nil, err
	}
	if t.Name == "" {
		return nil, fmt.Errorf("%w: theme has no name", ErrInvalidDescriptor)
	}
	if t.Version == "" {
		return nil, fmt.Errorf("%w: theme %q has no version", ErrInvalidDescriptor, t.Name)
	}
	for compName, ct := range t.Components {
		if compName == "" {
			return nil, fmt.Errorf("%w: theme %q has a component override with empty name", ErrInvalidDescriptor, t.Name)
		}
		if ct == nil {
			return nil, fmt.Errorf("%w: theme %q has nil component override %q", ErrInvalidDescriptor, t.Name, compName)
		}
		for variant := range ct.Variants {
			if variant == "" {
				return nil, fmt.Errorf("%w: theme %q component %q has an unnamed variant",
					ErrInvalidDescriptor, t.Name, compName)
			}
		}
	}
	return nil, nil
}
