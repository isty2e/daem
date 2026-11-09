package configdocument

import (
	"bytes"
	"context"
	"fmt"
	"slices"

	"github.com/isty2e/daem/internal/output"
	"github.com/tailscale/hujson"
)

type jsonArraySource struct {
	array sourceSpan
	rows  []sourceSpan
}

func (document Document) jsonArraySource(ctx context.Context, path output.KeyPath) (jsonArraySource, error) {
	if ctx == nil {
		return jsonArraySource{}, fmt.Errorf("array observation requires context")
	}
	if err := ctx.Err(); err != nil {
		return jsonArraySource{}, err
	}
	if document.root.data == nil || !document.exists || document.syntax == TOML || path.IsRoot() {
		return jsonArraySource{}, fmt.Errorf("%w: array permutation requires an existing JSON property", ErrInvalidDocument)
	}
	value, present, err := document.Lookup(path)
	if err != nil {
		return jsonArraySource{}, err
	}
	if _, array := value.data.(arrayNode); !present || !array {
		return jsonArraySource{}, fmt.Errorf("%w: selected property is not an array", ErrPathConflict)
	}
	lexical, err := scanJSON(ctx, document.content, document.syntax, document.limits.MaximumDepth)
	if err != nil {
		return jsonArraySource{}, err
	}
	tree, err := parseJSONTree(lexical.normalized)
	if err != nil {
		return jsonArraySource{}, err
	}
	selected := tree.Find(path.String())
	if selected == nil {
		return jsonArraySource{}, fmt.Errorf("%w: array property has no concrete syntax", ErrPathConflict)
	}
	array, ok := selected.Value.(*hujson.Array)
	if !ok {
		return jsonArraySource{}, fmt.Errorf("%w: array property has incompatible concrete syntax", ErrPathConflict)
	}
	result := jsonArraySource{array: sourceSpan{selected.StartOffset, selected.EndOffset}}
	for _, row := range array.Elements {
		if err := ctx.Err(); err != nil {
			return jsonArraySource{}, err
		}
		result.rows = append(result.rows, sourceSpan{row.StartOffset, row.EndOffset})
	}
	return result, nil
}

// PermuteJSONArray moves literal row values while retaining destination-slot
// trivia and every byte outside the selected array. It does not change membership.
func PermuteJSONArray(ctx context.Context, document Document, path output.KeyPath, order []int) (Document, error) {
	source, err := document.jsonArraySource(ctx, path)
	if err != nil {
		return Document{}, err
	}
	array, err := source.permute(ctx, document.content, order)
	if err != nil {
		return Document{}, err
	}
	content := make([]byte, 0, len(document.content))
	content = append(content, document.content[:source.array.start]...)
	content = append(content, array...)
	content = append(content, document.content[source.array.end:]...)
	return Parse(ctx, content, document.syntax, document.limits)
}

func (source jsonArraySource) permute(ctx context.Context, original []byte, order []int) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(order) != len(source.rows) {
		return nil, fmt.Errorf("%w: array permutation changes row count", ErrInvalidValue)
	}
	seen := make([]bool, len(order))
	content := make([]byte, 0, source.array.end-source.array.start)
	previous := source.array.start
	for slot, row := range order {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if row < 0 || row >= len(order) || seen[row] {
			return nil, fmt.Errorf("%w: array permutation is not a bijection", ErrInvalidValue)
		}
		seen[row] = true
		destination, selected := source.rows[slot], source.rows[row]
		content = append(content, original[previous:destination.start]...)
		content = append(content, original[selected.start:selected.end]...)
		previous = destination.end
	}
	content = append(content, original[previous:source.array.end]...)
	return content, nil
}

// JSONArrayPermutation retains the exact selected array before and after one
// row permutation. Concurrent changes outside that array are not its state.
type JSONArrayPermutation struct {
	syntax   Syntax
	path     output.KeyPath
	before   []byte
	expected []byte
}

func (permutation JSONArrayPermutation) ArrayPath() output.KeyPath { return permutation.path }
func (permutation JSONArrayPermutation) Syntax() Syntax            { return permutation.syntax }

func (permutation JSONArrayPermutation) Equal(other JSONArrayPermutation) bool {
	return permutation.path == other.path && permutation.syntax == other.syntax &&
		bytes.Equal(permutation.before, other.before) && bytes.Equal(permutation.expected, other.expected)
}

func NewJSONArrayPermutation(ctx context.Context, before, expected Document, path output.KeyPath) (JSONArrayPermutation, error) {
	if ctx == nil {
		return JSONArrayPermutation{}, fmt.Errorf("array permutation capture requires context")
	}
	if err := ctx.Err(); err != nil {
		return JSONArrayPermutation{}, err
	}
	if before.syntax != expected.syntax {
		return JSONArrayPermutation{}, fmt.Errorf("%w: array permutation changes document grammar", ErrInvalidDocument)
	}
	left, err := before.jsonArraySource(ctx, path)
	if err != nil {
		return JSONArrayPermutation{}, err
	}
	right, err := expected.jsonArraySource(ctx, path)
	if err != nil {
		return JSONArrayPermutation{}, err
	}
	if len(left.rows) != len(right.rows) {
		return JSONArrayPermutation{}, fmt.Errorf("%w: array permutation changes membership", ErrInvalidValue)
	}
	positions := make(map[string][]int, len(left.rows))
	for index, row := range left.rows {
		if err := ctx.Err(); err != nil {
			return JSONArrayPermutation{}, err
		}
		literal := string(before.content[row.start:row.end])
		positions[literal] = append(positions[literal], index)
	}
	order := make([]int, 0, len(right.rows))
	for _, row := range right.rows {
		if err := ctx.Err(); err != nil {
			return JSONArrayPermutation{}, err
		}
		literal := string(expected.content[row.start:row.end])
		remaining := positions[literal]
		if len(remaining) == 0 {
			return JSONArrayPermutation{}, fmt.Errorf("%w: array permutation changes a literal row", ErrInvalidValue)
		}
		order = append(order, remaining[0])
		positions[literal] = remaining[1:]
	}
	permuted, err := left.permute(ctx, before.content, order)
	if err != nil {
		return JSONArrayPermutation{}, err
	}
	want := expected.content[right.array.start:right.array.end]
	if !bytes.Equal(permuted, want) {
		return JSONArrayPermutation{}, fmt.Errorf("%w: array permutation changes slot syntax", ErrInvalidValue)
	}
	return JSONArrayPermutation{
		syntax: before.syntax, path: path,
		before:   slices.Clone(before.content[left.array.start:left.array.end]),
		expected: slices.Clone(want),
	}, nil
}

func (permutation JSONArrayPermutation) selected(ctx context.Context, document Document) (jsonArraySource, bool, error) {
	if ctx == nil {
		return jsonArraySource{}, false, fmt.Errorf("array permutation comparison requires context")
	}
	if err := ctx.Err(); err != nil {
		return jsonArraySource{}, false, err
	}
	if permutation.syntax == 0 || permutation.path.IsRoot() || len(permutation.before) == 0 || len(permutation.expected) == 0 || permutation.syntax != document.syntax {
		return jsonArraySource{}, false, fmt.Errorf("%w: array permutation has no compatible captured state", ErrInvalidValue)
	}
	_, present, err := document.Lookup(permutation.path)
	if err != nil || !present {
		return jsonArraySource{}, false, err
	}
	source, err := document.jsonArraySource(ctx, permutation.path)
	return source, err == nil, err
}

func (permutation JSONArrayPermutation) Match(ctx context.Context, document Document) (before, expected bool, err error) {
	source, present, err := permutation.selected(ctx, document)
	if err != nil || !present {
		return false, false, err
	}
	current := document.content[source.array.start:source.array.end]
	return bytes.Equal(current, permutation.before), bytes.Equal(current, permutation.expected), nil
}

func (permutation JSONArrayPermutation) MatchesBefore(ctx context.Context, document Document) (bool, error) {
	before, _, err := permutation.Match(ctx, document)
	return before, err
}

func (permutation JSONArrayPermutation) MatchesExpected(ctx context.Context, document Document) (bool, error) {
	_, expected, err := permutation.Match(ctx, document)
	return expected, err
}

func (permutation JSONArrayPermutation) Restore(ctx context.Context, document Document) (Document, bool, error) {
	source, present, err := permutation.selected(ctx, document)
	if err != nil {
		return Document{}, false, err
	}
	if !present {
		return Document{}, false, fmt.Errorf("%w: captured array is absent", ErrPathConflict)
	}
	current := document.content[source.array.start:source.array.end]
	if bytes.Equal(current, permutation.before) {
		return document, false, nil
	}
	if !bytes.Equal(current, permutation.expected) {
		return Document{}, false, fmt.Errorf("%w: array differs from captured permutation states", ErrPathConflict)
	}
	content := make([]byte, 0, len(document.content))
	content = append(content, document.content[:source.array.start]...)
	content = append(content, permutation.before...)
	content = append(content, document.content[source.array.end:]...)
	restored, err := Parse(ctx, content, document.syntax, document.limits)
	if err != nil {
		return Document{}, false, err
	}
	return restored, true, nil
}
