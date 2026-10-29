package configdocument

import (
	"bytes"
	"context"
	"slices"
	"strings"

	"github.com/pelletier/go-toml/v2/unstable"
)

// Each node owns its decoded path and lexical representation. Assignment values
// and array tables are coupled; dotted ancestors and standard tables remain
// independently editable containers, even when their sections are interleaved.
type tomlSyntaxNode struct {
	path        []string
	parent      *tomlSyntaxNode
	children    map[string]*tomlSyntaxNode
	expressions []*tomlExpression
	header      *tomlExpression
	assignment  *tomlExpression
	coupled     bool
	represented bool
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
	renderer literalRenderer
}

func patchTOML(ctx context.Context, document Document, expected Value, index tomlIndex) ([]byte, error) {
	root := &tomlSyntaxNode{represented: true}
	for i := range index.expressions {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		expression := &index.expressions[i]
		node := root
		for _, key := range expression.path {
			node = node.child(key)
			node.represented = true
		}
		node.expressions = append(node.expressions, expression)
		switch expression.kind {
		case unstable.Table:
			node.header = expression
		case unstable.ArrayTable:
			node.coupled = true
		case unstable.KeyValue:
			node.assignment = expression
		}
	}

	editor := tomlEditor{
		ctx: ctx, document: document, index: index, newline: "\n",
		renderer: newLiteralRenderer(ctx, TOML, document.limits),
	}
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
		if err := editor.renderer.value(&rendered, after, len(node.path)); err != nil {
			return err
		}
		expression := node.assignment
		comments, err := editor.commentsWithin(expression.value)
		if err != nil {
			return err
		}
		if comments == "" {
			editor.patches = append(editor.patches, tomlPatch{expression.value, rendered.String()})
		} else {
			prefix := editor.document.content[expression.span.start:expression.value.start]
			if err := editor.renderer.charge(int64(len(prefix))); err != nil {
				return err
			}
			text := comments + string(prefix) + rendered.String()
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
	scope := node.parent
	if scope.represented {
		for scope.parent != nil && scope.header == nil {
			scope = scope.parent
		}
	}
	editor.section(scope)
	for i, key := range node.path[len(scope.path):] {
		if i > 0 {
			if err := editor.renderer.raw(&scope.insert, "."); err != nil {
				return err
			}
		}
		if err := editor.renderer.key(&scope.insert, key); err != nil {
			return err
		}
	}
	if err := editor.renderer.raw(&scope.insert, " = "); err != nil {
		return err
	}
	if err := editor.renderer.value(&scope.insert, value, len(node.path)); err != nil {
		return err
	}
	return editor.renderer.raw(&scope.insert, editor.newline)
}

func (editor *tomlEditor) commentsWithin(span sourceSpan) (string, error) {
	var result strings.Builder
	start, _ := slices.BinarySearchFunc(editor.index.comments, span.start, func(comment sourceSpan, start int) int {
		return comment.start - start
	})
	for _, comment := range editor.index.comments[start:] {
		if comment.start >= span.end {
			break
		}
		if err := editor.renderer.charge(int64(comment.end-comment.start) + int64(len(editor.newline))); err != nil {
			return "", err
		}
		result.Write(editor.document.content[comment.start:comment.end])
		result.WriteString(editor.newline)
	}
	return result.String(), nil
}

func (editor *tomlEditor) remove(node *tomlSyntaxNode) error {
	if err := editor.ctx.Err(); err != nil {
		return err
	}
	for _, expression := range node.expressions {
		span := expression.span
		text, err := editor.commentsWithin(span)
		if err != nil {
			return err
		}
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
	var appendedSections []tomlPatch
	for _, node := range editor.pending {
		if err := editor.ctx.Err(); err != nil {
			return nil, err
		}
		position := 0
		text := node.insert.String()
		switch {
		case node.parent == nil:
			// Preserve leading trivia without placing root assignments inside a table.
			position = len(content)
			if len(editor.index.expressions) != 0 {
				position = bytes.LastIndexByte(content[:editor.index.expressions[0].span.start], '\n') + 1
			}
			if position > 0 && content[position-1] != '\n' {
				if err := editor.renderer.charge(int64(len(editor.newline))); err != nil {
					return nil, err
				}
				text = editor.newline + text
			}
		case node.header != nil:
			position = node.header.span.end
			if next := bytes.IndexByte(content[position:], '\n'); next >= 0 {
				position += next + 1
			} else {
				position = len(content)
				if err := editor.renderer.charge(int64(len(editor.newline))); err != nil {
					return nil, err
				}
				text = editor.newline + text
			}
		default:
			position = len(content)
			var section strings.Builder
			if position > 0 && content[position-1] != '\n' {
				if err := editor.renderer.raw(&section, editor.newline); err != nil {
					return nil, err
				}
			}
			if err := editor.renderer.raw(&section, "["); err != nil {
				return nil, err
			}
			for i, key := range node.path {
				if i > 0 {
					if err := editor.renderer.raw(&section, "."); err != nil {
						return nil, err
					}
				}
				if err := editor.renderer.key(&section, key); err != nil {
					return nil, err
				}
			}
			if err := editor.renderer.raw(&section, "]"+editor.newline); err != nil {
				return nil, err
			}
			// The inserted body was admitted while being produced, not again here.
			section.WriteString(text)
			text = section.String()
		}
		patch := tomlPatch{sourceSpan{position, position}, text}
		if node.parent != nil && node.header == nil {
			appendedSections = append(appendedSections, patch)
		} else {
			editor.patches = append(editor.patches, patch)
		}
	}
	// At a shared EOF offset, assignments in the root or an existing section
	// must precede new section headers that change the assignment's scope.
	editor.patches = append(editor.patches, appendedSections...)
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
