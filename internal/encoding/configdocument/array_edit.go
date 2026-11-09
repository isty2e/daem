package configdocument

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"slices"

	"github.com/isty2e/daem/internal/output"
	"github.com/tailscale/hujson"
)

type jsonArraySource struct {
	array sourceSpan
	rows  []jsonArrayRow
}

type jsonArrayRow struct {
	value    sourceSpan
	comma    int
	comments []sourceSpan
}

func (document Document) jsonArraySource(ctx context.Context, path output.KeyPath) (jsonArraySource, error) {
	if document.root.data == nil || !document.exists {
		return jsonArraySource{}, fmt.Errorf("%w: array edit requires an existing document", ErrInvalidDocument)
	}
	return readJSONArraySource(ctx, document.content, document.syntax, document.limits, path)
}

func readJSONArraySource(ctx context.Context, content []byte, syntax Syntax, limits Limits, path output.KeyPath) (jsonArraySource, error) {
	if ctx == nil {
		return jsonArraySource{}, fmt.Errorf("array observation requires context")
	}
	if err := ctx.Err(); err != nil {
		return jsonArraySource{}, err
	}
	if err := limits.Validate(); err != nil {
		return jsonArraySource{}, err
	}
	if err := syntax.validate(); err != nil {
		return jsonArraySource{}, err
	}
	if int64(len(content)) > limits.MaximumBytes {
		return jsonArraySource{}, ErrLimitExceeded
	}
	if syntax == TOML || path.IsRoot() {
		return jsonArraySource{}, fmt.Errorf("%w: array edit requires a JSON property", ErrInvalidDocument)
	}
	if syntax == JSON && !json.Valid(content) {
		return jsonArraySource{}, ErrInvalidDocument
	}
	lexical, err := scanJSON(ctx, content, syntax, limits.MaximumDepth)
	if err != nil {
		return jsonArraySource{}, err
	}
	tree, err := parseJSONTree(lexical.normalized)
	if err != nil {
		return jsonArraySource{}, err
	}
	selected := &tree
	for _, segment := range path.Segments() {
		object, ok := selected.Value.(*hujson.Object)
		if !ok {
			return jsonArraySource{}, ErrPathConflict
		}
		var child *hujson.Value
		for index := range object.Members {
			if err := ctx.Err(); err != nil {
				return jsonArraySource{}, err
			}
			member := &object.Members[index]
			if member.Name.Value.(hujson.Literal).String() == segment {
				if child != nil {
					return jsonArraySource{}, fmt.Errorf("%w: array selection crosses a duplicate object property", ErrPathConflict)
				}
				child = &member.Value
			}
		}
		if child == nil {
			return jsonArraySource{}, fmt.Errorf("%w: selected array property is absent", ErrPathConflict)
		}
		selected = child
	}
	array, ok := selected.Value.(*hujson.Array)
	if !ok {
		return jsonArraySource{}, fmt.Errorf("%w: array property has incompatible concrete syntax", ErrPathConflict)
	}
	result := jsonArraySource{array: sourceSpan{selected.StartOffset, selected.EndOffset}}
	commentIndex := 0
	for _, row := range array.Elements {
		if err := ctx.Err(); err != nil {
			return jsonArraySource{}, err
		}
		for commentIndex < len(lexical.spans) && lexical.spans[commentIndex].end <= row.StartOffset {
			commentIndex++
		}
		firstComment := commentIndex
		for commentIndex < len(lexical.spans) && lexical.spans[commentIndex].start < row.EndOffset {
			commentIndex++
		}
		comma := row.EndOffset + len(row.AfterExtra)
		if comma >= selected.EndOffset || content[comma] != ',' {
			comma = -1
		}
		result.rows = append(result.rows, jsonArrayRow{
			value: sourceSpan{row.StartOffset, row.EndOffset}, comma: comma,
			comments: lexical.spans[firstComment:commentIndex],
		})
	}
	return result, nil
}

// EditJSONArray removes original slots, then permutes surviving literal rows.
// Slot trivia and removed-row comments survive; no rows or parent values are added.
func EditJSONArray(ctx context.Context, document Document, path output.KeyPath, removed, order []int) (Document, error) {
	source, err := document.jsonArraySource(ctx, path)
	if err != nil {
		return Document{}, err
	}
	array, err := source.edit(ctx, document.content, removed, order)
	if err != nil {
		return Document{}, err
	}
	content := make([]byte, 0, len(document.content))
	content = append(content, document.content[:source.array.start]...)
	content = append(content, array...)
	content = append(content, document.content[source.array.end:]...)
	return Parse(ctx, content, document.syntax, document.limits)
}

// EditJSONArraySource is the syntax-only boundary for existing semantic parsers.
// The semantic parser's literal rows must match this parser's view before any
// position is applied. Unrelated properties remain opaque to canonical semantics.
// Callers must reparse and verify the complete intended semantic rows afterward:
// equal literals can denote different tokens under different comment grammars.
func EditJSONArraySource(ctx context.Context, content []byte, syntax Syntax, limits Limits, path output.KeyPath, rowLiterals []string, removed, order []int) ([]byte, error) {
	source, err := readJSONArraySource(ctx, content, syntax, limits, path)
	if err != nil {
		return nil, err
	}
	if len(source.rows) != len(rowLiterals) {
		return nil, fmt.Errorf("%w: syntax row count differs from the semantic parser witness", ErrPathConflict)
	}
	for index, row := range source.rows {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if string(content[row.value.start:row.value.end]) != rowLiterals[index] {
			return nil, fmt.Errorf("%w: syntax row differs from the semantic parser witness", ErrPathConflict)
		}
	}
	array, err := source.edit(ctx, content, removed, order)
	if err != nil {
		return nil, err
	}
	candidate := make([]byte, 0, len(content))
	candidate = append(candidate, content[:source.array.start]...)
	candidate = append(candidate, array...)
	candidate = append(candidate, content[source.array.end:]...)
	if _, err := readJSONArraySource(ctx, candidate, syntax, limits, path); err != nil {
		return nil, err
	}
	return candidate, nil
}

func (source jsonArraySource) edit(ctx context.Context, original []byte, removed, order []int) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	previous := -1
	for _, index := range removed {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if index <= previous || index < 0 || index >= len(source.rows) {
			return nil, fmt.Errorf("%w: removed array slots must be sorted, unique and in range", ErrInvalidValue)
		}
		previous = index
	}
	survivors := make([]int, 0, len(source.rows)-len(removed))
	removedIndex := 0
	for index := range source.rows {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if removedIndex < len(removed) && removed[removedIndex] == index {
			removedIndex++
		} else {
			survivors = append(survivors, index)
		}
	}
	if len(order) != len(survivors) {
		return nil, fmt.Errorf("%w: array edit changes surviving row count", ErrInvalidValue)
	}
	seen := make([]bool, len(order))
	for _, index := range order {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if index < 0 || index >= len(order) || seen[index] {
			return nil, fmt.Errorf("%w: surviving array order is not a bijection", ErrInvalidValue)
		}
		seen[index] = true
	}

	content := make([]byte, 0, source.array.end-source.array.start)
	previous = source.array.start
	survivingSlot := 0
	trailingComma := len(source.rows) != 0 && source.rows[len(source.rows)-1].comma >= 0
	for index, row := range source.rows {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		content = append(content, original[previous:row.value.start]...)
		retained := survivingSlot < len(survivors) && survivors[survivingSlot] == index
		if retained {
			selected := source.rows[survivors[order[survivingSlot]]].value
			content = append(content, original[selected.start:selected.end]...)
			survivingSlot++
		} else {
			for _, comment := range row.comments {
				if err := ctx.Err(); err != nil {
					return nil, err
				}
				end := comment.end
				for end < row.value.end && isJSONWhitespace(original[end]) {
					if end%4096 == 0 {
						if err := ctx.Err(); err != nil {
							return nil, err
						}
					}
					end++
				}
				content = append(content, original[comment.start:end]...)
			}
		}
		previous = row.value.end
		if row.comma >= 0 {
			content = append(content, original[previous:row.comma]...)
			if retained && (survivingSlot < len(survivors) || trailingComma) {
				content = append(content, ',')
			}
			previous = row.comma + 1
		}
	}
	content = append(content, original[previous:source.array.end]...)
	return content, nil
}

func isJSONWhitespace(value byte) bool {
	return value == ' ' || value == '\t' || value == '\r' || value == '\n'
}

// JSONArrayEdit retains a constrained removal/permutation witness and the exact
// original/final array. A surviving-row planning baseline is not a recovery state.
type JSONArrayEdit struct {
	syntax   Syntax
	path     output.KeyPath
	removed  []int
	order    []int
	before   []byte
	expected []byte
}

func (edit JSONArrayEdit) ArrayPath() output.KeyPath { return edit.path }
func (edit JSONArrayEdit) Syntax() Syntax            { return edit.syntax }
func (edit JSONArrayEdit) RemovedSlots() []int       { return slices.Clone(edit.removed) }
func (edit JSONArrayEdit) SurvivorOrder() []int      { return slices.Clone(edit.order) }

func (edit JSONArrayEdit) Equal(other JSONArrayEdit) bool {
	return edit.path == other.path && edit.syntax == other.syntax &&
		slices.Equal(edit.removed, other.removed) && slices.Equal(edit.order, other.order) &&
		bytes.Equal(edit.before, other.before) && bytes.Equal(edit.expected, other.expected)
}

// PrepareJSONArrayEdit derives a positional witness without exposing a second
// whole-document candidate. Semantic row identity stays with the normalizer.
func PrepareJSONArrayEdit(ctx context.Context, before Document, path output.KeyPath, removed, order []int) (JSONArrayEdit, error) {
	if path.IsRoot() {
		return JSONArrayEdit{}, fmt.Errorf("%w: selected array requires a non-root path", ErrInvalidValue)
	}
	source, err := before.jsonArraySource(ctx, path)
	if err != nil {
		return JSONArrayEdit{}, err
	}
	array, err := source.edit(ctx, before.content, removed, order)
	if err != nil {
		return JSONArrayEdit{}, err
	}
	expected, _, err := replaceCapturedArray(ctx, before, source, array)
	if err != nil {
		return JSONArrayEdit{}, err
	}
	return NewJSONArrayEdit(ctx, before, expected, path, removed, order)
}

func NewJSONArrayEdit(ctx context.Context, before, expected Document, path output.KeyPath, removed, order []int) (JSONArrayEdit, error) {
	if ctx == nil {
		return JSONArrayEdit{}, fmt.Errorf("array edit capture requires context")
	}
	if err := ctx.Err(); err != nil {
		return JSONArrayEdit{}, err
	}
	if before.syntax != expected.syntax {
		return JSONArrayEdit{}, fmt.Errorf("%w: array edit changes document grammar", ErrInvalidDocument)
	}
	left, err := before.jsonArraySource(ctx, path)
	if err != nil {
		return JSONArrayEdit{}, err
	}
	right, err := expected.jsonArraySource(ctx, path)
	if err != nil {
		return JSONArrayEdit{}, err
	}

	edited, err := left.edit(ctx, before.content, removed, order)
	if err != nil {
		return JSONArrayEdit{}, err
	}
	want := expected.content[right.array.start:right.array.end]
	if !bytes.Equal(edited, want) {
		return JSONArrayEdit{}, fmt.Errorf("%w: array edit differs from positional replay", ErrInvalidValue)
	}
	return JSONArrayEdit{
		syntax: before.syntax, path: path, removed: slices.Clone(removed), order: slices.Clone(order),
		before:   slices.Clone(before.content[left.array.start:left.array.end]),
		expected: slices.Clone(want),
	}, nil
}

func (edit JSONArrayEdit) selected(ctx context.Context, document Document) (jsonArraySource, bool, error) {
	if ctx == nil {
		return jsonArraySource{}, false, fmt.Errorf("array edit comparison requires context")
	}
	if err := ctx.Err(); err != nil {
		return jsonArraySource{}, false, err
	}
	if edit.syntax == 0 || edit.path.IsRoot() || len(edit.before) == 0 || len(edit.expected) == 0 || edit.syntax != document.syntax {
		return jsonArraySource{}, false, fmt.Errorf("%w: array edit has no compatible captured state", ErrInvalidValue)
	}
	_, present, err := document.Lookup(edit.path)
	if err != nil || !present {
		return jsonArraySource{}, false, err
	}
	source, err := document.jsonArraySource(ctx, edit.path)
	return source, err == nil, err
}

func (edit JSONArrayEdit) Match(ctx context.Context, document Document) (before, expected bool, err error) {
	source, present, err := edit.selected(ctx, document)
	if err != nil || !present {
		return false, false, err
	}
	current := document.content[source.array.start:source.array.end]
	return bytes.Equal(current, edit.before), bytes.Equal(current, edit.expected), nil
}

func (edit JSONArrayEdit) MatchesBefore(ctx context.Context, document Document) (bool, error) {
	before, _, err := edit.Match(ctx, document)
	return before, err
}

func (edit JSONArrayEdit) MatchesExpected(ctx context.Context, document Document) (bool, error) {
	_, expected, err := edit.Match(ctx, document)
	return expected, err
}

// Apply requires the captured original array, even when the current array already
// matches the expected state. Changes outside the selected array are retained.
func (edit JSONArrayEdit) Apply(ctx context.Context, document Document) (Document, bool, error) {
	source, present, err := edit.selected(ctx, document)
	if err != nil {
		return Document{}, false, err
	}
	if !present {
		return Document{}, false, fmt.Errorf("%w: captured array is absent", ErrPathConflict)
	}
	current := document.content[source.array.start:source.array.end]
	if !bytes.Equal(current, edit.before) {
		return Document{}, false, fmt.Errorf("%w: array differs from captured original", ErrPathConflict)
	}
	if bytes.Equal(current, edit.expected) {
		return document, false, nil
	}
	return replaceCapturedArray(ctx, document, source, edit.expected)
}

func (edit JSONArrayEdit) Restore(ctx context.Context, document Document) (Document, bool, error) {
	source, present, err := edit.selected(ctx, document)
	if err != nil {
		return Document{}, false, err
	}
	if !present {
		return Document{}, false, fmt.Errorf("%w: captured array is absent", ErrPathConflict)
	}
	current := document.content[source.array.start:source.array.end]
	if bytes.Equal(current, edit.before) {
		return document, false, nil
	}
	if !bytes.Equal(current, edit.expected) {
		return Document{}, false, fmt.Errorf("%w: array differs from captured edit states", ErrPathConflict)
	}
	return replaceCapturedArray(ctx, document, source, edit.before)
}

func replaceCapturedArray(ctx context.Context, document Document, source jsonArraySource, replacement []byte) (Document, bool, error) {
	content := make([]byte, 0, len(document.content)-(source.array.end-source.array.start)+len(replacement))
	content = append(content, document.content[:source.array.start]...)
	content = append(content, replacement...)
	content = append(content, document.content[source.array.end:]...)
	restored, err := Parse(ctx, content, document.syntax, document.limits)
	if err != nil {
		return Document{}, false, err
	}
	return restored, true, nil
}
