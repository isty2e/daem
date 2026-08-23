//go:build windows

package commit

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf16"
	"unicode/utf8"
)

const maximumWindowsComponentUTF16 = 32767

type windowsComponent struct {
	value string
	units []uint16
}

func parseWindowsComponent(value string) (windowsComponent, error) {
	if value == "" || value == "." || value == ".." {
		return windowsComponent{}, fmt.Errorf("Windows name must be one non-special component")
	}
	if !utf8.ValidString(value) {
		return windowsComponent{}, fmt.Errorf("Windows name is not valid UTF-8")
	}
	units := utf16.Encode([]rune(value))
	if len(units) == 0 || len(units) > maximumWindowsComponentUTF16 {
		return windowsComponent{}, fmt.Errorf("Windows name has an invalid UTF-16 length")
	}
	for _, character := range value {
		if unicode.IsControl(character) || strings.ContainsRune(`<>:"/\\|?*`, character) {
			return windowsComponent{}, fmt.Errorf("Windows name contains an unsafe character")
		}
	}
	if strings.HasSuffix(value, ".") || strings.HasSuffix(value, " ") {
		return windowsComponent{}, fmt.Errorf("Windows name has a trailing dot or space")
	}
	upper := strings.ToUpper(value)
	base := upper
	if index := strings.IndexByte(base, '.'); index >= 0 {
		base = base[:index]
	}
	switch base {
	case "CON", "PRN", "AUX", "NUL", "CONIN$", "CONOUT$", "CLOCK$", "COM¹", "COM²", "COM³", "LPT¹", "LPT²", "LPT³":
		return windowsComponent{}, fmt.Errorf("Windows device name is not a filesystem component")
	}
	if len(base) == 4 && (strings.HasPrefix(base, "COM") || strings.HasPrefix(base, "LPT")) &&
		base[3] >= '1' && base[3] <= '9' {
		return windowsComponent{}, fmt.Errorf("Windows device name is not a filesystem component")
	}
	return windowsComponent{value: value, units: units}, nil
}

func validateWindowsComponentName(value string) error {
	_, err := parseWindowsComponent(value)
	return err
}

func validateWindowsComponentForVolume(component windowsComponent, maximumUTF16 uint32) error {
	if maximumUTF16 == 0 {
		return windowsNativeUnsupported(
			windowsNativePhaseOpen,
			"volume component-length evidence is unavailable",
			nil,
		)
	}
	if uint32(len(component.units)) > maximumUTF16 {
		return fmt.Errorf("Windows name exceeds the admitted volume component limit")
	}
	return nil
}

func (component windowsComponent) String() string { return component.value }

func (component windowsComponent) utf16() []uint16 {
	return append([]uint16(nil), component.units...)
}
