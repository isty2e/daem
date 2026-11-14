package tomlsyntax

import (
	"context"
	"slices"

	"github.com/pelletier/go-toml/v2/unstable"
)

// InlineTable retains authored keyed members and their value ranges. Inline
// tables inside arrays do not acquire object-key addresses.
type InlineTable struct {
	Path    Key
	Span    Span
	Entries []Expression
}

func (index Index) InlineTables() []InlineTable {
	result := slices.Clone(index.inlines)
	for table := range result {
		result[table].Path = slices.Clone(result[table].Path)
		result[table].Entries = slices.Clone(result[table].Entries)
		for entry := range result[table].Entries {
			result[table].Entries[entry].Path = slices.Clone(result[table].Entries[entry].Path)
		}
	}
	return result
}

func (index *Index) recordInlineTables(ctx context.Context, content []byte, node *unstable.Node, prefix Key, bias int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if node.Value().Kind != unstable.InlineTable {
		return nil
	}
	expression := keyValueExpression(content, node, prefix, bias)
	table := InlineTable{
		Path: expression.Path,
		Span: expression.Value,
	}
	for child := node.Value().Child(); child.Valid(); child = child.Next() {
		if child.Kind != unstable.KeyValue {
			continue
		}
		table.Entries = append(table.Entries, keyValueExpression(content, child, table.Path, bias))
		if err := index.recordInlineTables(ctx, content, child, table.Path, bias); err != nil {
			return err
		}
	}
	index.inlines = append(index.inlines, table)
	return ctx.Err()
}

type arrayTableScope struct {
	array    bool
	children map[string]*arrayTableScope
}

func (scope *arrayTableScope) add(path Key) {
	for _, key := range path {
		if scope.children == nil {
			scope.children = make(map[string]*arrayTableScope)
		}
		if scope.children[key] == nil {
			scope.children[key] = &arrayTableScope{}
		}
		scope = scope.children[key]
	}
	scope.array = true
}

func (scope *arrayTableScope) contains(path Key) bool {
	for _, key := range path {
		if scope.array {
			return true
		}
		scope = scope.children[key]
		if scope == nil {
			return false
		}
	}
	return scope.array
}
