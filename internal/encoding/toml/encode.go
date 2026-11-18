package tomlsyntax

import (
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

// QuoteBasicString encodes valid UTF-8 as a single-line TOML 1.1 basic string.
func QuoteBasicString(value string) string {
	// Keep earlier capture spelling wherever the TOML grammar admits it.
	if !strings.ContainsAny(value, "\a\v") {
		return strconv.Quote(value)
	}
	var builder strings.Builder
	builder.WriteByte('"')
	for _, character := range value {
		switch character {
		case '\a':
			builder.WriteString(`\u0007`)
		case '\v':
			builder.WriteString(`\u000b`)
		default:
			if strconv.IsPrint(character) && character != '"' && character != '\\' {
				builder.WriteRune(character)
			} else {
				quoted := strconv.Quote(string(character))
				builder.WriteString(quoted[1 : len(quoted)-1])
			}
		}
	}
	builder.WriteByte('"')
	return builder.String()
}

func formatKey(key Key) string { return legacy.Key(key).String() }
