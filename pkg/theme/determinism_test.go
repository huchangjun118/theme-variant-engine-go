package theme

import (
	"context"
	"testing"
)

func TestSameInputSameOutputAcrossEngines(t *testing.T) {
	th := &Theme{
		Name: "t", Version: "1", SpecVersion: "1.0",
		Tokens: map[string]any{"c": "red"},
		Components: map[string]*ComponentTheme{"Button": {
			Slots: map[string]SlotOverride{"root": {Style: StyleMap{"color": "{token:c}"}}},
			Variants: map[string]map[string]SlotOverride{
				"primary": {"root": {Style: StyleMap{"bold": true}}},
			},
		}},
	}
	resolve := func() *ResolvedComponent {
		reg := NewRegistry(WithCacheSize(0))
		mustPublish(t, reg, th)
		eng := NewEngine(reg)
		r, err := eng.Resolve(context.Background(), testButton(),
			InstanceInput{Variants: []string{"primary"}}, ResolveOptions{})
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	a, b := resolve(), resolve()
	if !styleEqual(a.Slots, b.Slots) {
		t.Fatalf("identical descriptor+theme+input gave different results:\n%#v\n%#v", a.Slots, b.Slots)
	}
}

func TestPinnedVariantOrder(t *testing.T) {
	// Both variants touch padding.x; later in order wins.
	comp := &ComponentDescriptor{
		Name: "C", SpecVersion: "1.0",
		Slots: []SlotDecl{{Name: "root"}},
		Variants: []VariantDecl{
			{Name: "a", Slots: map[string]StyleMap{"root": {"x": 1}}},
			{Name: "b", Slots: map[string]StyleMap{"root": {"x": 2}}},
		},
	}
	reg := NewRegistry(WithCacheSize(0))
	mustPublish(t, reg, &Theme{Name: "t", Version: "1", SpecVersion: "1.0"})
	eng := NewEngine(reg)

	r1, err := eng.Resolve(context.Background(), comp,
		InstanceInput{Variants: []string{"a", "b"}},
		ResolveOptions{VariantOrder: []string{"a", "b"}})
	if err != nil {
		t.Fatal(err)
	}
	if toI(r1.Slots["root"].Style["x"]) != 2 {
		t.Fatalf("order a,b: x=%v", r1.Slots["root"].Style["x"])
	}
	r2, err := eng.Resolve(context.Background(), comp,
		InstanceInput{Variants: []string{"a", "b"}},
		ResolveOptions{VariantOrder: []string{"b", "a"}})
	if err != nil {
		t.Fatal(err)
	}
	if toI(r2.Slots["root"].Style["x"]) != 1 {
		t.Fatalf("pinned order b,a: x=%v", r2.Slots["root"].Style["x"])
	}

	// Order that doesn't match the requested set is rejected.
	_, err = eng.Resolve(context.Background(), comp,
		InstanceInput{Variants: []string{"a"}},
		ResolveOptions{VariantOrder: []string{"a", "b"}})
	if err == nil {
		t.Fatal("mismatched variantOrder must error")
	}
}

func TestStyleImmutabilityAfterResolve(t *testing.T) {
	comp := testButton()
	reg := NewRegistry(WithCacheSize(0))
	mustPublish(t, reg, &Theme{Name: "t", Version: "1", SpecVersion: "1.0"})
	eng := NewEngine(reg)
	r, err := eng.Resolve(context.Background(), comp, InstanceInput{}, ResolveOptions{})
	if err != nil {
		t.Fatal(err)
	}
	// Mutating the caller's descriptor after resolution must not affect the result.
	comp.Slots[0].Default["color"] = "MUTATED"
	if r.Slots["root"].Style["color"] != "black" {
		t.Fatalf("resolved result aliased mutable descriptor: %v", r.Slots["root"].Style["color"])
	}
}
