package theme

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// DecodeComponent parses a component descriptor from JSON.
//
// Unknown fields are NOT silently ignored: by default they produce a warning.
// With strict=true an unknown field is a hard error. Deprecated fields follow
// the same rule set enforced by ValidateComponent.
func DecodeComponent(raw []byte, strict bool) (*ComponentDescriptor, []Warning, error) {
	dec := newStrictDecoder(raw, strict, "component")
	var c ComponentDescriptor
	if err := dec.decode(&c); err != nil {
		return nil, nil, err
	}
	ws, err := ValidateComponent(&c)
	if err != nil {
		return nil, nil, err
	}
	ws = append(dec.warnings, ws...)
	return &c, ws, nil
}

// DecodeTheme parses a theme descriptor from JSON with the same strictness
// semantics as DecodeComponent.
func DecodeTheme(raw []byte, strict bool) (*Theme, []Warning, error) {
	dec := newStrictDecoder(raw, strict, "theme")
	var t Theme
	if err := dec.decode(&t); err != nil {
		return nil, nil, err
	}
	ws, err := ValidateTheme(&t)
	if err != nil {
		return nil, nil, err
	}
	ws = append(dec.warnings, ws...)
	return &t, ws, nil
}

type strictDecoder struct {
	raw      []byte
	strict   bool
	kind     string
	warnings []Warning
}

func newStrictDecoder(raw []byte, strict bool, kind string) *strictDecoder {
	return &strictDecoder{raw: raw, strict: strict, kind: kind}
}

func (d *strictDecoder) decode(v any) error {
	dec := json.NewDecoder(bytes.NewReader(d.raw))
	if d.strict {
		dec.DisallowUnknownFields()
	}
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("%w: %s: %v", ErrInvalidDescriptor, d.kind, err)
	}
	if dec.More() {
		return fmt.Errorf("%w: %s: multiple JSON values", ErrInvalidDescriptor, d.kind)
	}
	if !d.strict {
		d.collectUnknownFields()
	}
	return nil
}

// collectUnknownFields re-parses loosely and flags top-level unknown keys so
// they are never silently dropped.
func (d *strictDecoder) collectUnknownFields() {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(d.raw, &top); err != nil {
		return
	}
	var known map[string]struct{}
	if d.kind == "component" {
		known = map[string]struct{}{
			"name": {}, "specVersion": {}, "slots": {}, "variants": {}, "defaultStyles": {},
		}
	} else {
		known = map[string]struct{}{
			"name": {}, "version": {}, "specVersion": {}, "inherits": {},
			"components": {}, "tokens": {}, "strictDeprecated": {},
		}
	}
	for k := range top {
		if _, ok := known[k]; !ok {
			d.warnings = append(d.warnings, Warning{
				Kind:    "unknown-field",
				Path:    k,
				Message: fmt.Sprintf("%s descriptor has unknown field %q; it is not applied", d.kind, k),
			})
		}
	}
}
