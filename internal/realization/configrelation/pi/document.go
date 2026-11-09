// Package pi owns the strict-JSON syntax needed to observe and reorder Pi
// package settings without reserializing unrelated content.
package pi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"unicode"

	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/encoding/jsonstrict"
	"github.com/isty2e/daem/internal/output"
	"github.com/tailscale/hujson"
)

const maximumSettingsDepth = 32

// Document is one parsed Pi settings document with exact package-row spans.
type Document struct {
	content     []byte
	entries     []Entry
	spans       []valueSpan
	packageSpan valueSpan
}

// Entry is one exact Pi package source in physical settings order.
type Entry struct {
	source string
}

type valueSpan struct {
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
	value, err := packageArray(object)
	if err != nil {
		return Document{}, err
	}
	if value == nil {
		return Document{content: owned}, nil
	}
	start, end := value.StartOffset, value.EndOffset
	if start < 0 || end <= start || end > len(owned) {
		return Document{}, fmt.Errorf("Pi package array has invalid syntax offsets")
	}
	array := value.Value.(*hujson.Array)

	entries := make([]Entry, 0, len(array.Elements))
	spans := make([]valueSpan, 0, len(array.Elements))
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
		spans = append(spans, valueSpan{start: start, end: end})
	}
	return Document{
		content:     owned,
		entries:     entries,
		spans:       spans,
		packageSpan: valueSpan{start: start, end: end},
	}, nil
}

// Source returns the exact source spelling stored in this row.
func (entry Entry) Source() string { return entry.source }

// Entries returns package rows in exact physical order.
func (document Document) Entries() []Entry {
	return append([]Entry(nil), document.entries...)
}

// HasPackages distinguishes a present array from an absent packages property.
func (document Document) HasPackages() bool { return document.packageSpan.end != 0 }

// PackageArrayEqual compares the entire literal array, including row options
// and slot whitespace, while ignoring other root properties. Unparsed documents
// are not evidence of an absent array.
func (document Document) PackageArrayEqual(other Document) bool {
	if document.content == nil || other.content == nil || document.HasPackages() != other.HasPackages() {
		return false
	}
	if !document.HasPackages() {
		return true
	}
	return bytes.Equal(
		document.content[document.packageSpan.start:document.packageSpan.end],
		other.content[other.packageSpan.start:other.packageSpan.end],
	)
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
	unchanged := true
	for destination, source := range order {
		unchanged = unchanged && destination == source
	}
	if unchanged {
		return bytes.Clone(document.content), false, nil
	}
	path, err := output.NewKeyPath("packages")
	if err != nil {
		return nil, false, err
	}
	// jsonstrict starts at depth zero; the token scanner counts the root container.
	limits := configdocument.Limits{MaximumBytes: int64(len(document.content)), MaximumDepth: maximumSettingsDepth + 1}
	rows := make([]string, len(document.spans))
	for index, span := range document.spans {
		rows[index] = string(document.content[span.start:span.end])
	}
	content, err := configdocument.EditJSONArraySource(context.Background(), document.content, configdocument.JSON, limits, path, rows, nil, order)
	if err != nil {
		return nil, false, err
	}
	candidate, err := Parse(content)
	if err != nil {
		return nil, false, fmt.Errorf("validate reordered Pi settings: %w", err)
	}
	if len(candidate.entries) != len(order) {
		return nil, false, fmt.Errorf("reordered Pi package row count changed")
	}
	for destination, source := range order {
		if candidate.entries[destination].source != document.entries[source].source {
			return nil, false, fmt.Errorf(
				"reordered Pi package row[%d] does not preserve source semantics",
				destination,
			)
		}
	}
	return content, !bytes.Equal(content, document.content), nil
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

func packageArray(object *hujson.Object) (*hujson.Value, error) {
	for index := range object.Members {
		member := &object.Members[index]
		name, ok := member.Name.Value.(hujson.Literal)
		if !ok || name.Kind() != '"' {
			return nil, fmt.Errorf("Pi settings object member name must be a string")
		}
		if name.String() != "packages" {
			continue
		}
		_, ok = member.Value.Value.(*hujson.Array)
		if !ok {
			if literal, literalOK := member.Value.Value.(hujson.Literal); literalOK &&
				literal.Kind() == 'n' {
				return nil, fmt.Errorf("packages must be an array when present")
			}
			return nil, fmt.Errorf("packages: cannot unmarshal non-array value")
		}
		return &member.Value, nil
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
