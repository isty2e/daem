package configdocument

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/tailscale/hujson"

	"github.com/isty2e/daem/internal/encoding/jsonstrict"
)

type jsonSource struct {
	tree     hujson.Value
	comments []string
}

// StandardJSON uses the editor's JSON-family lexical rules to return strict
// JSON. It preserves the caller's source and grants no editing authority.
func StandardJSON(ctx context.Context, content []byte, syntax Syntax, limits Limits) ([]byte, error) {
	if ctx == nil {
		return nil, fmt.Errorf("configuration context is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if syntax != JSON && syntax != JSONC && syntax != JSONLineComments {
		return nil, fmt.Errorf("JSON-family syntax is required")
	}
	if err := limits.Validate(); err != nil {
		return nil, err
	}
	if int64(len(content)) > limits.MaximumBytes {
		return nil, ErrLimitExceeded
	}
	source, err := readJSONSource(ctx, content, syntax, limits)
	if err != nil {
		return nil, err
	}
	return source.standard(ctx, limits)
}

func readJSONSource(ctx context.Context, content []byte, syntax Syntax, limits Limits) (jsonSource, error) {
	if !utf8.Valid(content) {
		return jsonSource{}, ErrInvalidDocument
	}
	trivia, err := scanJSON(ctx, content, syntax, limits.MaximumDepth)
	if err != nil {
		return jsonSource{}, err
	}
	if syntax == JSON && !json.Valid(content) {
		return jsonSource{}, ErrInvalidDocument
	}
	tree, err := parseJSONTree(trivia.normalized)
	if err != nil {
		return jsonSource{}, err
	}
	return jsonSource{tree: tree, comments: trivia.comments}, nil
}

func (source jsonSource) standard(ctx context.Context, limits Limits) ([]byte, error) {
	standard := source.tree.Clone()
	standard.Standardize()
	content := standard.Pack()
	if err := jsonstrict.ValidateContext(ctx, content, "native configuration", limits.MaximumDepth); err != nil {
		return nil, err
	}
	return content, nil
}

func parseJSON(ctx context.Context, content []byte, syntax Syntax, limits Limits) (Value, []string, error) {
	source, err := readJSONSource(ctx, content, syntax, limits)
	if err != nil {
		return Value{}, nil, err
	}
	if _, err := source.standard(ctx, limits); err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return Value{}, nil, err
		}
		if errors.Is(err, jsonstrict.ErrMaximumDepthExceeded) {
			return Value{}, nil, ErrLimitExceeded
		}
		return Value{}, nil, ErrInvalidDocument
	}
	value, err := fromJSONTree(ctx, source.tree)
	return value, source.comments, err
}

func parseJSONTree(content []byte) (hujson.Value, error) {
	tree, err := hujson.Parse(content)
	if err != nil {
		return hujson.Value{}, ErrInvalidDocument
	}
	return tree, nil
}

// This bounded lexical pass protects the recursive CST parser; the shared
// strict validator still owns JSON string, Unicode and duplicate-key validity.
func scanJSON(ctx context.Context, content []byte, syntax Syntax, maximumDepth int) (jsonLexical, error) {
	trivia := jsonLexical{normalized: slices.Clone(content)}
	if len(content) == 0 || content[len(content)-1] != '\n' {
		trivia.normalized = append(trivia.normalized, '\n')
	}

	depth := 0
	for index := 0; index < len(content); {
		if err := ctx.Err(); err != nil {
			return jsonLexical{}, err
		}
		switch content[index] {
		case '"':
			index++
			for index < len(content) && content[index] != '"' {
				if index%4096 == 0 {
					if err := ctx.Err(); err != nil {
						return jsonLexical{}, err
					}
				}
				if content[index] == '\\' {
					index++
				}
				index++
			}
			index++
		case '{', '[':
			depth++
			if depth > maximumDepth {
				return jsonLexical{}, ErrLimitExceeded
			}
			index++
		case '}', ']':
			depth--
			if depth < 0 {
				return jsonLexical{}, ErrInvalidDocument
			}
			index++
		case '/':
			start := index
			if index+1 >= len(content) || syntax == JSON {
				return jsonLexical{}, ErrInvalidDocument
			}
			switch content[index+1] {
			case '/':
				index += 2
				for index < len(content) && content[index] != '\n' && content[index] != '\r' {
					if index%4096 == 0 {
						if err := ctx.Err(); err != nil {
							return jsonLexical{}, err
						}
					}
					index++
				}
				if index < len(content) && content[index] == '\r' && (index+1 == len(content) || content[index+1] != '\n') {
					trivia.normalized[index] = '\n'
				}
			case '*':
				if syntax != JSONC {
					return jsonLexical{}, ErrInvalidDocument
				}
				end := bytes.Index(content[index+2:], []byte("*/"))
				if end < 0 {
					return jsonLexical{}, ErrInvalidDocument
				}
				index += end + 4
			default:
				return jsonLexical{}, ErrInvalidDocument
			}
			trivia.comments = append(trivia.comments, string(content[start:index]))
			trivia.spans = append(trivia.spans, sourceSpan{start, index})
		default:
			index++
		}
	}
	return trivia, ctx.Err()
}

func fromJSONTree(ctx context.Context, tree hujson.Value) (Value, error) {
	if err := ctx.Err(); err != nil {
		return Value{}, err
	}
	switch node := tree.Value.(type) {
	case *hujson.Object:
		members := make([]objectMember, 0, len(node.Members))
		for _, member := range node.Members {
			var key string
			if err := json.Unmarshal(member.Name.Value.(hujson.Literal), &key); err != nil {
				return Value{}, ErrInvalidDocument
			}
			value, err := fromJSONTree(ctx, member.Value)
			if err != nil {
				return Value{}, err
			}
			members = append(members, objectMember{key: key, value: value})
		}
		return newObject(members)
	case *hujson.Array:
		values := make([]Value, 0, len(node.Elements))
		for _, element := range node.Elements {
			value, err := fromJSONTree(ctx, element)
			if err != nil {
				return Value{}, err
			}
			values = append(values, value)
		}
		return newArray(values)
	case hujson.Literal:
		switch node.Kind() {
		case 'n':
			return Value{data: nullNode{}}, nil
		case 't', 'f':
			return Value{data: boolNode(node.Kind() == 't')}, nil
		case '"':
			var text string
			if err := json.Unmarshal(node, &text); err != nil {
				return Value{}, ErrInvalidDocument
			}
			return newString(text)
		default:
			return newJSONNumber(string(node))
		}
	default:
		return Value{}, ErrInvalidDocument
	}
}

func editJSON(ctx context.Context, document Document, changes *changeTree) ([]byte, error) {
	content := document.content
	renderer := newLiteralRenderer(ctx, document.syntax, document.limits)
	if !document.exists {
		content = []byte("{}")
		if err := renderer.charge(2); err != nil {
			return nil, err
		}
	}
	if len(content) == 0 || content[len(content)-1] != '\n' {
		if err := renderer.charge(1); err != nil {
			return nil, err
		}
	}
	trivia, err := scanJSON(ctx, content, document.syntax, document.limits.MaximumDepth)
	if err != nil {
		return nil, err
	}
	tree, err := parseJSONTree(trivia.normalized)
	if err != nil {
		return nil, err
	}
	editor := jsonTreeEditor{renderer: &renderer, source: jsonLayoutSource{ctx, content, trivia.spans}}
	original := jsonTreeOriginal{value: document.root, present: document.exists}
	if err := editor.change(&tree, changes, original, defaultJSONObjectLayout(), 0); err != nil {
		return nil, err
	}
	result := tree.Pack()
	if int64(len(result)) > document.limits.MaximumBytes {
		return nil, ErrLimitExceeded
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

type jsonTreeEditor struct {
	renderer *literalRenderer
	source   jsonLayoutSource
}

type jsonTreeOriginal struct {
	value   Value
	present bool
}

func (editor jsonTreeEditor) change(tree *hujson.Value, changes *changeTree, original jsonTreeOriginal, inherited jsonObjectLayout, depth int) error {
	renderer := editor.renderer
	ctx := renderer.ctx
	if err := ctx.Err(); err != nil {
		return err
	}
	if changes.exact != nil {
		if original.present && original.value.Equal(changes.exact.value) {
			return nil
		}
		var source *hujson.Value
		if original.present {
			before := *tree
			source = &before
		}
		removed := (hujson.Value{Value: tree.Value}).Pack()
		trivia, err := scanJSON(ctx, removed, JSONC, renderer.maximumDepth)
		if err != nil {
			return err
		}
		for _, comment := range trivia.comments {
			if err := renderer.charge(int64(len(comment)) + 2); err != nil {
				return err
			}
			tree.BeforeExtra = append(tree.BeforeExtra, []byte("\n"+comment+"\n")...)
		}
		var literal strings.Builder
		if err := renderer.value(&literal, changes.exact.value, depth); err != nil {
			return err
		}
		replacement, err := parseJSONTree([]byte(literal.String()))
		if err != nil {
			return err
		}
		if err := editor.annotateObjects(&replacement, source, inherited, depth); err != nil {
			return err
		}
		tree.Value = replacement.Value
		return nil
	}
	object, ok := tree.Value.(*hujson.Object)
	if !ok {
		return ErrPathConflict
	}
	layout := inherited
	if original.present {
		var err error
		layout, err = editor.source.objectLayout(tree, inherited, depth == 0)
		if err != nil {
			return err
		}
	}
	generateClosing := !original.present || len(object.Members) == 0 && len(object.AfterExtra) == 0
	beforeMembers, _ := original.value.data.(objectNode)
	seen := make(map[string]struct{}, len(object.Members))
	retained := make([]hujson.ObjectMember, 0, len(object.Members))
	for _, member := range object.Members {
		if err := ctx.Err(); err != nil {
			return err
		}
		var key string
		if err := json.Unmarshal(member.Name.Value.(hujson.Literal), &key); err != nil {
			return ErrInvalidDocument
		}
		seen[key] = struct{}{}
		branch, selected := changes.children[key]
		if !selected {
			retained = append(retained, member)
			continue
		}
		if branch.exact != nil && branch.exact.remove {
			removed := append(member.Name.Pack(), member.Value.Pack()...)
			trivia, err := scanJSON(ctx, removed, JSONC, renderer.maximumDepth)
			if err != nil {
				return err
			}
			for _, comment := range trivia.comments {
				if err := renderer.charge(int64(len(comment)) + 2); err != nil {
					return err
				}
				object.AfterExtra = append(object.AfterExtra, []byte("\n"+comment+"\n")...)
			}
			continue
		}
		childLayout, err := editor.source.childLayout(layout, member.Name)
		if err != nil {
			return err
		}
		childBefore := jsonTreeOriginal{}
		if index, found := memberIndex(beforeMembers, key); original.present && found {
			childBefore = jsonTreeOriginal{value: beforeMembers[index].value, present: true}
		}
		if err := editor.change(&member.Value, branch, childBefore, childLayout, depth+1); err != nil {
			return err
		}
		retained = append(retained, member)
	}
	oldRetained := len(retained)
	keys := make([]string, 0, len(changes.children))
	for key := range changes.children {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	for _, key := range keys {
		branch := changes.children[key]
		if _, found := seen[key]; found || !branch.containsSet {
			continue
		}
		punctuation := int64(1) // Colon; layout bytes are charged separately.
		if len(retained) != 0 {
			punctuation++
		}
		if branch.exact == nil {
			punctuation += 2 // A newly created intermediate object.
		}
		if err := renderer.charge(punctuation); err != nil {
			return err
		}
		var name strings.Builder
		if err := renderer.key(&name, key); err != nil {
			return err
		}
		member := hujson.ObjectMember{
			Name:  hujson.Value{Value: hujson.Literal(name.String())},
			Value: hujson.Value{Value: &hujson.Object{}},
		}
		if err := layout.writeMember(&member, len(retained) == 0, renderer); err != nil {
			return err
		}
		if err := editor.change(&member.Value, branch, jsonTreeOriginal{}, layout.child(), depth+1); err != nil {
			return err
		}
		retained = append(retained, member)
	}
	object.Members = retained
	if len(object.Members) > 0 {
		if generateClosing {
			if err := layout.writeClosing(object, renderer); err != nil {
				return err
			}
		}
		if oldRetained == 0 && layout.trailing {
			if err := renderer.charge(1); err != nil {
				return err
			}
		}
		normalizeJSONObjectComma(object, layout.trailing)
	}
	return nil
}
