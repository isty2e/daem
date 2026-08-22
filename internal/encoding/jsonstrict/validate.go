// Package jsonstrict validates JSON syntax shared by persistence and host-document boundaries.
package jsonstrict

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode/utf8"
)

const cancelCheckInterval = 4096

var (
	// ErrDuplicateObjectKey classifies a JSON object with repeated key text.
	ErrDuplicateObjectKey = errors.New("duplicate JSON object key")
	// ErrMaximumDepthExceeded classifies JSON nesting beyond the caller's budget.
	ErrMaximumDepthExceeded = errors.New("JSON maximum depth exceeded")
	// ErrMultipleValues classifies trailing content that begins a second JSON value.
	ErrMultipleValues = errors.New("multiple JSON values")
)

// VersionDisposition classifies a valid positive version relative to the
// current schema version selected by the caller.
type VersionDisposition uint8

const (
	VersionLegacy VersionDisposition = iota
	VersionCurrent
	VersionFuture
)

// VersionEnvelope is the schema-independent authority boundary for one
// canonical versioned JSON object.
type VersionEnvelope struct {
	Version     int
	Disposition VersionDisposition
}

type classifiedError struct {
	kind    error
	message string
}

func (err classifiedError) Error() string { return err.message }

func (err classifiedError) Unwrap() error { return err.kind }

// Validate requires one UTF-8 JSON value with unique object keys and bounded nesting.
func Validate(content []byte, document string, maximumDepth int) error {
	return ValidateContext(context.Background(), content, document, maximumDepth)
}

// ValidateContext requires one UTF-8 JSON value with unique object keys and
// bounded nesting while preserving caller cancellation during validation.
func ValidateContext(ctx context.Context, content []byte, document string, maximumDepth int) error {
	return validate(ctx, content, document, maximumDepth, false)
}

func validate(
	ctx context.Context,
	content []byte,
	document string,
	maximumDepth int,
	canonicalObjectKeys bool,
) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if strings.TrimSpace(document) == "" {
		return fmt.Errorf("JSON document label is required")
	}
	if maximumDepth <= 0 {
		return fmt.Errorf("%s JSON maximum depth must be positive", document)
	}
	if err := validateUTF8(ctx, content); err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return err
		}
		return fmt.Errorf("%s is not valid UTF-8", document)
	}
	if err := validateUnicodeScalarEscapes(ctx, content, document); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.UseNumber()
	if err := consumeValue(ctx, decoder, document, maximumDepth, 0, canonicalObjectKeys); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if token, err := decoder.Token(); err == nil {
		if contextErr := ctx.Err(); contextErr != nil {
			return contextErr
		}
		return classifiedError{
			kind:    ErrMultipleValues,
			message: fmt.Sprintf("%s contains multiple JSON values beginning with %v", document, token),
		}
	} else if err != io.EOF {
		if contextErr := ctx.Err(); contextErr != nil {
			return contextErr
		}
		return fmt.Errorf("parse %s trailer: %w", document, err)
	}
	return ctx.Err()
}

// ValidateVersionedObject validates one strict versioned persistence object
// and returns its required positive integer version. Every object key must use
// canonical ASCII lower_snake_case spelling; schema-specific fields stay opaque.
func ValidateVersionedObject(content []byte, document string, maximumDepth int) (int, error) {
	if err := validate(context.Background(), content, document, maximumDepth, true); err != nil {
		return 0, err
	}
	return exactObjectVersion(content, document)
}

// DecodeVersionEnvelope validates one canonical versioned object and
// classifies its version before schema-specific decoding begins.
func DecodeVersionEnvelope(
	content []byte,
	document string,
	maximumDepth int,
	currentVersion int,
) (VersionEnvelope, error) {
	if currentVersion <= 0 {
		return VersionEnvelope{}, fmt.Errorf("%s current version must be positive", document)
	}
	version, err := ValidateVersionedObject(content, document, maximumDepth)
	if err != nil {
		return VersionEnvelope{}, err
	}
	disposition := VersionCurrent
	if version < currentVersion {
		disposition = VersionLegacy
	} else if version > currentVersion {
		disposition = VersionFuture
	}
	return VersionEnvelope{
		Version:     version,
		Disposition: disposition,
	}, nil
}

func exactObjectVersion(content []byte, document string) (int, error) {
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.UseNumber()
	opening, err := decoder.Token()
	if err != nil {
		return 0, fmt.Errorf("parse %s: %w", document, err)
	}
	if opening != json.Delim('{') {
		return 0, fmt.Errorf("%s must be a JSON object", document)
	}

	for decoder.More() {
		keyToken, err := decoder.Token()
		if err != nil {
			return 0, fmt.Errorf("parse %s object key: %w", document, err)
		}
		key, ok := keyToken.(string)
		if !ok {
			return 0, fmt.Errorf("%s object key is not a string", document)
		}
		if key != "version" {
			if err := skipValue(decoder, document); err != nil {
				return 0, err
			}
			continue
		}

		versionToken, err := decoder.Token()
		if err != nil {
			return 0, fmt.Errorf("parse %s field %q: %w", document, "version", err)
		}
		number, ok := versionToken.(json.Number)
		if !ok {
			return 0, fmt.Errorf("%s field %q must be a positive integer", document, "version")
		}
		version, err := strconv.Atoi(number.String())
		if err != nil || version <= 0 {
			return 0, fmt.Errorf("%s field %q must be a positive integer", document, "version")
		}
		return version, nil
	}
	return 0, fmt.Errorf("%s field %q is required", document, "version")
}

func skipValue(decoder *json.Decoder, document string) error {
	token, err := decoder.Token()
	if err != nil {
		return fmt.Errorf("parse %s: %w", document, err)
	}
	delimiter, composite := token.(json.Delim)
	if !composite {
		return nil
	}
	object := delimiter == json.Delim('{')
	closing := json.Delim(']')
	if object {
		closing = json.Delim('}')
	}
	for decoder.More() {
		if object {
			if _, err := decoder.Token(); err != nil {
				return fmt.Errorf("parse %s object key: %w", document, err)
			}
		}
		if err := skipValue(decoder, document); err != nil {
			return err
		}
	}
	return consumeClosingDelimiter(decoder, document, closing)
}

func consumeValue(
	ctx context.Context,
	decoder *json.Decoder,
	document string,
	maximumDepth int,
	depth int,
	canonicalObjectKeys bool,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if depth > maximumDepth {
		return classifiedError{
			kind:    ErrMaximumDepthExceeded,
			message: fmt.Sprintf("%s JSON exceeds maximum depth %d", document, maximumDepth),
		}
	}
	token, err := decoder.Token()
	if err != nil {
		if contextErr := ctx.Err(); contextErr != nil {
			return contextErr
		}
		return fmt.Errorf("parse %s: %w", document, err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	delimiter, composite := token.(json.Delim)
	if !composite {
		return nil
	}
	switch delimiter {
	case '{':
		seen := make(map[string]struct{})
		for decoder.More() {
			if err := ctx.Err(); err != nil {
				return err
			}
			keyToken, err := decoder.Token()
			if err != nil {
				if contextErr := ctx.Err(); contextErr != nil {
					return contextErr
				}
				return fmt.Errorf("parse %s object key: %w", document, err)
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			key, ok := keyToken.(string)
			if !ok {
				return fmt.Errorf("%s object key is not a string", document)
			}
			if canonicalObjectKeys && !isASCIILowerSnakeCase(key) {
				return fmt.Errorf(
					"%s object key %q must use canonical ASCII lower_snake_case spelling",
					document,
					key,
				)
			}
			if _, duplicate := seen[key]; duplicate {
				return classifiedError{
					kind:    ErrDuplicateObjectKey,
					message: fmt.Sprintf("%s contains duplicate object key %q", document, key),
				}
			}
			seen[key] = struct{}{}
			if err := consumeValue(
				ctx,
				decoder,
				document,
				maximumDepth,
				depth+1,
				canonicalObjectKeys,
			); err != nil {
				return err
			}
		}
		return consumeClosingDelimiterContext(ctx, decoder, document, '}')
	case '[':
		for decoder.More() {
			if err := ctx.Err(); err != nil {
				return err
			}
			if err := consumeValue(
				ctx,
				decoder,
				document,
				maximumDepth,
				depth+1,
				canonicalObjectKeys,
			); err != nil {
				return err
			}
		}
		return consumeClosingDelimiterContext(ctx, decoder, document, ']')
	default:
		return fmt.Errorf("%s has unexpected JSON delimiter %q", document, delimiter)
	}
}

func validateUTF8(ctx context.Context, content []byte) error {
	for start := 0; start < len(content); {
		end := min(start+cancelCheckInterval, len(content))
		if end < len(content) {
			for end > start && !utf8.RuneStart(content[end]) {
				end--
			}
			if end == start {
				end = min(start+cancelCheckInterval, len(content))
			}
		}
		if !utf8.Valid(content[start:end]) {
			return fmt.Errorf("invalid UTF-8")
		}
		start = end
		if err := ctx.Err(); err != nil {
			return err
		}
	}
	return ctx.Err()
}

func validateUnicodeScalarEscapes(ctx context.Context, content []byte, document string) error {
	cancelAt := cancelCheckInterval
	for index := 0; index < len(content); index++ {
		if index >= cancelAt {
			if err := ctx.Err(); err != nil {
				return err
			}
			cancelAt = index + cancelCheckInterval
		}
		if content[index] != '"' {
			continue
		}
		closing, err := validateJSONStringScalarEscapes(content, document, index+1)
		if err != nil {
			return err
		}
		index = closing
	}
	return ctx.Err()
}

func validateJSONStringScalarEscapes(content []byte, document string, index int) (int, error) {
	for index < len(content) {
		switch content[index] {
		case '"':
			return index, nil
		case '\\':
			if index+1 >= len(content) {
				return len(content), nil
			}
			if content[index+1] != 'u' {
				index += 2
				continue
			}
			first, ok := decodeJSONUnicodeEscape(content, index)
			if !ok {
				index += 2
				continue
			}
			switch {
			case first >= 0xd800 && first <= 0xdbff:
				secondIndex := index + 6
				second, paired := decodeJSONUnicodeEscape(content, secondIndex)
				if !paired || second < 0xdc00 || second > 0xdfff {
					return 0, fmt.Errorf(
						"%s contains unpaired UTF-16 surrogate escape at byte %d",
						document,
						index,
					)
				}
				index = secondIndex + 6
			case first >= 0xdc00 && first <= 0xdfff:
				return 0, fmt.Errorf(
					"%s contains unpaired UTF-16 surrogate escape at byte %d",
					document,
					index,
				)
			default:
				index += 6
			}
		default:
			index++
		}
	}
	return len(content), nil
}

func decodeJSONUnicodeEscape(content []byte, index int) (uint16, bool) {
	if index+6 > len(content) || content[index] != '\\' || content[index+1] != 'u' {
		return 0, false
	}
	var value uint16
	for _, digit := range content[index+2 : index+6] {
		nibble, ok := jsonHexNibble(digit)
		if !ok {
			return 0, false
		}
		value = value<<4 | nibble
	}
	return value, true
}

func jsonHexNibble(digit byte) (uint16, bool) {
	switch {
	case digit >= '0' && digit <= '9':
		return uint16(digit - '0'), true
	case digit >= 'a' && digit <= 'f':
		return uint16(digit-'a') + 10, true
	case digit >= 'A' && digit <= 'F':
		return uint16(digit-'A') + 10, true
	default:
		return 0, false
	}
}

func isASCIILowerSnakeCase(value string) bool {
	if value == "" {
		return false
	}
	previousUnderscore := false
	for index := range len(value) {
		character := value[index]
		switch {
		case character >= 'a' && character <= 'z':
			previousUnderscore = false
		case index > 0 && character >= '0' && character <= '9':
			previousUnderscore = false
		case index > 0 && index < len(value)-1 && character == '_' && !previousUnderscore:
			previousUnderscore = true
		default:
			return false
		}
	}
	return true
}

func consumeClosingDelimiter(decoder *json.Decoder, document string, expected json.Delim) error {
	closing, err := decoder.Token()
	if err != nil {
		return fmt.Errorf("parse %s closing delimiter: %w", document, err)
	}
	if closing != expected {
		return fmt.Errorf("%s has invalid closing delimiter %v", document, closing)
	}
	return nil
}

func consumeClosingDelimiterContext(
	ctx context.Context,
	decoder *json.Decoder,
	document string,
	expected json.Delim,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	closing, err := decoder.Token()
	if err != nil {
		if contextErr := ctx.Err(); contextErr != nil {
			return contextErr
		}
		return fmt.Errorf("parse %s closing delimiter: %w", document, err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if closing != expected {
		return fmt.Errorf("%s has invalid closing delimiter %v", document, closing)
	}
	return nil
}
