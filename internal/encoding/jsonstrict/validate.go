// Package jsonstrict validates JSON syntax shared by persistence and host-document boundaries.
package jsonstrict

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode/utf8"
)

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
	return validate(content, document, maximumDepth, false)
}

func validate(content []byte, document string, maximumDepth int, canonicalObjectKeys bool) error {
	if strings.TrimSpace(document) == "" {
		return fmt.Errorf("JSON document label is required")
	}
	if maximumDepth <= 0 {
		return fmt.Errorf("%s JSON maximum depth must be positive", document)
	}
	if !utf8.Valid(content) {
		return fmt.Errorf("%s is not valid UTF-8", document)
	}
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.UseNumber()
	if err := consumeValue(decoder, document, maximumDepth, 0, canonicalObjectKeys); err != nil {
		return err
	}
	if token, err := decoder.Token(); err == nil {
		return classifiedError{
			kind:    ErrMultipleValues,
			message: fmt.Sprintf("%s contains multiple JSON values beginning with %v", document, token),
		}
	} else if err != io.EOF {
		return fmt.Errorf("parse %s trailer: %w", document, err)
	}
	return nil
}

// ValidateVersionedObject validates one strict versioned persistence object
// and returns its required positive integer version. Every object key must use
// canonical ASCII lower_snake_case spelling; schema-specific fields stay opaque.
func ValidateVersionedObject(content []byte, document string, maximumDepth int) (int, error) {
	if err := validate(content, document, maximumDepth, true); err != nil {
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
	decoder *json.Decoder,
	document string,
	maximumDepth int,
	depth int,
	canonicalObjectKeys bool,
) error {
	if depth > maximumDepth {
		return classifiedError{
			kind:    ErrMaximumDepthExceeded,
			message: fmt.Sprintf("%s JSON exceeds maximum depth %d", document, maximumDepth),
		}
	}
	token, err := decoder.Token()
	if err != nil {
		return fmt.Errorf("parse %s: %w", document, err)
	}
	delimiter, composite := token.(json.Delim)
	if !composite {
		return nil
	}
	switch delimiter {
	case '{':
		seen := make(map[string]struct{})
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return fmt.Errorf("parse %s object key: %w", document, err)
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
				decoder,
				document,
				maximumDepth,
				depth+1,
				canonicalObjectKeys,
			); err != nil {
				return err
			}
		}
		return consumeClosingDelimiter(decoder, document, '}')
	case '[':
		for decoder.More() {
			if err := consumeValue(
				decoder,
				document,
				maximumDepth,
				depth+1,
				canonicalObjectKeys,
			); err != nil {
				return err
			}
		}
		return consumeClosingDelimiter(decoder, document, ']')
	default:
		return fmt.Errorf("%s has unexpected JSON delimiter %q", document, delimiter)
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
