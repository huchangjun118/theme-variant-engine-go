package theme

import (
	"context"
	"testing"
)

func TestCachedResultNotMutableByCaller(t *testing.T) {
	reg := NewRegistry(WithCacheSize(256))
	mustPublish(t, reg, &Theme{Name: "t", Version: "1", SpecVersion: "1.0"})
	eng := NewEngine(reg)
	comp := &ComponentDescriptor{Name: "C", SpecVersion: "1.0",
		Slots: []SlotDecl{{Name: "root", Default: StyleMap{"color": "red"}}}}
	r1, _ := eng.Resolve(context.Background(), comp, InstanceInput{}, ResolveOptions{})
	r1.Slots["root"].Style["color"] = "HACKED"
	r2, _ := eng.Resolve(context.Background(), comp, InstanceInput{}, ResolveOptions{})
	if r2.Slots["root"].Style["color"] != "red" {
		t.Fatalf("cache leaked mutable result: %v", r2.Slots["root"].Style["color"])
	}

	// Two concurrent-looking callers must not share backing maps.
	r3, _ := eng.Resolve(context.Background(), comp, InstanceInput{}, ResolveOptions{})
	r3.Slots["root"].Style["color"] = "OTHER"
	r4, _ := eng.Resolve(context.Background(), comp, InstanceInput{}, ResolveOptions{})
	if r4.Slots["root"].Style["color"] != "red" {
		t.Fatalf("callers share cached backing storage: %v", r4.Slots["root"].Style["color"])
	}
}
