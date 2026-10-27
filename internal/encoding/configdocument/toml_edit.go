package configdocument

import (
	"bytes"
	"context"
	"slices"
	"strings"

	"github.com/pelletier/go-toml/v2/unstable"
	"github.com/tailscale/hujson"
)

// Each node owns its decoded path and lexical representation. Assignments
// (including dotted groups) and array tables are coupled; standard tables are
// independently editable containers, even when their sections are interleaved.
type tomlSyntaxNode struct {
	path        []string
	parent      *tomlSyntaxNode
	children    map[string]*tomlSyntaxNode
	expressions []*tomlExpression
	header      *tomlExpression
	assignment  *tomlExpression
	coupled     bool
	insert      strings.Builder
	pending     bool
}

func (node *tomlSyntaxNode) child(key string) *tomlSyntaxNode {
	if node.children == nil {
		node.children = make(map[string]*tomlSyntaxNode)
	}
	if node.children[key] == nil {
		node.children[key] = &tomlSyntaxNode{path: append(slices.Clone(node.path), key), parent: node}
	}
	return node.children[key]
}

type tomlPatch struct {
	sourceSpan
	text string
}

type tomlEditor struct {
	ctx      context.Context
	document Document
	index    tomlIndex
	patches  []tomlPatch
	pending  []*tomlSyntaxNode
	newline  string
}

func patchTOML(ctx context.Context, document Document, expected Value, index tomlIndex) ([]byte, error) {
	root := &tomlSyntaxNode{}
	for i := range index.expressions {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		expression := &index.expressions[i]
		node := root
		for depth, key := range expression.path {
			node = node.child(key)
			if expression.kind == unstable.KeyValue && depth == len(expression.table) {
				node.coupled = true
			}
		}
		node.expressions = append(node.expressions, expression)
		switch expression.kind {
		case unstable.Table:
			node.header = expression
		case unstable.ArrayTable:
			node.coupled = true
		case unstable.KeyValue:
			if len(expression.path) == len(expression.table)+1 {
				node.assignment = expression
			}
		}
	}

	editor := tomlEditor{ctx: ctx, document: document, index: index, newline: "\n"}
	if bytes.Contains(document.content, []byte("\r\n")) {
		editor.newline = "\r\n"
	}
	if err := editor.visit(root, document.root, expected); err != nil {
		return nil, err
	}
	return editor.apply()
}

func (editor *tomlEditor) visit(node *tomlSyntaxNode, before, after Value) error {
	if err := editor.ctx.Err(); err != nil {
		return err
	}
	if before.Equal(after) {
		return nil
	}
	if after.data == nil {
		return editor.remove(node)
	}
	if node.assignment != nil {
		var rendered strings.Builder
		if err := appendTOMLValue(&rendered, after); err != nil {
			return err
		}
		expression := node.assignment
		comments := editor.commentsWithin(expression.value)
		if comments == "" {
			editor.patches = append(editor.patches, tomlPatch{expression.value, rendered.String()})
		} else {
			text := comments + string(editor.document.content[expression.span.start:expression.value.start]) + rendered.String()
			editor.patches = append(editor.patches, tomlPatch{expression.span, text})
		}
		return nil
	}
	if node.coupled || !after.IsObject() {
		if err := editor.remove(node); err != nil {
			return err
		}
		return editor.insertValue(node, after)
	}

	beforeMembers, _ := before.data.(objectNode)
	afterMembers := after.data.(objectNode)
	if len(afterMembers) == 0 && node.header == nil {
		editor.section(node)
	}
	for _, member := range beforeMembers {
		index, found := memberIndex(afterMembers, member.key)
		var next Value
		if found {
			next = afterMembers[index].value
		}
		if err := editor.visit(node.child(member.key), member.value, next); err != nil {
			return err
		}
	}
	for _, member := range afterMembers {
		if _, found := memberIndex(beforeMembers, member.key); found {
			continue
		}
		if err := editor.visit(node.child(member.key), Value{}, member.value); err != nil {
			return err
		}
	}
	return nil
}

func (editor *tomlEditor) section(node *tomlSyntaxNode) {
	if !node.pending {
		editor.pending = append(editor.pending, node)
		node.pending = true
	}
}

func (editor *tomlEditor) insertValue(node *tomlSyntaxNode, value Value) error {
	parent := node.parent
	editor.section(parent)
	parent.insert.Write(hujson.String(node.path[len(node.path)-1]))
	parent.insert.WriteString(" = ")
	if err := appendTOMLValue(&parent.insert, value); err != nil {
		return err
	}
	parent.insert.WriteString(editor.newline)
	return nil
}

func (editor *tomlEditor) commentsWithin(span sourceSpan) string {
	var result strings.Builder
	start, _ := slices.BinarySearchFunc(editor.index.comments, span.start, func(comment sourceSpan, start int) int {
		return comment.start - start
	})
	for _, comment := range editor.index.comments[start:] {
		if comment.start >= span.end {
			break
		}
		result.Write(editor.document.content[comment.start:comment.end])
		result.WriteString(editor.newline)
	}
	return result.String()
}

func (editor *tomlEditor) remove(node *tomlSyntaxNode) error {
	if err := editor.ctx.Err(); err != nil {
		return err
	}
	for _, expression := range node.expressions {
		span := expression.span
		text := editor.commentsWithin(span)
		content := editor.document.content
		lineStart := bytes.LastIndexByte(content[:span.start], '\n') + 1
		if len(bytes.Trim(content[lineStart:span.start], " \t\r")) == 0 {
			span.start = lineStart
		}
		lineEnd := span.end
		for lineEnd < len(content) && (content[lineEnd] == ' ' || content[lineEnd] == '\t' || content[lineEnd] == '\r') {
			if lineEnd%4096 == 0 {
				if err := editor.ctx.Err(); err != nil {
					return err
				}
			}
			lineEnd++
		}
		if lineEnd < len(content) && content[lineEnd] == '\n' {
			span.end = lineEnd + 1
		}
		editor.patches = append(editor.patches, tomlPatch{span, text})
	}
	for _, child := range node.children {
		if err := editor.remove(child); err != nil {
			return err
		}
	}
	return nil
}

func (editor *tomlEditor) apply() ([]byte, error) {
	content := editor.document.content
	for _, node := range editor.pending {
		if err := editor.ctx.Err(); err != nil {
			return nil, err
		}
		position := 0
		text := node.insert.String()
		switch {
		case node.parent == nil:
			// Root assignments must precede every table header.
		case node.header != nil:
			position = node.header.span.end
			if next := bytes.IndexByte(content[position:], '\n'); next >= 0 {
				position += next + 1
			} else {
				position = len(content)
				text = editor.newline + text
			}
		default:
			position = len(content)
			var section strings.Builder
			if position > 0 && content[position-1] != '\n' {
				section.WriteString(editor.newline)
			}
			section.WriteByte('[')
			for i, key := range node.path {
				if i > 0 {
					section.WriteByte('.')
				}
				section.Write(hujson.String(key))
			}
			section.WriteByte(']')
			section.WriteString(editor.newline)
			section.WriteString(text)
			text = section.String()
		}
		editor.patches = append(editor.patches, tomlPatch{sourceSpan{position, position}, text})
	}
	slices.SortStableFunc(editor.patches, func(left, right tomlPatch) int {
		if left.start != right.start {
			return left.start - right.start
		}
		return left.end - right.end
	})
	var result strings.Builder
	position := 0
	for _, patch := range editor.patches {
		if err := editor.ctx.Err(); err != nil {
			return nil, err
		}
		if patch.start < position {
			return nil, ErrOverlappingChange
		}
		if int64(result.Len())+int64(patch.start-position)+int64(len(patch.text)) > editor.document.limits.MaximumBytes {
			return nil, ErrLimitExceeded
		}
		result.Write(content[position:patch.start])
		result.WriteString(patch.text)
		position = patch.end
	}
	if int64(result.Len())+int64(len(content)-position) > editor.document.limits.MaximumBytes {
		return nil, ErrLimitExceeded
	}
	result.Write(content[position:])
	return []byte(result.String()), editor.ctx.Err()
}
