package tomlsyntax

import (
	"io"

	legacy "github.com/BurntSushi/toml"
)

// Encode preserves the established generated TOML artifact representation.
// NON-GOAL: changing writer format during parser unification. Revisit only with
// maintainer-approved artifact parity or an explicit output-format change.
func Encode(output io.Writer, value any) error { return legacy.NewEncoder(output).Encode(value) }

func Marshal(value any) ([]byte, error) { return legacy.Marshal(value) }

func formatKey(key Key) string { return legacy.Key(key).String() }
