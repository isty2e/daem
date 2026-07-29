// Package pi owns the strict-JSON syntax needed to observe and reorder Pi
// package settings without reserializing unrelated content.
package pi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"unicode"

	"github.com/isty2e/daem/internal/encoding/jsonstrict"
	"github.com/tailscale/hujson"
)

const maximumSettingsDepth = 32

// Document is one parsed Pi settings document with exact package-row spans.
type Document struct {
	content []byte
	entries []Entry
	spans   []rowSpan
}

// Entry is one exact Pi package source in physical settings order.
type Entry struct {
	source string
}

type rowSpan struct {
	start int
	end   int
}

// Parse validates strict Pi JSON without normalizing its bytes.
func Parse(content []byte) (Document, error) {
	if err := jsonstrict.Validate(content, "Pi settings", maximumSettingsDepth); err != nil {
		return Document{}, err
	}
	owned := bytes.Clone(content)
	root, err := hujson.Parse(owned)
	if err != nil {
		return Document{}, fmt.Errorf("parse Pi settings JSON: %w", err)
	}
	object, ok := root.Value.(*hujson.Object)
	if !ok {
		return Document{}, fmt.Errorf("Pi settings root must be an object")
	}
	array, err := packageArray(object)
	if err != nil || array == nil {
		return Document{content: owned}, err
	}

	entries := make([]Entry, 0, len(array.Elements))
	spans := make([]rowSpan, 0, len(array.Elements))
	for index, value := range array.Elements {
		start, end := value.StartOffset, value.EndOffset
		if start < 0 || end < start || end > len(owned) {
			return Document{}, fmt.Errorf("Pi package row[%d] has invalid syntax offsets", index)
		}
		source, err := decodePackageSource(owned[start:end])
		if err != nil {
			return Document{}, fmt.Errorf("packages[%d]: %w", index, err)
		}
		entries = append(entries, Entry{source: source})
		spans = append(spans, rowSpan{start: start, end: end})
	}
	return Document{content: owned, entries: entries, spans: spans}, nil
}

// Source returns the exact source spelling stored in this row.
func (entry Entry) Source() string { return entry.source }

// Entries returns package rows in exact physical order.
func (document Document) Entries() []Entry {
	return append([]Entry(nil), document.entries...)
}

// PermutePackageRows returns a document whose destination row i contains the
// complete original row at order[i]. Whitespace and separators remain attached
// to destination slots rather than moving with row values.
func (document Document) PermutePackageRows(order []int) ([]byte, bool, error) {
	if len(order) != len(document.spans) {
		return nil, false, fmt.Errorf(
			"Pi package row permutation has %d indexes for %d rows",
			len(order),
			len(document.spans),
		)
	}
	seen := make([]bool, len(order))
	for destination, source := range order {
		if source < 0 || source >= len(order) {
			return nil, false, fmt.Errorf(
				"Pi package row permutation source[%d] %d is out of range",
				destination,
				source,
			)
		}
		if seen[source] {
			return nil, false, fmt.Errorf(
				"Pi package row permutation source index %d appears more than once",
				source,
			)
		}
		seen[source] = true
	}

	output := make([]byte, 0, len(document.content))
	previousEnd := 0
	for destination, source := range order {
		destinationSpan := document.spans[destination]
		sourceSpan := document.spans[source]
		output = append(output, document.content[previousEnd:destinationSpan.start]...)
		output = append(output, document.content[sourceSpan.start:sourceSpan.end]...)
		previousEnd = destinationSpan.end
	}
	output = append(output, document.content[previousEnd:]...)

	candidate, err := Parse(output)
	if err != nil {
		return nil, false, fmt.Errorf("validate reordered Pi settings: %w", err)
	}
	for destination, source := range order {
		if candidate.entries[destination].source != document.entries[source].source {
			return nil, false, fmt.Errorf(
				"reordered Pi package row[%d] does not preserve source semantics",
				destination,
			)
		}
	}
	return output, !bytes.Equal(output, document.content), nil
}

// ValidatePackageSource rejects source text Pi settings cannot safely carry.
func ValidatePackageSource(source string) error {
	if strings.TrimSpace(source) == "" || strings.TrimSpace(source) != source {
		return fmt.Errorf("source must be non-empty and trimmed")
	}
	if strings.IndexFunc(source, func(character rune) bool {
		return unicode.IsControl(character) || unicode.Is(unicode.Bidi_Control, character)
	}) >= 0 {
		return fmt.Errorf("source must not contain control or bidirectional formatting characters")
	}
	return nil
}

func packageArray(object *hujson.Object) (*hujson.Array, error) {
	for _, member := range object.Members {
		name, ok := member.Name.Value.(hujson.Literal)
		if !ok || name.Kind() != '"' {
			return nil, fmt.Errorf("Pi settings object member name must be a string")
		}
		if name.String() != "packages" {
			continue
		}
		array, ok := member.Value.Value.(*hujson.Array)
		if !ok {
			if literal, literalOK := member.Value.Value.(hujson.Literal); literalOK &&
				literal.Kind() == 'n' {
				return nil, fmt.Errorf("packages must be an array when present")
			}
			return nil, fmt.Errorf("packages: cannot unmarshal non-array value")
		}
		return array, nil
	}
	return nil, nil
}

func decodePackageSource(raw []byte) (string, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return "", fmt.Errorf("must be a string or object with a source string")
	}
	var source string
	if trimmed[0] == '"' {
		if err := json.Unmarshal(trimmed, &source); err != nil {
			return "", fmt.Errorf("source string: %w", err)
		}
		if err := ValidatePackageSource(source); err != nil {
			return "", err
		}
		return source, nil
	}
	if trimmed[0] != '{' {
		return "", fmt.Errorf("must be a string or object with a source string")
	}

	var entry map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &entry); err != nil {
		return "", fmt.Errorf("must be a string or object with a source string")
	}
	rawSource, present := entry["source"]
	if !present {
		return "", fmt.Errorf("object source is required")
	}
	trimmedSource := bytes.TrimSpace(rawSource)
	if len(trimmedSource) == 0 || trimmedSource[0] != '"' {
		return "", fmt.Errorf("object source must be a string")
	}
	if err := json.Unmarshal(trimmedSource, &source); err != nil {
		return "", fmt.Errorf("object source must be a string")
	}
	if err := ValidatePackageSource(source); err != nil {
		return "", err
	}
	return source, nil
}
