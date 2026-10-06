package theme

import (
	"context"
	"errors"
	"testing"
)

func TestSpecVersionRange(t *testing.T) {
	cases := []struct {
		v       string
		wantErr bool
	}{
		{"1.0", false},
		{"1.4", false},
		{"1.2.3", false},
		{"0.9", true},
		{"2.0", true},
		{"1.5", true},
		{"", true},
		{"1.x", true},
	}
	for _, c := range cases {
		err := CheckSpecVersion("component", "X", c.v)
		if c.wantErr && err == nil {
			t.Errorf("version %q accepted, want rejection", c.v)
		}
		if !c.wantErr && err != nil {
			t.Errorf("version %q rejected: %v", c.v, err)
		}
		if c.wantErr && !errors.Is(err, ErrSpecVersion) {
			t.Errorf("version %q error not ErrSpecVersion: %v", c.v, err)
		}
	}
}

func TestRejectHighVersionOnComponent(t *testing.T) {
	reg := NewRegistry()
	mustPublish(t, reg, &Theme{Name: "t", Version: "1", SpecVersion: "1.0"})
	eng := NewEngine(reg)
	comp := &ComponentDescriptor{
		Name: "Future", SpecVersion: "3.0",
		Slots: []SlotDecl{{Name: "root"}},
	}
	_, err := eng.Resolve(context.Background(), comp, InstanceInput{}, ResolveOptions{})
	var ve *VersionError
	if !errors.As(err, &ve) {
		t.Fatalf("want VersionError, got %v", err)
	}
	if ve.Kind != "component" {
		t.Fatalf("version error kind = %q", ve.Kind)
	}
}

func TestRejectHighVersionOnThemePublish(t *testing.T) {
	reg := NewRegistry()
	bad := &Theme{Name: "future", Version: "1", SpecVersion: "9.9"}
	err := reg.Publish(context.Background(), []*Theme{bad}, "future")
	if !errors.Is(err, ErrSpecVersion) {
		t.Fatalf("want ErrSpecVersion, got %v", err)
	}
}

func TestDeprecatedDefaultStylesHasDefinedSemantics(t *testing.T) {
	comp := &ComponentDescriptor{
		Name: "Old", SpecVersion: "1.0",
		Slots:         []SlotDecl{{Name: "root"}},
		DefaultStyles: StyleMap{"color": "legacy"},
	}
	ws, err := ValidateComponent(comp)
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	found := false
	for _, w := range ws {
		if w.Kind == "deprecated" && w.Path == "defaultStyles" {
			found = true
		}
	}
	if !found {
		t.Fatalf("deprecated field must produce a warning, got %+v", ws)
	}

	// Defined behavior: seeds root slot default when root declares none.
	reg := NewRegistry(WithCacheSize(0))
	mustPublish(t, reg, &Theme{Name: "t", Version: "1", SpecVersion: "1.0"})
	eng := NewEngine(reg)
	res, err := eng.Resolve(context.Background(), comp, InstanceInput{}, ResolveOptions{})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if res.Slots["root"].Style["color"] != "legacy" {
		t.Fatalf("deprecated defaultStyles ignored: %#v", res.Slots["root"].Style)
	}
	if res.Slots["root"].Source["color"].Layer != LayerBase {
		t.Fatalf("deprecated seed should be base layer provenance")
	}
	if len(res.Warnings) == 0 {
		t.Fatal("resolved result should surface the deprecation warning")
	}
}

func TestDuplicateSlotsAndVariantsRejected(t *testing.T) {
	_, err := ValidateComponent(&ComponentDescriptor{
		Name: "X", SpecVersion: "1.0",
		Slots: []SlotDecl{{Name: "root"}, {Name: "root"}},
	})
	if !errors.Is(err, ErrInvalidDescriptor) {
		t.Fatalf("dup slots: %v", err)
	}
	_, err = ValidateComponent(&ComponentDescriptor{
		Name: "X", SpecVersion: "1.0",
		Slots: []SlotDecl{{Name: "root"}},
		Variants: []VariantDecl{
			{Name: "v"}, {Name: "v"},
		},
	})
	if !errors.Is(err, ErrInvalidDescriptor) {
		t.Fatalf("dup variants: %v", err)
	}
}

func TestEmptyComponentRejected(t *testing.T) {
	_, err := ValidateComponent(&ComponentDescriptor{Name: "X", SpecVersion: "1.0"})
	if !errors.Is(err, ErrInvalidDescriptor) {
		t.Fatalf("slotless component: %v", err)
	}
}
