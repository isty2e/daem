package mcp

import (
	"fmt"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"
)

func validateStableToken(value string, label string) error {
	if value == "" {
		return fmt.Errorf("%s is required", label)
	}
	if !isASCIIAlnum(value[0]) {
		return fmt.Errorf("%s must start with an ASCII letter or digit", label)
	}
	for index := 0; index < len(value); index++ {
		character := value[index]
		if isASCIIAlnum(character) || character == '.' || character == '_' || character == '-' {
			continue
		}
		return fmt.Errorf("%s must be a stable token", label)
	}
	return nil
}

func validatePortableCommand(value string) error {
	if strings.TrimSpace(value) != value || filepath.IsAbs(value) || strings.ContainsAny(value, "/\\ \t\n\r;&|$`") {
		return fmt.Errorf("command must be a portable command token")
	}
	return validateStableToken(value, "command")
}

func validateEnvName(value string, label string) error {
	if value == "" {
		return fmt.Errorf("%s is required", label)
	}
	for index := 0; index < len(value); index++ {
		character := value[index]
		switch {
		case character >= 'A' && character <= 'Z':
		case character >= 'a' && character <= 'z':
		case character == '_':
		case character >= '0' && character <= '9' && index > 0:
		default:
			return fmt.Errorf("%s must contain only ASCII letters, digits, or underscore and must not start with a digit", label)
		}
	}
	return nil
}

func validateArgument(value string) error {
	if !utf8.ValidString(value) {
		return fmt.Errorf("argument must be valid UTF-8")
	}
	if strings.IndexFunc(value, isUnsafeControl) >= 0 {
		return fmt.Errorf("argument contains a control character")
	}
	return nil
}

func isUnsafeControl(value rune) bool {
	return unicode.IsControl(value) || unicode.Is(unicode.Bidi_Control, value)
}

func isASCIIAlnum(character byte) bool {
	return (character >= 'A' && character <= 'Z') ||
		(character >= 'a' && character <= 'z') ||
		(character >= '0' && character <= '9')
}
