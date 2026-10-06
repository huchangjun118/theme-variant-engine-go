package theme

import (
	"fmt"
	"strings"
)

// Sentinel errors callers can inspect with errors.Is.
var (
	// ErrUndeclaredVariant is returned when a style override references a
	// variant key that the component does not declare.
	ErrUndeclaredVariant = fmt.Errorf("theme: undeclared variant")
	// ErrUnknownSlot is returned when an override targets an undeclared slot.
	ErrUnknownSlot = fmt.Errorf("theme: unknown slot")
	// ErrInheritanceCycle is returned when a theme inherits from itself.
	ErrInheritanceCycle = fmt.Errorf("theme: inheritance cycle")
	// ErrInheritanceBroken is returned when a theme inherits from a missing theme.
	ErrInheritanceBroken = fmt.Errorf("theme: inheritance broken chain")
	// ErrReferenceUndefined is returned when a style value references a token
	// or style key that does not exist.
	ErrReferenceUndefined = fmt.Errorf("theme: undefined reference")
	// ErrReferenceCycle is returned when style references form a cycle.
	ErrReferenceCycle = fmt.Errorf("theme: reference cycle")
	// ErrSpecVersion is returned when a declared spec version is unsupported.
	ErrSpecVersion = fmt.Errorf("theme: unsupported spec version")
	// ErrInvalidDescriptor is returned for malformed component/theme input.
	ErrInvalidDescriptor = fmt.Errorf("theme: invalid descriptor")
	// ErrCacheCorrupt is returned internally when a cache entry fails integrity checks.
	ErrCacheCorrupt = fmt.Errorf("theme: cache entry corrupt")
)

// VariantError points at the slot and variant name responsible for an
// undeclared variant reference.
type VariantError struct {
	Component string
	Slot      string
	Variant   string
	Layer     Layer
}

func (e *VariantError) Error() string {
	return fmt.Sprintf("component %q: slot %q references undeclared variant %q (layer %s)",
		e.Component, e.Slot, e.Variant, e.Layer)
}

func (e *VariantError) Unwrap() error { return ErrUndeclaredVariant }

// SlotError points at an unknown slot.
type SlotError struct {
	Component string
	Slot      string
	Layer     Layer
}

func (e *SlotError) Error() string {
	return fmt.Sprintf("component %q: %s layer targets unknown slot %q",
		e.Component, e.Layer, e.Slot)
}

func (e *SlotError) Unwrap() error { return ErrUnknownSlot }

// InheritanceError distinguishes cyclic from broken inheritance chains.
type InheritanceError struct {
	Theme string
	Chain []string
	Cycle bool
}

func (e *InheritanceError) Error() string {
	if e.Cycle {
		return fmt.Sprintf("theme %q inherits in a cycle: %s",
			e.Theme, strings.Join(e.Chain, " -> "))
	}
	return fmt.Sprintf("theme %q inherits from missing theme %q (chain: %s)",
		e.Theme, missingLink(e.Chain), strings.Join(e.Chain, " -> "))
}

func missingLink(chain []string) string {
	if len(chain) == 0 {
		return ""
	}
	return chain[len(chain)-1]
}

func (e *InheritanceError) Unwrap() error {
	if e.Cycle {
		return ErrInheritanceCycle
	}
	return ErrInheritanceBroken
}

// ReferenceError describes an undefined or cyclic style reference.
type ReferenceError struct {
	Component string
	Slot      string
	Key       string
	Ref       string
	Cycle     bool
	Path      []string
}

func (e *ReferenceError) Error() string {
	if e.Cycle {
		return fmt.Sprintf("component %q: cyclic reference at slot %q key %q: %s (path: %s)",
			e.Component, e.Slot, e.Key, e.Ref, strings.Join(e.Path, " -> "))
	}
	return fmt.Sprintf("component %q: undefined reference %q at slot %q key %q (path: %s)",
		e.Component, e.Ref, e.Slot, e.Key, strings.Join(e.Path, " -> "))
}

func (e *ReferenceError) Unwrap() error {
	if e.Cycle {
		return ErrReferenceCycle
	}
	return ErrReferenceUndefined
}

// VersionError describes an unsupported spec version declaration.
type VersionError struct {
	Kind    string // "component" or "theme"
	Name    string
	Version string
	Reason  string
}

func (e *VersionError) Error() string {
	return fmt.Sprintf("%s %q declares spec version %q: %s",
		e.Kind, e.Name, e.Version, e.Reason)
}

func (e *VersionError) Unwrap() error { return ErrSpecVersion }
