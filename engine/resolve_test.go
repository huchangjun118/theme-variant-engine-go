package engine

import (
	"errors"
	"testing"
)

func testComponents() []ComponentDesc {
	return []ComponentDesc{
		{
			Name: "button",
			Slots: []Slot{
				{Name: "root", Default: Style{"color": "gray", "padding": 4}},
				{Name: "label", Default: Style{"fontSize": 14}},
			},
			Variants: map[string]Variant{
				"primary": {Name: "primary", Slots: map[string]Style{
					"root": {"color": "{tokens.color.brand}"},
				}},
				"large": {Name: "large", Slots: map[string]Style{
					"root":  {"padding": 8},
					"label": {"fontSize": 18},
				}},
			},
		},
		{
			Name:    "iconButton",
			Extends: "button",
			Slots: []Slot{
				{Name: "icon", Default: Style{"size": 16}},
			},
			Variants: map[string]Variant{
				"primary": {Name: "primary", Slots: map[string]Style{
					"icon": {"color": "white"},
				}},
			},
		},
	}
}

func testTheme() *Theme {
	return &Theme{
		Name: "default", Version: "v1", SpecVersion: "1.0",
		Tokens: map[string]any{
			"color": map[string]any{"brand": "blue"},
		},
		Components: map[string]ComponentOverride{
			"button": {
				Slots: map[string]Style{
					"root": {"color": "green", "radius": 2},
				},
				Variants: map[string]map[string]Style{
					"large": {"root": {"padding": 12}},
				},
			},
		},
	}
}

// plainTheme 不含组件覆盖，用于独立组件的测试。
func plainTheme() *Theme {
	return &Theme{
		Name: "plain", Version: "v1", SpecVersion: "1.0",
		Tokens: map[string]any{"color": map[string]any{"brand": "blue"}},
	}
}

func newTestEngine(t *testing.T) *Engine {
	t.Helper()
	e, err := New(testComponents(), testTheme(), 128)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return e
}

func TestVariantCombinationDeterministic(t *testing.T) {
	e := newTestEngine(t)
	in := Input{Component: "button", Variants: []string{"primary", "large"}}
	r1, err := e.Resolve(in)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	// 组合后每个插槽取值确定
	root := r1.Slots["root"].Style
	if root["color"] != "green" { // 主题槽位覆盖优先于变体基础样式
		t.Fatalf("color = %v, want green", root["color"])
	}
	if o := r1.Slots["root"].Origins["color"]; o.Layer != LayerTheme {
		t.Fatalf("color origin = %+v, want theme", o)
	}
	if root["padding"] != 12 { // 主题对 large 变体的覆盖优先于变体自身
		t.Fatalf("padding = %v, want 12", root["padding"])
	}
	if r1.Slots["label"].Style["fontSize"] != 18 {
		t.Fatalf("fontSize = %v, want 18", r1.Slots["label"].Style["fontSize"])
	}
	// 变体顺序不影响结果
	r2, err := e.Resolve(Input{Component: "button", Variants: []string{"large", "primary"}})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if r2.Slots["root"].Style["color"] != "green" || r2.Slots["root"].Style["padding"] != 12 {
		t.Fatalf("order-dependent result: %+v", r2.Slots["root"].Style)
	}
}

func TestUnknownVariantError(t *testing.T) {
	e := newTestEngine(t)
	_, err := e.Resolve(Input{Component: "button", Variants: []string{"ghost"}})
	var uv *ErrUnknownVariant
	if !errors.As(err, &uv) {
		t.Fatalf("err = %v, want ErrUnknownVariant", err)
	}
	if uv.Variant != "ghost" || uv.Component != "button" {
		t.Fatalf("error detail = %+v", uv)
	}
}

func TestVariantUndeclaredSlotError(t *testing.T) {
	comps := append(testComponents(), ComponentDesc{
		Name:  "bad",
		Slots: []Slot{{Name: "root"}},
		Variants: map[string]Variant{
			"x": {Name: "x", Slots: map[string]Style{"nope": {"a": 1}}},
		},
	})
	_, err := New(comps, testTheme(), 0)
	var us *ErrUnknownSlot
	if !errors.As(err, &us) {
		t.Fatalf("err = %v, want ErrUnknownSlot", err)
	}
	if us.Slot != "nope" || us.Variant != "x" {
		t.Fatalf("error detail = %+v", us)
	}
}

func TestLayerPriorityAndOrigins(t *testing.T) {
	e := newTestEngine(t)
	r, err := e.Resolve(Input{
		Component: "button",
		Variants:  []string{"large"},
		Props:     Style{"color": "red"},                   // 实例公共覆盖
		SlotProps: map[string]Style{"root": {"radius": 9}}, // 实例按插槽覆盖
	})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	root := r.Slots["root"]
	if root.Style["color"] != "red" {
		t.Fatalf("color = %v, want red (instance wins)", root.Style["color"])
	}
	if root.Style["radius"] != 9 {
		t.Fatalf("radius = %v, want 9 (slotProps wins)", root.Style["radius"])
	}
	if root.Style["padding"] != 12 {
		t.Fatalf("padding = %v, want 12 (theme variant override)", root.Style["padding"])
	}
	// 来源层可定位
	if o := root.Origins["color"]; o.Layer != LayerInstance {
		t.Fatalf("color origin = %+v, want instance", o)
	}
	if o := root.Origins["radius"]; o.Layer != LayerInstance {
		t.Fatalf("radius origin = %+v, want instance", o)
	}
	if o := root.Origins["padding"]; o.Layer != LayerTheme {
		t.Fatalf("padding origin = %+v, want theme", o)
	}
}

func TestInheritanceAndInstanceBeatsInherited(t *testing.T) {
	e := newTestEngine(t)
	r, err := e.Resolve(Input{
		Component: "iconButton",
		Variants:  []string{"primary"},
		SlotProps: map[string]Style{"icon": {"size": 32}},
	})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	// 继承父组件的插槽与变体
	if r.Slots["root"].Style["color"] != "blue" {
		t.Fatalf("inherited root color = %v, want blue", r.Slots["root"].Style["color"])
	}
	// 子组件变体覆盖应用到子组件插槽
	if r.Slots["icon"].Style["color"] != "white" {
		t.Fatalf("icon color = %v, want white", r.Slots["icon"].Style["color"])
	}
	// 实例值优先于继承的默认值
	if r.Slots["icon"].Style["size"] != 32 {
		t.Fatalf("icon size = %v, want 32", r.Slots["icon"].Style["size"])
	}
	if o := r.Slots["icon"].Origins["size"]; o.Layer != LayerInstance {
		t.Fatalf("size origin = %+v, want instance", o)
	}
}

func TestInheritanceCycleAndBrokenLink(t *testing.T) {
	cyc := []ComponentDesc{
		{Name: "a", Extends: "b", Slots: []Slot{{Name: "s"}}},
		{Name: "b", Extends: "a", Slots: []Slot{{Name: "s"}}},
	}
	_, err := New(cyc, testTheme(), 0)
	var cycle *ErrInheritanceCycle
	if !errors.As(err, &cycle) {
		t.Fatalf("err = %v, want ErrInheritanceCycle", err)
	}

	broken := []ComponentDesc{
		{Name: "a", Extends: "missing", Slots: []Slot{{Name: "s"}}},
	}
	_, err = New(broken, testTheme(), 0)
	var bl *ErrBrokenInheritance
	if !errors.As(err, &bl) {
		t.Fatalf("err = %v, want ErrBrokenInheritance", err)
	}
	if bl.Parent != "missing" {
		t.Fatalf("broken parent = %q", bl.Parent)
	}
}

func TestReferenceErrors(t *testing.T) {
	// 未定义引用
	comps := []ComponentDesc{{
		Name:  "c1",
		Slots: []Slot{{Name: "root", Default: Style{"color": "{tokens.nope}"}}},
	}}
	e, err := New(comps, plainTheme(), 0)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, err = e.Resolve(Input{Component: "c1"})
	var undef *ErrUndefinedReference
	if !errors.As(err, &undef) {
		t.Fatalf("err = %v, want ErrUndefinedReference", err)
	}

	// 循环引用（styles 自引用）
	comps2 := []ComponentDesc{{
		Name: "c2",
		Slots: []Slot{{Name: "root", Default: Style{
			"a": "{styles.b}",
			"b": "{styles.a}",
		}}},
	}}
	e2, err := New(comps2, plainTheme(), 0)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, err = e2.Resolve(Input{Component: "c2"})
	var rc *ErrReferenceCycle
	if !errors.As(err, &rc) {
		t.Fatalf("err = %v, want ErrReferenceCycle", err)
	}

	// 令牌循环引用在主题加载期即被拒绝
	badTheme := &Theme{
		Name: "bad", Version: "v9",
		Tokens: map[string]any{
			"x": "{tokens.y}",
			"y": "{tokens.x}",
		},
	}
	_, err = New(testComponents(), badTheme, 0)
	if !errors.As(err, &rc) {
		t.Fatalf("err = %v, want ErrReferenceCycle at load time", err)
	}
}

func TestSpecVersionRejected(t *testing.T) {
	comps := []ComponentDesc{{
		Name: "c", SpecVersion: "2.0",
		Slots: []Slot{{Name: "root"}},
	}}
	_, err := New(comps, testTheme(), 0)
	var usv *ErrUnsupportedSpecVersion
	if !errors.As(err, &usv) {
		t.Fatalf("err = %v, want ErrUnsupportedSpecVersion", err)
	}

	th := testTheme()
	th.SpecVersion = "9.9"
	_, err = New(testComponents(), th, 0)
	if !errors.As(err, &usv) {
		t.Fatalf("err = %v, want ErrUnsupportedSpecVersion", err)
	}
}

func TestDeprecatedDefaultVariants(t *testing.T) {
	comps := []ComponentDesc{{
		Name:            "legacy",
		Slots:           []Slot{{Name: "root", Default: Style{"color": "gray"}}},
		DefaultVariants: []string{"primary"},
		Variants: map[string]Variant{
			"primary": {Name: "primary", Slots: map[string]Style{"root": {"color": "blue"}}},
		},
	}}
	e, err := New(comps, plainTheme(), 0)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	r, err := e.Resolve(Input{Component: "legacy"})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	// 废弃字段语义确定：仍生效，但记录警告
	if r.Slots["root"].Style["color"] != "blue" {
		t.Fatalf("color = %v, want blue", r.Slots["root"].Style["color"])
	}
	if len(r.DeprecatedWarnings) == 0 {
		t.Fatal("expected deprecated warning")
	}
}
