package theme

import (
	"context"
	"errors"
	"testing"
)

func refComp(slots ...SlotDecl) *ComponentDescriptor {
	return &ComponentDescriptor{
		Name:        "C",
		SpecVersion: "1.0",
		Slots:       slots,
	}
}

func publishSingle(t *testing.T, th *Theme) (*Engine, *Registry) {
	t.Helper()
	reg := NewRegistry(WithCacheSize(0))
	mustPublish(t, reg, th)
	return NewEngine(reg), reg
}

func TestTokenReferenceResolves(t *testing.T) {
	th := &Theme{
		Name: "t", Version: "1", SpecVersion: "1.0",
		Tokens: map[string]any{"color": map[string]any{"primary": "#0af"}},
		Components: map[string]*ComponentTheme{
			"C": {Slots: map[string]SlotOverride{
				"root": {Style: StyleMap{"bg": "{token:color.primary}"}},
			}},
		},
	}
	eng, _ := publishSingle(t, th)
	res, err := eng.Resolve(context.Background(),
		refComp(SlotDecl{Name: "root", Default: StyleMap{"bg": "x"}}),
		InstanceInput{}, ResolveOptions{})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if res.Slots["root"].Style["bg"] != "#0af" {
		t.Fatalf("token not resolved: %#v", res.Slots["root"].Style)
	}
	src := res.Slots["root"].Source["bg"]
	if len(src.Resolved) != 1 || src.Resolved[0].KeyPath != "token:color.primary" {
		t.Fatalf("reference provenance missing target: %+v", src)
	}
}

func TestWholeReferencePreservesType(t *testing.T) {
	th := &Theme{
		Name: "t", Version: "1", SpecVersion: "1.0",
		Tokens: map[string]any{"radius": 8},
	}
	reg := NewRegistry(WithCacheSize(0))
	mustPublish(t, reg, th)
	eng := NewEngine(reg)
	res, err := eng.Resolve(context.Background(),
		refComp(SlotDecl{Name: "root", Default: StyleMap{"r": "{token:radius}"}}),
		InstanceInput{}, ResolveOptions{})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if toI(res.Slots["root"].Style["r"]) != 8 {
		t.Fatalf("whole reference must preserve numeric type, got %T %v",
			res.Slots["root"].Style["r"], res.Slots["root"].Style["r"])
	}
}

func TestCrossSlotReference(t *testing.T) {
	reg := NewRegistry(WithCacheSize(0))
	mustPublish(t, reg, &Theme{Name: "t", Version: "1", SpecVersion: "1.0"})
	eng := NewEngine(reg)
	comp := refComp(
		SlotDecl{Name: "a", Default: StyleMap{"v": "hello"}},
		SlotDecl{Name: "b", Default: StyleMap{"copy": "{ref:a.v}"}},
	)
	res, err := eng.Resolve(context.Background(), comp, InstanceInput{}, ResolveOptions{})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if res.Slots["b"].Style["copy"] != "hello" {
		t.Fatalf("cross-slot ref = %v", res.Slots["b"].Style["copy"])
	}
}

func TestUndefinedTokenRejectedWholesale(t *testing.T) {
	reg := NewRegistry(WithCacheSize(0))
	mustPublish(t, reg, &Theme{Name: "t", Version: "1", SpecVersion: "1.0"})
	eng := NewEngine(reg)
	comp := refComp(
		SlotDecl{Name: "a", Default: StyleMap{"good": 1}},
		SlotDecl{Name: "b", Default: StyleMap{"bad": "{token:missing.path}"}},
	)
	res, err := eng.Resolve(context.Background(), comp, InstanceInput{}, ResolveOptions{})
	var re *ReferenceError
	if !errors.As(err, &re) || re.Cycle {
		t.Fatalf("want undefined ReferenceError, got %v", err)
	}
	if !errors.Is(err, ErrReferenceUndefined) {
		t.Fatalf("missing sentinel: %v", err)
	}
	if re.Slot != "b" || re.Key != "bad" {
		t.Fatalf("error must name slot/key: %+v", re)
	}
	if res != nil {
		t.Fatalf("no partial result allowed on undefined reference")
	}
}

func TestUndefinedStyleRefRejected(t *testing.T) {
	reg := NewRegistry(WithCacheSize(0))
	mustPublish(t, reg, &Theme{Name: "t", Version: "1", SpecVersion: "1.0"})
	eng := NewEngine(reg)
	comp := refComp(SlotDecl{Name: "root", Default: StyleMap{
		"a": "{ref:root.missing}",
	}})
	_, err := eng.Resolve(context.Background(), comp, InstanceInput{}, ResolveOptions{})
	if !errors.Is(err, ErrReferenceUndefined) {
		t.Fatalf("want undefined, got %v", err)
	}
}

func TestReferenceCycleRejected(t *testing.T) {
	reg := NewRegistry(WithCacheSize(0))
	mustPublish(t, reg, &Theme{Name: "t", Version: "1", SpecVersion: "1.0"})
	eng := NewEngine(reg)
	comp := refComp(SlotDecl{Name: "root", Default: StyleMap{
		"a": "{ref:root.b}",
		"b": "{ref:root.a}",
	}})
	_, err := eng.Resolve(context.Background(), comp, InstanceInput{}, ResolveOptions{})
	var re *ReferenceError
	if !errors.As(err, &re) || !re.Cycle {
		t.Fatalf("want cyclic ReferenceError, got %v", err)
	}
	if !errors.Is(err, ErrReferenceCycle) {
		t.Fatalf("missing cycle sentinel: %v", err)
	}
	if len(re.Path) < 2 {
		t.Fatalf("cycle path should name the nodes: %+v", re)
	}
}

func TestTokenReferenceCycleRejected(t *testing.T) {
	th := &Theme{
		Name: "t", Version: "1", SpecVersion: "1.0",
		Tokens: map[string]any{
			"a": "{token:b}",
			"b": "{token:a}",
		},
	}
	reg := NewRegistry(WithCacheSize(0))
	mustPublish(t, reg, th)
	eng := NewEngine(reg)
	comp := refComp(SlotDecl{Name: "root", Default: StyleMap{"x": "{token:a}"}})
	_, err := eng.Resolve(context.Background(), comp, InstanceInput{}, ResolveOptions{})
	if !errors.Is(err, ErrReferenceCycle) {
		t.Fatalf("want token cycle error, got %v", err)
	}
}

func TestEmbeddedReferenceString(t *testing.T) {
	th := &Theme{
		Name: "t", Version: "1", SpecVersion: "1.0",
		Tokens: map[string]any{"name": "world"},
	}
	reg := NewRegistry(WithCacheSize(0))
	mustPublish(t, reg, th)
	eng := NewEngine(reg)
	comp := refComp(SlotDecl{Name: "root", Default: StyleMap{"label": "hello {token:name}!"}})
	res, err := eng.Resolve(context.Background(), comp, InstanceInput{}, ResolveOptions{})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if res.Slots["root"].Style["label"] != "hello world!" {
		t.Fatalf("embedded ref = %q", res.Slots["root"].Style["label"])
	}
}
