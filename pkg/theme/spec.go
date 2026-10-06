package theme

import (
	"fmt"
	"strconv"
	"strings"
)

// AlgorithmVersion identifies the built-in resolution algorithm. It is part of
// the cache key; bumping it invalidates every previously cached result.
const AlgorithmVersion = "1.0.0"

// MinSupportedSpec / MaxSupportedSpec bound the accepted descriptor spec
// versions. Both bounds are inclusive; comparisons use the major.minor tuple
// (an accepted patch level is implied by the minor).
const (
	MinSupportedSpec = "1.0"
	MaxSupportedSpec = "1.4"
)

// Warning is a non-fatal diagnostic produced while parsing a descriptor, for
// example when a deprecated field is encountered.
type Warning struct {
	Kind    string // "deprecated" or "unknown-field"
	Message string
	Path    string
}

func (w Warning) String() string {
	if w.Path != "" {
		return fmt.Sprintf("%s: %s (%s)", w.Kind, w.Message, w.Path)
	}
	return fmt.Sprintf("%s: %s", w.Kind, w.Message)
}

// CheckSpecVersion validates a declared spec version against the supported
// range and returns a VersionError when it is missing, malformed or out of
// range.
func CheckSpecVersion(kind, name, version string) error {
	if strings.TrimSpace(version) == "" {
		return &VersionError{
			Kind:    kind,
			Name:    name,
			Version: version,
			Reason:  "specVersion is required",
		}
	}
	if _, err := CompareSemver(version, MinSupportedSpec); err != nil {
		return &VersionError{
			Kind:    kind,
			Name:    name,
			Version: version,
			Reason:  fmt.Sprintf("malformed version: %v", err),
		}
	}
	min, _ := CompareSemver(version, MinSupportedSpec)
	max, _ := CompareSemver(version, MaxSupportedSpec)
	if min < 0 || max > 0 {
		return &VersionError{
			Kind:    kind,
			Name:    name,
			Version: version,
			Reason:  fmt.Sprintf("supported range is %s..%s", MinSupportedSpec, MaxSupportedSpec),
		}
	}
	return nil
}

// CompareSemver compares two dotted positive-integer versions of the form
// major[.minor[.patch]]. Missing components count as zero; non-numeric or
// negative components are an error. Returns -1, 0 or 1.
func CompareSemver(a, b string) (int, error) {
	pa, err := parseVersion(a)
	if err != nil {
		return 0, fmt.Errorf("invalid version %q: %w", a, err)
	}
	pb, err := parseVersion(b)
	if err != nil {
		return 0, fmt.Errorf("invalid version %q: %w", b, err)
	}
	for i := 0; i < 3; i++ {
		if pa[i] != pb[i] {
			if pa[i] < pb[i] {
				return -1, nil
			}
			return 1, nil
		}
	}
	return 0, nil
}

func parseVersion(v string) ([3]int, error) {
	var out [3]int
	v = strings.TrimSpace(v)
	if v == "" {
		return out, fmt.Errorf("empty version")
	}
	parts := strings.Split(v, ".")
	if len(parts) > 3 {
		return out, fmt.Errorf("too many components")
	}
	for i, p := range parts {
		if p == "" {
			return out, fmt.Errorf("empty component")
		}
		n, err := strconv.Atoi(p)
		if err != nil {
			return out, fmt.Errorf("component %q is not an integer", p)
		}
		if n < 0 {
			return out, fmt.Errorf("component %d is negative", n)
		}
		out[i] = n
	}
	return out, nil
}
