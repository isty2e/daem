//go:build windows

package rootedpath

import (
	"strings"
	"unicode"
	"unicode/utf16"
)

func validatePlatformPhysicalRoot(value string) error {
	if err := validateWindowsAbsoluteSpelling(value, FailureInvalidRoot); err != nil {
		return err
	}
	return nil
}

func validatePlatformDestinationPath(value string) error {
	return validateWindowsAbsoluteSpelling(value, FailureInvalidDestination)
}

func validatePlatformRelativeDestination(value string) error {
	for _, component := range strings.Split(value, "/") {
		if err := validateWindowsComponent(component, FailureInvalidDestination); err != nil {
			return err
		}
	}
	return nil
}

func validatePlatformComponent(value string) error {
	return validateWindowsComponent(value, FailureInvalidDestination)
}

func validatePlatformRelativeForRoot(platform *capturedRootPlatform, value string) error {
	if platform == nil || platform.maximumComponentUTF16 == 0 {
		return newFailure(
			FailureUnsupportedPlatform,
			value,
			"Windows volume component-length evidence is unavailable",
			nil,
		)
	}
	for _, component := range strings.Split(value, "/") {
		length := uint32(len(utf16.Encode([]rune(component))))
		if length > platform.maximumComponentUTF16 {
			return newFailure(
				FailureInvalidDestination,
				component,
				"Windows path component exceeds the admitted volume limit",
				nil,
			)
		}
	}
	return nil
}

func validateWindowsAbsoluteSpelling(value string, kind FailureKind) error {
	if strings.TrimSpace(value) == "" {
		return newFailure(kind, value, "Windows path is required", nil)
	}
	normalized := strings.ReplaceAll(value, "/", `\`)
	if strings.HasPrefix(normalized, `\`) || strings.HasPrefix(normalized, `//`) {
		return newFailure(kind, value, "UNC and device paths are not admitted", nil)
	}
	if len(normalized) < 3 || !isASCIIAlpha(normalized[0]) || normalized[1] != ':' || normalized[2] != '\\' {
		return newFailure(kind, value, "Windows path must be drive-absolute", nil)
	}
	for _, component := range strings.Split(normalized[3:], `\`) {
		if component == "" || component == "." || component == ".." {
			continue
		}
		if err := validateWindowsComponent(component, kind); err != nil {
			return err
		}
	}
	return nil
}

func validateWindowsComponent(value string, kind FailureKind) error {
	if value == "" || value == "." || value == ".." {
		return nil
	}
	if strings.IndexFunc(value, func(r rune) bool {
		return unicode.IsControl(r) || strings.ContainsRune(`<>:"/\\|?*`, r)
	}) >= 0 {
		return newFailure(kind, value, "Windows path component contains an unsafe spelling", nil)
	}
	if strings.HasSuffix(value, ".") || strings.HasSuffix(value, " ") {
		return newFailure(kind, value, "Windows path component has a trailing dot or space", nil)
	}
	upper := strings.ToUpper(value)
	base := upper
	if index := strings.IndexByte(base, '.'); index >= 0 {
		base = base[:index]
	}
	switch base {
	case "CON", "PRN", "AUX", "NUL", "CONIN$", "CONOUT$", "CLOCK$", "COM¹", "COM²", "COM³", "LPT¹", "LPT²", "LPT³":
		return newFailure(kind, value, "Windows device name is not a filesystem component", nil)
	}
	if len(base) == 4 && (strings.HasPrefix(base, "COM") || strings.HasPrefix(base, "LPT")) &&
		base[3] >= '1' && base[3] <= '9' {
		return newFailure(kind, value, "Windows device name is not a filesystem component", nil)
	}
	return nil
}
