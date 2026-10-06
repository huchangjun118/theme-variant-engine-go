package engine

import (
	"fmt"
	"strconv"
	"strings"
)

// 当前支持的规范版本范围：主版本必须为 SupportedSpecMajor，
// 次版本不得高于 SupportedSpecMaxMinor。
const (
	SupportedSpecMajor    = 1
	SupportedSpecMaxMinor = 2
	SupportedSpecRange    = "1.0 - 1.2"
)

// checkSpecVersion 校验规范版本，越界时返回 ErrUnsupportedSpecVersion。
// version 形如 "1.0"、"1.2"；空版本视为 1.0。
func checkSpecVersion(kind, name, version string) error {
	if version == "" {
		return nil
	}
	major, minor, err := parseSpecVersion(version)
	if err != nil {
		return &ErrUnsupportedSpecVersion{Kind: kind, Name: name, Version: version, Supported: SupportedSpecRange}
	}
	if major != SupportedSpecMajor || minor > SupportedSpecMaxMinor {
		return &ErrUnsupportedSpecVersion{Kind: kind, Name: name, Version: version, Supported: SupportedSpecRange}
	}
	return nil
}

func parseSpecVersion(v string) (major, minor int, err error) {
	parts := strings.Split(strings.TrimSpace(v), ".")
	if len(parts) < 1 || len(parts) > 2 {
		return 0, 0, fmt.Errorf("invalid spec version %q", v)
	}
	major, err = strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, fmt.Errorf("invalid spec version %q", v)
	}
	if len(parts) == 2 {
		minor, err = strconv.Atoi(parts[1])
		if err != nil {
			return 0, 0, fmt.Errorf("invalid spec version %q", v)
		}
	}
	return major, minor, nil
}
