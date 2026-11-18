package tomlsyntax

import (
	"fmt"
	"io"
	"strconv"
	"strings"

	legacy "github.com/BurntSushi/toml"
)

// Encode preserves the established generated TOML artifact representation.
// NON-GOAL: changing writer format during parser unification. Revisit only with
// maintainer-approved artifact parity or an explicit output-format change.
func Encode(output io.Writer, value any) error { return legacy.NewEncoder(output).Encode(value) }

func Marshal(value any) ([]byte, error) { return legacy.Marshal(value) }

// QuoteBasicString encodes valid UTF-8 as a single-line TOML basic string.
func QuoteBasicString(value string) string {
	var builder strings.Builder
	builder.WriteByte('"')
	for _, character := range value {
		switch {
		case strings.ContainsRune("\\\"\b\t\n\f\r", character):
			quoted := strconv.Quote(string(character))
			builder.WriteString(quoted[1 : len(quoted)-1])
		case strconv.IsPrint(character):
			builder.WriteRune(character)
		case character <= 0xFFFF:
			fmt.Fprintf(&builder, `\u%04x`, character)
		default:
			fmt.Fprintf(&builder, `\U%08x`, character)
		}
	}
	builder.WriteByte('"')
	return builder.String()
}

func formatKey(key Key) string { return legacy.Key(key).String() }
