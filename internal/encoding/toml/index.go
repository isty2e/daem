package tomlsyntax

import (
	"bytes"
	"context"
	"fmt"
	"math"
	"slices"

	"github.com/pelletier/go-toml/v2/unstable"
)

// ExpressionKind distinguishes authored syntax, not domain schema or ownership.
type ExpressionKind uint8

const (
	Table ExpressionKind = iota + 1
	ArrayTable
	KeyValue
)

// Span identifies a half-open byte range in the indexed source.
type Span struct{ Start, End int }

// Expression contains decoded keys and exact authored source/value ranges.
type Expression struct {
	Path  Key
	Kind  ExpressionKind
	Span  Span
	Value Span
}

// ScopedComment associates a comment with its enclosing native key or table.
// Standalone comments belong to the current table; root comments have no path.
// Paths retain TOML key scope without addressing individual array elements.
type ScopedComment struct {
	Span Span
	Path Key
}

type keyOccurrence struct {
	path   Key
	offset int
	value  Span
}

// Index owns independent source facts. Returned slices do not alias its storage.
type Index struct {
	expressions []Expression
	scoped      []ScopedComment
	keys        []keyOccurrence
}

func (index Index) Expressions() []Expression {
	result := slices.Clone(index.expressions)
	for item := range result {
		result[item].Path = slices.Clone(result[item].Path)
	}
	return result
}

func (index Index) Comments() []Span {
	var comments []Span
	for _, comment := range index.scoped {
		comments = append(comments, comment.Span)
	}
	return comments
}

func (index Index) ScopedComments() []ScopedComment {
	comments := slices.Clone(index.scoped)
	for item := range comments {
		comments[item].Path = slices.Clone(comments[item].Path)
	}
	return comments
}

// IndexDocument reads lexical facts with the same parser used for typed decode.
// It is not a substitute for semantic syntax validation or resource admission.
func IndexDocument(ctx context.Context, content []byte) (Index, error) {
	if ctx == nil {
		return Index{}, fmt.Errorf("TOML indexing context is required")
	}
	if err := ctx.Err(); err != nil {
		return Index{}, err
	}
	if uint64(len(content)) > math.MaxUint32 {
		return Index{}, ErrMaximumWorkExceeded
	}
	input := bytes.TrimPrefix(content, []byte{0xef, 0xbb, 0xbf})
	bias := len(content) - len(input)
	var parser unstable.Parser
	parser.KeepComments = true
	parser.Reset(input)
	var index Index
	var table Key
	for parser.NextExpression() {
		if err := ctx.Err(); err != nil {
			return Index{}, err
		}
		node := parser.Expression()
		scope := table
		if node.Kind == unstable.Comment && len(index.expressions) != 0 {
			previous := index.expressions[len(index.expressions)-1]
			start := bias + int(node.Raw.Offset)
			if previous.Span.End <= start && !bytes.ContainsAny(content[previous.Span.End:start], "\r\n") {
				scope = previous.Path
			}
		}
		if err := collectScopedComments(ctx, content, node, scope, bias, &index.scoped); err != nil {
			return Index{}, err
		}
		switch node.Kind {
		case unstable.Table, unstable.ArrayTable:
			path, first, last := expressionKey(node, nil, bias)
			table = path
			kind, closers := Table, 1
			if node.Kind == unstable.ArrayTable {
				kind, closers = ArrayTable, 2
			}
			start := bytes.LastIndexByte(content[:first], '\n') + 1
			for start < first && (content[start] == ' ' || content[start] == '\t' || content[start] == '\r' || start < bias) {
				start++
			}
			end := last
			for end < len(content) && closers > 0 {
				if content[end] == ']' {
					closers--
				}
				end++
			}
			index.keys = append(index.keys, keyOccurrence{path: slices.Clone(path), offset: first})
			index.expressions = append(index.expressions, Expression{Path: path, Kind: kind, Span: Span{start, end}})
		case unstable.KeyValue:
			path, _, last := expressionKey(node, table, bias)
			start := bias + int(node.Raw.Offset)
			end := start + int(node.Raw.Length)
			separator := last + bytes.IndexByte(content[last:end], '=')
			valueStart := end - len(bytes.TrimLeft(content[separator+1:end], " \t"))
			index.expressions = append(index.expressions, Expression{Path: path, Kind: KeyValue, Span: Span{start, end}, Value: Span{valueStart, end}})
			if err := index.recordKeyValue(ctx, node, table, bias); err != nil {
				return Index{}, err
			}
		}
	}
	if err := parser.Error(); err != nil {
		return Index{}, err
	}
	slices.SortFunc(index.scoped, func(left, right ScopedComment) int { return left.Span.Start - right.Span.Start })
	if err := ctx.Err(); err != nil {
		return Index{}, err
	}
	return index, nil
}

func expressionKey(node *unstable.Node, prefix Key, bias int) (Key, int, int) {
	path := slices.Clone(prefix)
	keys := node.Key()
	first, last := -1, -1
	for keys.Next() {
		key := keys.Node()
		offset := bias + int(key.Raw.Offset)
		if first < 0 {
			first = offset
		}
		last = offset + int(key.Raw.Length)
		path = append(path, string(key.Data))
	}
	return path, first, last
}

func (index *Index) recordKeyValue(ctx context.Context, node *unstable.Node, prefix Key, bias int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	path, first, _ := expressionKey(node, prefix, bias)
	value := node.Value().Raw
	index.keys = append(index.keys, keyOccurrence{path: path, offset: first, value: Span{bias + int(value.Offset), bias + int(value.Offset) + int(value.Length)}})
	return index.recordChildren(ctx, node.Value(), path, bias)
}

func (index *Index) recordChildren(ctx context.Context, node *unstable.Node, path Key, bias int) error {
	if node.Kind != unstable.Array && node.Kind != unstable.InlineTable {
		return ctx.Err()
	}
	for child := node.Child(); child.Valid(); child = child.Next() {
		if child.Kind == unstable.KeyValue {
			if err := index.recordKeyValue(ctx, child, path, bias); err != nil {
				return err
			}
		} else {
			if err := index.recordChildren(ctx, child, path, bias); err != nil {
				return err
			}
		}
	}
	return ctx.Err()
}
