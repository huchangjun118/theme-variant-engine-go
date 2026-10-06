package theme

import (
	"errors"
	"testing"
)

func TestDecodeUnknownFieldWarnsByDefaultRejectsStrict(t *testing.T) {
	raw := []byte(`{
		"name":"C","specVersion":"1.0",
		"slots":[{"name":"root","default":{"a":1}}],
		"mystery": 42
	}`)
	c, ws, err := DecodeComponent(raw, false)
	if err != nil {
		t.Fatalf("lenient decode: %v", err)
	}
	if c.Name != "C" {
		t.Fatal("component not decoded")
	}
	found := false
	for _, w := range ws {
		if w.Kind == "unknown-field" && w.Path == "mystery" {
			found = true
		}
	}
	if !found {
		t.Fatalf("unknown field must warn, got %+v", ws)
	}

	if _, _, err := DecodeComponent(raw, true); !errors.Is(err, ErrInvalidDescriptor) {
		t.Fatalf("strict decode must reject unknown field, got %v", err)
	}
}

func TestDecodeThemeRoundTrip(t *testing.T) {
	raw := []byte(`{
		"name":"t","version":"1","specVersion":"1.0",
		"tokens":{"c":"red"},
		"components":{}
	}`)
	th, _, err := DecodeTheme(raw, true)
	if err != nil {
		t.Fatalf("decode theme: %v", err)
	}
	if th.Name != "t" || th.Tokens["c"] != "red" {
		t.Fatalf("theme decoded wrong: %#v", th)
	}
}

func TestDecodeRejectsBadVersion(t *testing.T) {
	raw := []byte(`{"name":"C","specVersion":"2.0","slots":[{"name":"root"}]}`)
	if _, _, err := DecodeComponent(raw, false); !errors.Is(err, ErrSpecVersion) {
		t.Fatalf("want spec version error, got %v", err)
	}
}
