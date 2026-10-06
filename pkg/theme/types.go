package theme

// Layer identifies one of the deterministic override layers. Values from
// higher-numbered layers win over lower-numbered layers.
type Layer int

const (
	LayerUnknown Layer = iota
	// LayerBase is the component descriptor's own default style.
	LayerBase
	// LayerBaseVariant is a variant declared by the component descriptor.
	LayerBaseVariant
	// LayerTheme is the active theme's component/slot override.
	LayerTheme
	// LayerThemeVariant is a variant override carried by the theme.
	LayerThemeVariant
	// LayerInherit is a value inherited from an outer component context.
	LayerInherit
	// LayerInstance is a value passed directly at resolve() call time.
	LayerInstance
)

func (l Layer) String() string {
	switch l {
	case LayerBase:
		return "base"
	case LayerBaseVariant:
		return "base-variant"
	case LayerTheme:
		return "theme"
	case LayerThemeVariant:
		return "theme-variant"
	case LayerInherit:
		return "inherit"
	case LayerInstance:
		return "instance"
	default:
		return "unknown"
	}
}

// StyleMap is a bag of style fields. Values may be string, float64/int, bool,
// nil, nested map[string]any, or []any. Strings may contain references of the
// form "{token:path.to.token}" or "{ref:slot.style.key}".
type StyleMap map[string]any

// SlotDecl declares a named slot and its default style.
type SlotDecl struct {
	Name    string
	Default StyleMap
}

// VariantDecl declares a named variant and the per-slot styles it applies.
type VariantDecl struct {
	Name  string
	Slots map[string]StyleMap
}

// ComponentDescriptor describes a themeable component.
type ComponentDescriptor struct {
	Name        string
	SpecVersion string
	Slots       []SlotDecl
	Variants    []VariantDecl

	// DefaultStyles is DEPRECATED (spec <= 1.1): used as the default of the
	// "root" slot when that slot declares no default of its own.
	DefaultStyles StyleMap
}

// SlotOverride is one layer's override of a single slot. When Replace is true
// the override wholesale replaces the accumulated slot style instead of
// being field-merged.
type SlotOverride struct {
	Style   StyleMap
	Replace bool
}

// ComponentTheme is one theme's opinion about one component: base slot
// overrides plus per-variant slot overrides.
type ComponentTheme struct {
	Slots    map[string]SlotOverride
	Variants map[string]map[string]SlotOverride
}

// Theme is a named, versioned collection of component overrides. A theme may
// inherit one parent theme; the chain is resolved at publish time.
type Theme struct {
	Name        string
	Version     string
	SpecVersion string
	Inherits    string
	Components  map[string]*ComponentTheme
	Tokens      map[string]any

	// StrictDeprecated, when true, turns deprecated-field usage into a hard
	// parse error instead of a warning.
	StrictDeprecated bool
}

// InstanceInput carries per-resolve() caller input: enabled variants and
// per-slot instance styles.
type InstanceInput struct {
	Variants []string
	Slots    map[string]StyleMap
}

// ResolveOptions configures a single resolution.
type ResolveOptions struct {
	// ThemeName selects the active theme; "" means the registry default.
	ThemeName string
	// VariantOrder pins variant application order; otherwise variants are
	// applied in descriptor declaration order / sorted for theme-supplied ones.
	VariantOrder []string
	// OuterContext supplies inherited slot styles from an outer component.
	OuterContext map[string]StyleMap
	// OuterComponent names the outer component for provenance messages.
	OuterComponent string
}

// Source records where a resolved value came from.
type Source struct {
	Layer    Layer
	Theme    string
	Variant  string
	Slot     string
	KeyPath  string
	Resolved []Source // when the value is a reference, provenance of its target
}

// ResolvedSlot is one slot's fully resolved style plus per-field provenance.
type ResolvedSlot struct {
	Name   string
	Style  StyleMap
	Source map[string]Source
}

// ResolvedComponent is the deterministic result of resolving one component.
type ResolvedComponent struct {
	Component string
	Theme     string
	// ThemeVersion is the exact theme version snapshot that produced this result.
	ThemeVersion string
	Slots        map[string]ResolvedSlot
	Warnings     []Warning
}

// ProvenanceFor returns the source recorded for a dotted key path in a slot,
// or false when the slot or path does not exist.
func (r *ResolvedComponent) ProvenanceFor(slot, keyPath string) (Source, bool) {
	s, ok := r.Slots[slot]
	if !ok {
		return Source{}, false
	}
	src, ok := s.Source[keyPath]
	return src, ok
}
