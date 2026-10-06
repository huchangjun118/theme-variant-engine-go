package engine

import "fmt"

// ErrUnknownVariant 引用了未声明的变体键。
type ErrUnknownVariant struct {
	Component string
	Variant   string
}

func (e *ErrUnknownVariant) Error() string {
	return fmt.Sprintf("component %q: unknown variant %q", e.Component, e.Variant)
}

// ErrUnknownSlot 变体或覆盖引用了未声明的插槽。
type ErrUnknownSlot struct {
	Component string
	Variant   string // 为空表示来自主题覆盖或实例传入
	Slot      string
}

func (e *ErrUnknownSlot) Error() string {
	if e.Variant != "" {
		return fmt.Sprintf("component %q: variant %q references undeclared slot %q", e.Component, e.Variant, e.Slot)
	}
	return fmt.Sprintf("component %q: override references undeclared slot %q", e.Component, e.Slot)
}

// ErrInheritanceCycle 继承链出现循环。
type ErrInheritanceCycle struct {
	Chain []string
}

func (e *ErrInheritanceCycle) Error() string {
	return fmt.Sprintf("inheritance cycle detected: %v", e.Chain)
}

// ErrBrokenInheritance 继承链断链（父组件不存在）。
type ErrBrokenInheritance struct {
	Component string
	Parent    string
}

func (e *ErrBrokenInheritance) Error() string {
	return fmt.Sprintf("component %q extends unknown component %q", e.Component, e.Parent)
}

// ErrUndefinedReference 引用了未定义的令牌或样式键。
type ErrUndefinedReference struct {
	Ref string
}

func (e *ErrUndefinedReference) Error() string {
	return fmt.Sprintf("undefined reference %q", e.Ref)
}

// ErrReferenceCycle 引用之间出现循环。
type ErrReferenceCycle struct {
	Chain []string
}

func (e *ErrReferenceCycle) Error() string {
	return fmt.Sprintf("reference cycle detected: %v", e.Chain)
}

// ErrUnsupportedSpecVersion 规范版本超出支持范围。
type ErrUnsupportedSpecVersion struct {
	Kind      string // "component" 或 "theme"
	Name      string
	Version   string
	Supported string
}

func (e *ErrUnsupportedSpecVersion) Error() string {
	return fmt.Sprintf("%s %q declares spec version %q, supported range: %s", e.Kind, e.Name, e.Version, e.Supported)
}

// ErrUnknownComponent 请求解析未注册的组件。
type ErrUnknownComponent struct {
	Name string
}

func (e *ErrUnknownComponent) Error() string {
	return fmt.Sprintf("unknown component %q", e.Name)
}
