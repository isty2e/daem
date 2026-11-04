package tomlsyntax

import (
	"bytes"
	"context"
	"slices"

	"github.com/pelletier/go-toml/v2/unstable"
)

func collectScopedComments(ctx context.Context, content []byte, node *unstable.Node, prefix Key, bias int, comments *[]ScopedComment) error {
	previousScope, previousEnd := prefix, -1
	for node.Valid() {
		if err := ctx.Err(); err != nil {
			return err
		}
		if node.Kind == unstable.Comment {
			start := bias + int(node.Raw.Offset)
			scope := prefix
			if previousEnd >= 0 && !bytes.ContainsAny(content[min(previousEnd, start):start], "\r\n") {
				scope = previousScope
			}
			*comments = append(*comments, ScopedComment{Span: Span{start, start + int(node.Raw.Length)}, Path: slices.Clone(scope)})
		}
		child, scope := node.Child(), prefix
		end := bias + int(node.Raw.Offset) + int(node.Raw.Length)
		if node.Kind == unstable.KeyValue {
			scope, _, _ = expressionKey(node, prefix, bias)
			child = node.Value()
		} else if node.Kind == unstable.Table || node.Kind == unstable.ArrayTable {
			scope, _, end = expressionKey(node, nil, bias)
			prefix = scope
		}
		if node.Kind == unstable.KeyValue || node.Kind == unstable.Table || node.Kind == unstable.ArrayTable {
			previousScope = scope
			previousEnd = end
		}
		if err := collectScopedComments(ctx, content, child, scope, bias, comments); err != nil {
			return err
		}
		node = node.Next()
	}
	return ctx.Err()
}
