package opencode

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"unicode"

	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/output"
	"github.com/tailscale/hujson"
)

// Document retains parsed OpenCode plugin semantics and unchanged source bytes.
type Document struct {
	content    []byte
	sourcePath string
	entries    []Entry
}

// Entry is one supported OpenCode plugin row.
type Entry struct {
	source           string
	hostLoadIdentity string
	literal          string
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
	entries, err := pluginEntries(root, sourcePath, owned)
	if err != nil {
		return Document{}, err
	}
	return Document{
		content:    owned,
		sourcePath: sourcePath,
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

// PermutePluginRows returns a document whose destination row i contains the
// complete original row at order[i]. JSONC extras and separators remain
// attached to destination slots, while tuple values move without reencoding.
func (document Document) PermutePluginRows(order []int) ([]byte, bool, error) {
	if len(order) != len(document.entries) {
		return nil, false, fmt.Errorf(
			"OpenCode plugin row permutation has %d indexes for %d rows",
			len(order),
			len(document.entries),
		)
	}
	unchanged := true
	for destination, source := range order {
		unchanged = unchanged && destination == source
	}
	if unchanged {
		return bytes.Clone(document.content), false, nil
	}
	output, err := document.editRows(nil, order)
	if err != nil {
		return nil, false, err
	}
	candidate, err := ParseAt(output, document.sourcePath)
	if err != nil {
		return nil, false, fmt.Errorf("validate reordered OpenCode config: %w", err)
	}
	if len(candidate.entries) != len(order) {
		return nil, false, fmt.Errorf("reordered OpenCode plugin row count changed")
	}
	for destination, source := range order {
		got := candidate.entries[destination]
		want := document.entries[source]
		if got != want {
			return nil, false, fmt.Errorf(
				"reordered OpenCode plugin row[%d] does not preserve source semantics",
				destination,
			)
		}
	}
	return output, !bytes.Equal(output, document.content), nil
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

	for index, entry := range document.entries {
		if entry.source != source {
			continue
		}
		order := make([]int, len(document.entries)-1)
		for slot := range order {
			order[slot] = slot
		}
		output, err := document.editRows([]int{index}, order)
		if err != nil {
			return nil, false, err
		}
		candidate, err := ParseAt(output, document.sourcePath)
		if err != nil {
			return nil, false, fmt.Errorf("validate edited OpenCode config: %w", err)
		}
		if len(candidate.entries) != len(document.entries)-1 {
			return nil, false, fmt.Errorf("removed OpenCode plugin row count changed")
		}
		remaining := 0
		for original, retained := range document.entries {
			if original == index {
				continue
			}
			if candidate.entries[remaining] != retained {
				return nil, false, fmt.Errorf("removed OpenCode plugin changed a surviving literal row")
			}
			remaining++
		}
		return output, true, nil
	}
	return nil, false, fmt.Errorf("OpenCode plugin row disappeared from parsed document")
}

func (document Document) editRows(removed, order []int) ([]byte, error) {
	path, err := output.NewKeyPath("plugin")
	if err != nil {
		return nil, err
	}
	// Parsing already owns input admission. These ceilings preserve that input's
	// envelope; removals and literal permutations cannot enlarge it.
	limits := configdocument.Limits{MaximumBytes: int64(max(1, len(document.content))), MaximumDepth: max(1, len(document.content))}
	rows := make([]string, len(document.entries))
	for index, entry := range document.entries {
		rows[index] = entry.literal
	}
	return configdocument.EditJSONArraySource(context.Background(), document.content, configdocument.JSONC, limits, path, rows, removed, order)
}

func pluginEntries(
	root hujson.Value,
	sourcePath string,
	content []byte,
) ([]Entry, error) {
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
		if value.StartOffset < 0 || value.EndOffset < value.StartOffset || value.EndOffset > len(content) {
			return nil, fmt.Errorf("OpenCode plugin row has invalid literal offsets")
		}
		entry.literal = string(content[value.StartOffset:value.EndOffset])
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
