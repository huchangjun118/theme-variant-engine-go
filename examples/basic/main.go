// Command basic demonstrates deterministic component resolution: variants,
// slot layers, inheritance, references, provenance and hot replacement.
package main

import (
	"context"
	"fmt"

	theme "github.com/huchangjun118/theme-variant-engine-go/pkg/theme"
)

func main() {
	// 1. Describe a component with named slots and named variants.
	button := &theme.ComponentDescriptor{
		Name:        "Button",
		SpecVersion: "1.0",
		Slots: []theme.SlotDecl{
			{Name: "root", Default: theme.StyleMap{
				"color":  "#111",
				"padding": theme.StyleMap{"x": 4, "y": 4},
			}},
			{Name: "label", Default: theme.StyleMap{"weight": "normal"}},
		},
		Variants: []theme.VariantDecl{
			{Name: "primary", Slots: map[string]theme.StyleMap{
				"root":  {"color": "{token:brand.primary}"},
				"label": {"weight": "bold"},
			}},
		},
	}

	// 2. Publish a parent and a child theme.
	parent := &theme.Theme{
		Name: "base", Version: "2024.10", SpecVersion: "1.0",
		Tokens: map[string]any{
			"brand": map[string]any{"primary": "#3366ff"},
		},
	}
	dark := &theme.Theme{
		Name: "dark", Version: "2024.10.1", SpecVersion: "1.0", Inherits: "base",
		Components: map[string]*theme.ComponentTheme{
			"Button": {
				Slots: map[string]theme.SlotOverride{
					"root": {Style: theme.StyleMap{
						"color":      "{token:brand.primary}",
						"background": "#000",
					}},
				},
			},
		},
	}
	reg := theme.NewRegistry(theme.WithCacheSize(128))
	if err := reg.Publish(context.Background(), []*theme.Theme{parent, dark}, "dark"); err != nil {
		panic(err)
	}
	eng := theme.NewEngine(reg)

	// 3. Resolve: primary variant + an instance-level override.
	res, err := eng.Resolve(context.Background(), button,
		theme.InstanceInput{
			Variants: []string{"primary"},
			Slots:    map[string]theme.StyleMap{"root": {"margin": 0}},
		},
		theme.ResolveOptions{})
	if err != nil {
		panic(err)
	}

	fmt.Printf("theme=%s version=%s\n", res.Theme, res.ThemeVersion)
	root := res.Slots["root"].Style
	fmt.Printf("root.color      = %v\n", root["color"])
	fmt.Printf("root.background = %v\n", root["background"])
	fmt.Printf("root.padding    = %#v\n", root["padding"])
	fmt.Printf("root.margin     = %v\n", root["margin"])
	fmt.Printf("label.weight    = %v\n", res.Slots["label"].Style["weight"])

	if src, ok := res.ProvenanceFor("root", "color"); ok {
		fmt.Printf("color came from layer=%s theme=%s\n", src.Layer, src.Theme)
	}

	// 4. Hot replace: a new theme version is atomically visible.
	dark2 := *dark
	dark2.Version = "2024.11"
	dark2.Components = map[string]*theme.ComponentTheme{
		"Button": {Slots: map[string]theme.SlotOverride{
			"root": {Style: theme.StyleMap{"background": "#111"}},
		}},
	}
	if err := reg.Publish(context.Background(), []*theme.Theme{parent, &dark2}, "dark"); err != nil {
		panic(err)
	}
	res2, _ := eng.Resolve(context.Background(), button,
		theme.InstanceInput{Variants: []string{"primary"}}, theme.ResolveOptions{})
	fmt.Printf("after hot swap version=%s background=%v\n",
		res2.ThemeVersion, res2.Slots["root"].Style["background"])
}
