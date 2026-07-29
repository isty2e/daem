package opencode

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/tailscale/hujson"
)

// Document is one parsed OpenCode config document. It owns only the syntax and
// exact plugin-row semantics needed for passive observation and safe removal.
type Document struct {
	content    []byte
	sourcePath string
	root       hujson.Value
	entries    []Entry
}

// Entry is one supported OpenCode plugin row.
type Entry struct {
	source           string
	hostLoadIdentity string
}

// Parse validates one OpenCode JSONC document without normalizing its bytes.
func Parse(content []byte) (Document, error) {
	return ParseAt(content, "")
}

// ParseAt validates one OpenCode JSONC document and resolves path-like plugin
// rows relative to the config document that declared them.
func ParseAt(content []byte, sourcePath string) (Document, error) {
	owned := append([]byte(nil), content...)
	root, err := hujson.Parse(owned)
	if err != nil {
		return Document{}, fmt.Errorf("parse OpenCode config JSONC: %w", err)
	}
	entries, err := pluginEntries(root, sourcePath)
	if err != nil {
		return Document{}, err
	}
	return Document{
		content:    owned,
		sourcePath: sourcePath,
		root:       root,
		entries:    entries,
	}, nil
}

// Source returns the canonical source relation represented by this row.
func (entry Entry) Source() string { return entry.source }

// HostLoadIdentity returns the package or canonical file identity OpenCode
// uses when later config layers override an earlier row.
func (entry Entry) HostLoadIdentity() string { return entry.hostLoadIdentity }

// Entries returns an immutable copy of the supported plugin rows in document
// order.
func (document Document) Entries() []Entry {
	return append([]Entry(nil), document.entries...)
}

// ExactSourceCount returns the number of rows carrying source exactly.
func (document Document) ExactSourceCount(source string) int {
	count := 0
	for _, entry := range document.entries {
		if entry.source == source {
			count++
		}
	}
	return count
}

// RemoveExactSource removes one uniquely correlated plugin row. Absence is an
// exact no-op; duplicate exact rows are ambiguous and refuse mutation.
func (document Document) RemoveExactSource(source string) ([]byte, bool, error) {
	if err := validateSource(source); err != nil {
		return nil, false, err
	}
	switch count := document.ExactSourceCount(source); count {
	case 0:
		return append([]byte(nil), document.content...), false, nil
	case 1:
	default:
		return nil, false, fmt.Errorf(
			"OpenCode config contains %d exact plugin rows for source %q",
			count,
			source,
		)
	}

	object, ok := document.root.Value.(*hujson.Object)
	if !ok {
		return nil, false, fmt.Errorf("OpenCode config root must be an object")
	}
	pluginArray, err := uniquePluginArray(object)
	if err != nil {
		return nil, false, err
	}
	if pluginArray == nil {
		return nil, false, fmt.Errorf("OpenCode plugin row disappeared from parsed document")
	}
	for index, value := range pluginArray.Elements {
		entry, err := pluginEntry(value, document.sourcePath)
		if err != nil {
			return nil, false, err
		}
		if entry.source != source {
			continue
		}
		output, err := removeArrayElement(document.content, pluginArray, index)
		if err != nil {
			return nil, false, err
		}
		if _, err := Parse(output); err != nil {
			return nil, false, fmt.Errorf("validate edited OpenCode config: %w", err)
		}
		return output, true, nil
	}
	return nil, false, fmt.Errorf("OpenCode plugin row disappeared from parsed document")
}

func removeArrayElement(content []byte, array *hujson.Array, index int) ([]byte, error) {
	if index < 0 || index >= len(array.Elements) {
		return nil, fmt.Errorf("OpenCode plugin row index %d is out of range", index)
	}
	element := array.Elements[index]
	start := element.StartOffset - len(element.BeforeExtra)
	end := element.EndOffset + len(element.AfterExtra)
	if start < 0 || end < start || end > len(content) {
		return nil, fmt.Errorf("OpenCode plugin row has invalid syntax offsets")
	}

	if end < len(content) && content[end] == ',' {
		end++
	} else if index > 0 {
		previous := array.Elements[index-1]
		separator := previous.EndOffset + len(previous.AfterExtra)
		if separator < 0 || separator >= start || content[separator] != ',' {
			return nil, fmt.Errorf("OpenCode plugin row has no correlated array separator")
		}
		start = separator
	}

	output := make([]byte, 0, len(content)-(end-start))
	output = append(output, content[:start]...)
	output = append(output, content[end:]...)
	return output, nil
}

func pluginEntries(root hujson.Value, sourcePath string) ([]Entry, error) {
	object, ok := root.Value.(*hujson.Object)
	if !ok {
		return nil, fmt.Errorf("OpenCode config root must be an object")
	}
	pluginArray, err := uniquePluginArray(object)
	if err != nil || pluginArray == nil {
		return nil, err
	}
	entries := make([]Entry, 0, len(pluginArray.Elements))
	for index, value := range pluginArray.Elements {
		entry, err := pluginEntry(value, sourcePath)
		if err != nil {
			return nil, fmt.Errorf("OpenCode plugin row[%d]: %w", index, err)
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

func uniquePluginArray(object *hujson.Object) (*hujson.Array, error) {
	var selected *hujson.Array
	matches := 0
	for _, member := range object.Members {
		name, ok := member.Name.Value.(hujson.Literal)
		if !ok || name.Kind() != '"' {
			return nil, fmt.Errorf("OpenCode config object member name must be a string")
		}
		if name.String() != "plugin" {
			continue
		}
		matches++
		array, ok := member.Value.Value.(*hujson.Array)
		if !ok {
			return nil, fmt.Errorf("OpenCode config plugin field must be an array")
		}
		selected = array
	}
	if matches > 1 {
		return nil, fmt.Errorf("OpenCode config contains duplicate plugin fields")
	}
	return selected, nil
}

func pluginEntry(value hujson.Value, sourcePath string) (Entry, error) {
	if literal, ok := value.Value.(hujson.Literal); ok {
		if literal.Kind() != '"' {
			return Entry{}, fmt.Errorf("plugin row must be a string or [string, object]")
		}
		return newEntry(literal.String(), sourcePath)
	}
	tuple, ok := value.Value.(*hujson.Array)
	if !ok || len(tuple.Elements) != 2 {
		return Entry{}, fmt.Errorf("plugin row must be a string or [string, object]")
	}
	source, ok := tuple.Elements[0].Value.(hujson.Literal)
	if !ok || source.Kind() != '"' {
		return Entry{}, fmt.Errorf("plugin tuple source must be a string")
	}
	if _, ok := tuple.Elements[1].Value.(*hujson.Object); !ok {
		return Entry{}, fmt.Errorf("plugin tuple options must be an object")
	}
	return newEntry(source.String(), sourcePath)
}

func newEntry(source string, sourcePath string) (Entry, error) {
	if err := validateSource(source); err != nil {
		return Entry{}, err
	}
	canonical, loadIdentity, err := pluginSourceIdentity(source, sourcePath)
	if err != nil {
		return Entry{}, err
	}
	return Entry{source: canonical, hostLoadIdentity: loadIdentity}, nil
}

func validateSource(source string) error {
	if source == "" || strings.TrimSpace(source) != source {
		return fmt.Errorf("OpenCode plugin source must be non-empty and trimmed")
	}
	if strings.IndexFunc(source, func(value rune) bool {
		return unicode.IsControl(value) || unicode.Is(unicode.Bidi_Control, value)
	}) >= 0 {
		return fmt.Errorf("OpenCode plugin source must not contain control characters")
	}
	return nil
}
