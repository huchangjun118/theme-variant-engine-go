package theme

import (
	"context"
	"errors"
	"testing"
)

func inheritComp() *ComponentDescriptor {
	return &ComponentDescriptor{
		Name:        "C",
		SpecVersion: "1.0",
		Slots:       []SlotDecl{{Name: "root", Default: StyleMap{"color": "base"}}},
	}
}

func TestBrokenChainDistinctFromCycle(t *testing.T) {
	child := &Theme{
		Name: "child", Version: "1", SpecVersion: "1.0", Inherits: "ghost",
	}
	reg := NewRegistry()
	err := reg.Publish(context.Background(), []*Theme{child}, "child")
	var ie *InheritanceError
	if !errors.As(err, &ie) {
		t.Fatalf("want InheritanceError, got %v", err)
	}
	if ie.Cycle {
		t.Fatalf("broken link must not be reported as cycle: %+v", ie)
	}
	if !errors.Is(err, ErrInheritanceBroken) {
		t.Fatalf("want ErrInheritanceBroken, got %v", err)
	}
}

func TestInheritanceCycleRejected(t *testing.T) {
	a := &Theme{Name: "a", Version: "1", SpecVersion: "1.0", Inherits: "b"}
	b := &Theme{Name: "b", Version: "1", SpecVersion: "1.0", Inherits: "a"}
	reg := NewRegistry()
	err := reg.Publish(context.Background(), []*Theme{a, b}, "a")
	var ie *InheritanceError
	if !errors.As(err, &ie) || !ie.Cycle {
		t.Fatalf("want cyclic InheritanceError, got %v", err)
	}
	if !errors.Is(err, ErrInheritanceCycle) {
		t.Fatalf("want ErrInheritanceCycle, got %v", err)
	}
	if len(ie.Chain) < 3 {
		t.Fatalf("cycle diagnostic must show the loop: %+v", ie.Chain)
	}
}

func TestFailedPublishKeepsPreviousSnapshot(t *testing.T) {
	reg := NewRegistry()
	good := &Theme{Name: "good", Version: "1", SpecVersion: "1.0",
		Components: map[string]*ComponentTheme{
			"C": {Slots: map[string]SlotOverride{"root": {Style: StyleMap{"color": "good"}}}},
		}}
	mustPublish(t, reg, good)
	epochBefore := reg.Epoch()

	bad := &Theme{Name: "bad", Version: "1", SpecVersion: "1.0", Inherits: "nope"}
	if err := reg.Publish(context.Background(), []*Theme{bad}, "bad"); err == nil {
		t.Fatal("expected failed publish")
	}
	if reg.Epoch() != epochBefore {
		t.Fatal("epoch changed on failed publish")
	}
	if reg.DefaultThemeName() != "good" {
		t.Fatalf("default changed after failed publish: %q", reg.DefaultThemeName())
	}
	eng := NewEngine(reg)
	res, err := eng.Resolve(context.Background(), inheritComp(), InstanceInput{}, ResolveOptions{})
	if err != nil {
		t.Fatalf("old theme must remain usable: %v", err)
	}
	if res.Slots["root"].Style["color"] != "good" {
		t.Fatalf("old snapshot polluted: %#v", res.Slots["root"].Style)
	}
}

func TestParentChildMergeAndTokens(t *testing.T) {
	parent := &Theme{
		Name: "parent", Version: "p1", SpecVersion: "1.0",
		Tokens: map[string]any{"a": 1, "shared": "parent-token"},
		Components: map[string]*ComponentTheme{
			"C": {Slots: map[string]SlotOverride{"root": {Style: StyleMap{
				"color":  "parent",
				"kept":   "p",
				"nested": StyleMap{"x": 1, "y": 2},
			}}}},
		},
	}
	child := &Theme{
		Name: "child", Version: "c1", SpecVersion: "1.0", Inherits: "parent",
		Tokens: map[string]any{"shared": "child-token", "b": 2},
		Components: map[string]*ComponentTheme{
			"C": {Slots: map[string]SlotOverride{"root": {Style: StyleMap{
				"color":  "child",
				"nested": StyleMap{"x": 9},
			}}}},
		},
	}
	reg := NewRegistry(WithCacheSize(0))
	mustPublish(t, reg, parent, child)
	eng := NewEngine(reg)
	res, err := eng.Resolve(context.Background(), inheritComp(), InstanceInput{}, ResolveOptions{ThemeName: "child"})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	style := res.Slots["root"].Style
	if style["color"] != "child" || style["kept"] != "p" {
		t.Fatalf("parent/child merge wrong: %#v", style)
	}
	nested := asTestMap(style["nested"])
	if toI(nested["x"]) != 9 || toI(nested["y"]) != 2 {
		t.Fatalf("cross-theme field merge wrong: %#v", nested)
	}
	if res.ThemeVersion != "c1" {
		t.Fatalf("theme version = %q", res.ThemeVersion)
	}
	// child token overrides parent token
	tokenComp := &ComponentDescriptor{
		Name: "T", SpecVersion: "1.0",
		Slots: []SlotDecl{{Name: "root", Default: StyleMap{"v": "{token:shared}"}}},
	}
	res2, err := eng.Resolve(context.Background(), tokenComp, InstanceInput{}, ResolveOptions{ThemeName: "child"})
	if err != nil {
		t.Fatalf("token resolve: %v", err)
	}
	if res2.Slots["root"].Style["v"] != "child-token" {
		t.Fatalf("token override wrong: %v", res2.Slots["root"].Style["v"])
	}
}

func TestInstanceBeatsInheritance(t *testing.T) {
	parent := &Theme{Name: "p", Version: "1", SpecVersion: "1.0",
		Components: map[string]*ComponentTheme{"C": {Slots: map[string]SlotOverride{
			"root": {Style: StyleMap{"color": "theme"}},
		}}}}
	reg := NewRegistry(WithCacheSize(0))
	mustPublish(t, reg, parent)
	eng := NewEngine(reg)
	res, err := eng.Resolve(context.Background(), inheritComp(),
		InstanceInput{Slots: map[string]StyleMap{"root": {"color": "me"}}},
		ResolveOptions{OuterContext: map[string]StyleMap{"root": {"color": "outer"}}})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if res.Slots["root"].Style["color"] != "me" {
		t.Fatalf("instance must beat inherited: %v", res.Slots["root"].Style["color"])
	}
}
