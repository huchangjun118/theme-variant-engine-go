package theme

import (
	"context"
	"errors"
	"testing"
)

func testButton() *ComponentDescriptor {
	return &ComponentDescriptor{
		Name:        "Button",
		SpecVersion: "1.0",
		Slots: []SlotDecl{
			{Name: "root", Default: StyleMap{
				"color":   "black",
				"padding": StyleMap{"x": 1, "y": 2},
				"kept":    "base",
			}},
			{Name: "icon", Default: StyleMap{"size": 10}},
		},
		Variants: []VariantDecl{
			{Name: "primary", Slots: map[string]StyleMap{
				"root": {"color": "blue"},
			}},
			{Name: "large", Slots: map[string]StyleMap{
				"root": {"padding": StyleMap{"x": 8}},
				"icon": {"size": 20},
			}},
		},
	}
}

func mustPublish(t *testing.T, reg *Registry, themes ...*Theme) {
	t.Helper()
	if len(themes) == 0 {
		t.Fatal("no themes")
	}
	if err := reg.Publish(context.Background(), themes, themes[0].Name); err != nil {
		t.Fatalf("publish: %v", err)
	}
}

func TestVariantCombinationDeterministic(t *testing.T) {
	reg := NewRegistry(WithCacheSize(0))
	mustPublish(t, reg, &Theme{Name: "t", Version: "1", SpecVersion: "1.0"})
	eng := NewEngine(reg)

	comp := testButton()
	resA, err := eng.Resolve(context.Background(), comp, InstanceInput{
		Variants: []string{"primary", "large"},
	}, ResolveOptions{})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	// Repeated resolution with reversed request order must be identical
	// because order follows declaration order.
	resB, err := eng.Resolve(context.Background(), testButton(), InstanceInput{
		Variants: []string{"large", "primary"},
	}, ResolveOptions{})
	if err != nil {
		t.Fatalf("resolve2: %v", err)
	}
	if !styleEqual(resA.Slots["root"].Style, resB.Slots["root"].Style) {
		t.Fatalf("non-deterministic: %#v vs %#v", resA.Slots["root"].Style, resB.Slots["root"].Style)
	}
	root := resA.Slots["root"].Style
	if root["color"] != "blue" {
		t.Fatalf("color = %v want blue", root["color"])
	}
	pad := asTestMap(root["padding"])
	if toI(pad["x"]) != 8 || toI(pad["y"]) != 2 {
		t.Fatalf("nested field merge wrong: %#v", pad)
	}
	if root["kept"] != "base" {
		t.Fatalf("base leaf dropped: %#v", root)
	}
	if toI(resA.Slots["icon"].Style["size"]) != 20 {
		t.Fatalf("variant did not reach icon slot")
	}
}

func TestUndeclaredVariantNamesSlotAndVariant(t *testing.T) {
	reg := NewRegistry(WithCacheSize(0))
	mustPublish(t, reg, &Theme{Name: "t", Version: "1", SpecVersion: "1.0"})
	eng := NewEngine(reg)

	_, err := eng.Resolve(context.Background(), testButton(), InstanceInput{
		Variants: []string{"ghost"},
	}, ResolveOptions{})
	var ve *VariantError
	if !errors.As(err, &ve) {
		t.Fatalf("want *VariantError, got %v", err)
	}
	if ve.Slot != "root" || ve.Variant != "ghost" {
		t.Fatalf("error does not name slot/variant: %+v", ve)
	}
	if !errors.Is(err, ErrUndeclaredVariant) {
		t.Fatalf("want ErrUndeclaredVariant sentinel, got %v", err)
	}
}

func TestLayerPrecedenceAndProvenance(t *testing.T) {
	theme := &Theme{
		Name: "t", Version: "1", SpecVersion: "1.0",
		Components: map[string]*ComponentTheme{
			"Button": {
				Slots: map[string]SlotOverride{
					"root": {Style: StyleMap{"color": "themed", "radius": 4}},
				},
				Variants: map[string]map[string]SlotOverride{
					"primary": {"root": {Style: StyleMap{"color": "theme-primary"}}},
				},
			},
		},
	}
	reg := NewRegistry(WithCacheSize(0))
	mustPublish(t, reg, theme)
	eng := NewEngine(reg)

	res, err := eng.Resolve(context.Background(), testButton(),
		InstanceInput{
			Variants: []string{"primary"},
			Slots:    map[string]StyleMap{"root": {"color": "instance-color"}},
		},
		ResolveOptions{
			OuterContext:   map[string]StyleMap{"root": {"color": "inherited", "outerOnly": 1}},
			OuterComponent: "Panel",
		})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	src := res.Slots["root"].Source
	cases := []struct {
		key  string
		want Layer
	}{
		{"kept", LayerBase},
		{"radius", LayerTheme},
		{"color", LayerInstance},
		{"outerOnly", LayerInherit},
	}
	for _, c := range cases {
		got, ok := src[c.key]
		if !ok {
			t.Fatalf("no provenance for %q", c.key)
		}
		if got.Layer != c.want {
			t.Fatalf("%q provenance = %s, want %s", c.key, got.Layer, c.want)
		}
	}
	if res.Slots["root"].Style["color"] != "instance-color" {
		t.Fatalf("instance must win, got %v", res.Slots["root"].Style["color"])
	}
	if res.Slots["root"].Source["color"].Slot != "root" {
		t.Fatalf("provenance missing slot name")
	}
}

func TestInheritedBeatsThemeButInstanceBeatsInherited(t *testing.T) {
	theme := &Theme{
		Name: "t", Version: "1", SpecVersion: "1.0",
		Components: map[string]*ComponentTheme{
			"Button": {Slots: map[string]SlotOverride{
				"root": {Style: StyleMap{"a": "theme", "b": "theme", "c": "theme"}},
			}},
		},
	}
	reg := NewRegistry(WithCacheSize(0))
	mustPublish(t, reg, theme)
	eng := NewEngine(reg)

	res, err := eng.Resolve(context.Background(),
		&ComponentDescriptor{
			Name: "Button", SpecVersion: "1.0",
			Slots: []SlotDecl{{Name: "root"}},
		},
		InstanceInput{Slots: map[string]StyleMap{"root": {"a": "instance"}}},
		ResolveOptions{OuterContext: map[string]StyleMap{
			"root": {"a": "inherit", "b": "inherit"},
		}})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	style := res.Slots["root"].Style
	if style["a"] != "instance" || style["b"] != "inherit" || style["c"] != "theme" {
		t.Fatalf("precedence wrong: %#v", style)
	}
}

func TestThemeReplaceSemantics(t *testing.T) {
	parent := &Theme{
		Name: "parent", Version: "1", SpecVersion: "1.0",
		Components: map[string]*ComponentTheme{
			"Button": {Slots: map[string]SlotOverride{
				"root": {Style: StyleMap{"a": 1, "nested": StyleMap{"x": 1, "y": 2}}},
			}},
		},
	}
	child := &Theme{
		Name: "child", Version: "1", SpecVersion: "1.0", Inherits: "parent",
		Components: map[string]*ComponentTheme{
			"Button": {Slots: map[string]SlotOverride{
				// Wholesale replacement discards every base/parent leaf.
				"root": {Replace: true, Style: StyleMap{"z": 9}},
			}},
		},
	}
	reg := NewRegistry(WithCacheSize(0))
	mustPublish(t, reg, parent, child)
	eng := NewEngine(reg)

	comp := &ComponentDescriptor{
		Name: "Button", SpecVersion: "1.0",
		Slots: []SlotDecl{{Name: "root", Default: StyleMap{"baseOnly": true}}},
	}
	res, err := eng.Resolve(context.Background(), comp, InstanceInput{}, ResolveOptions{ThemeName: "child"})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	style := res.Slots["root"].Style
	if _, ok := style["a"]; ok {
		t.Fatalf("replace must discard parent leaf, got %#v", style)
	}
	if _, ok := style["nested"]; ok {
		t.Fatalf("replace must discard parent map, got %#v", style)
	}
	if _, ok := style["baseOnly"]; ok {
		t.Fatalf("theme replace resets base too, got %#v", style)
	}
	if toI(style["z"]) != 9 {
		t.Fatalf("replace value missing: %#v", style)
	}
}

func TestUnknownSlotRejected(t *testing.T) {
	theme := &Theme{
		Name: "t", Version: "1", SpecVersion: "1.0",
		Components: map[string]*ComponentTheme{
			"Button": {Slots: map[string]SlotOverride{
				"mystery": {Style: StyleMap{"x": 1}},
			}},
		},
	}
	reg := NewRegistry(WithCacheSize(0))
	mustPublish(t, reg, theme)
	eng := NewEngine(reg)
	_, err := eng.Resolve(context.Background(), testButton(), InstanceInput{}, ResolveOptions{})
	var se *SlotError
	if !errors.As(err, &se) || se.Slot != "mystery" {
		t.Fatalf("want SlotError naming mystery, got %v", err)
	}
}

func toI(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case float64:
		return int(n)
	default:
		return -999999
	}
}
