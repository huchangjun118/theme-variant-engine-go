// Package engine 提供主题化的组件样式解析能力：
// 变体/插槽模型、分层覆盖、继承、引用派生、热替换与缓存。
package engine

// Style 是一个插槽的样式字段集合，字段名为样式键，值为标量或嵌套 map。
type Style map[string]any

// Slot 声明一个具名插槽及其默认样式。
type Slot struct {
	Name    string
	Default Style
}

// Variant 声明一个具名变体：对每个插槽给出一组样式覆盖。
type Variant struct {
	Name  string
	Slots map[string]Style // slotName -> 样式覆盖
}

// ComponentDesc 描述一个组件：插槽、变体、继承关系与规范版本。
type ComponentDesc struct {
	Name        string
	SpecVersion string
	Extends     string // 父组件名，空表示不继承
	Slots       []Slot
	Variants    map[string]Variant

	// DefaultVariants 已废弃：由调用方在输入中显式给出变体组合。
	// 废弃语义：解析时仍会被应用，但会在结果中记录 DeprecatedWarning。
	DefaultVariants []string
}

// ComponentOverride 是主题对某个组件的覆盖。
type ComponentOverride struct {
	Slots    map[string]Style            // slotName -> 覆盖样式
	Variants map[string]map[string]Style // variantName -> slotName -> 覆盖样式
}

// Theme 是一份主题，可声明令牌、组件覆盖与父主题（继承）。
type Theme struct {
	Name        string
	Version     string // 主题版本，热替换时单调变化
	SpecVersion string
	Parent      *Theme // 父主题，nil 表示根主题
	Tokens      map[string]any
	Components  map[string]ComponentOverride
}

// Input 是一次解析的输入取值。
type Input struct {
	Component string
	Variants  []string         // 要组合的变体名
	Props     Style            // 实例级传入样式（作用于所有插槽的公共字段）
	SlotProps map[string]Style // 实例级传入的按插槽样式
}

// Layer 标识取值来源层。
type Layer int

const (
	LayerBase     Layer = iota // 基础样式（组件声明 + 变体）
	LayerTheme                 // 主题覆盖
	LayerInstance              // 实例传入
)

func (l Layer) String() string {
	switch l {
	case LayerBase:
		return "base"
	case LayerTheme:
		return "theme"
	case LayerInstance:
		return "instance"
	}
	return "unknown"
}

// FieldOrigin 记录某个字段取值的来源层与来源名（组件/主题/变体名）。
type FieldOrigin struct {
	Layer  Layer
	Source string
}

// ResolvedSlot 是一个插槽的解析结果。
type ResolvedSlot struct {
	Style   Style
	Origins map[string]FieldOrigin // 字段路径 -> 来源
}

// Result 是一次解析的完整结果。
type Result struct {
	Component          string
	ThemeVersion       string
	Slots              map[string]ResolvedSlot
	DeprecatedWarnings []string
}
