package tomlstrict

import (
	"bytes"
	"errors"
	"unicode/utf8"
)

type positionedError struct {
	cause  error
	line   int
	column int
}

func (err *positionedError) Error() string { return err.cause.Error() }
func (err *positionedError) Unwrap() error { return err.cause }

// ErrorPosition returns the one-based line and rune column where structural
// admission detected malformed TOML. It does not identify the opening token.
func ErrorPosition(err error) (line, column int, ok bool) {
	var positioned *positionedError
	if !errors.As(err, &positioned) {
		return 0, 0, false
	}
	return positioned.line, positioned.column, true
}

func positionError(content []byte, index int, cause error) error {
	if !errors.Is(cause, ErrMalformed) {
		return cause
	}
	prefix := content[:min(index, len(content))]
	if bytes.HasPrefix(prefix, []byte{0xef, 0xbb, 0xbf}) {
		prefix = prefix[3:]
	}
	lineStart := bytes.LastIndexByte(prefix, '\n') + 1
	return &positionedError{
		cause:  cause,
		line:   bytes.Count(prefix, []byte{'\n'}) + 1,
		column: utf8.RuneCount(prefix[lineStart:]) + 1,
	}
}
