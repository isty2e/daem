package configdocument

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"

	"github.com/tailscale/hujson"

	"github.com/isty2e/daem/internal/encoding/jsonstrict"
)

func parseJSON(ctx context.Context, content []byte, syntax Syntax, limits Limits) (Value, []string, error) {
	trivia, err := scanJSON(ctx, content, syntax, limits.MaximumDepth)
	if err != nil {
		return Value{}, nil, err
	}
	if syntax == JSON && !json.Valid(content) {
		return Value{}, nil, ErrInvalidDocument
	}
	tree, err := parseJSONTree(trivia.normalized)
	if err != nil {
		return Value{}, nil, err
	}
	standard := tree.Clone()
	standard.Standardize()
	if err := jsonstrict.ValidateContext(ctx, standard.Pack(), "native configuration", limits.MaximumDepth); err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return Value{}, nil, err
		}
		if errors.Is(err, jsonstrict.ErrMaximumDepthExceeded) {
			return Value{}, nil, ErrLimitExceeded
		}
		return Value{}, nil, ErrInvalidDocument
	}
	value, err := fromJSONTree(ctx, tree)
	return value, trivia.comments, err
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
				if index < len(content) && content[index] == '\r' {
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
	if err := changeJSONTree(&tree, changes, &renderer, 0); err != nil {
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

func changeJSONTree(tree *hujson.Value, changes *changeTree, renderer *literalRenderer, depth int) error {
	ctx := renderer.ctx
	if err := ctx.Err(); err != nil {
		return err
	}
	if changes.exact != nil {
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
		tree.Value = replacement.Value
		return nil
	}
	object, ok := tree.Value.(*hujson.Object)
	if !ok {
		return ErrPathConflict
	}
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
		if err := changeJSONTree(&member.Value, branch, renderer, depth+1); err != nil {
			return err
		}
		retained = append(retained, member)
	}
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
		punctuation := int64(3) // Newline, colon and value-leading space.
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
			Name:  hujson.Value{BeforeExtra: hujson.Extra("\n"), Value: hujson.Literal(name.String())},
			Value: hujson.Value{BeforeExtra: hujson.Extra(" "), Value: &hujson.Object{}},
		}
		if err := changeJSONTree(&member.Value, branch, renderer, depth+1); err != nil {
			return err
		}
		retained = append(retained, member)
	}
	object.Members = retained
	if renderer.syntax == JSON && len(object.Members) > 0 {
		last := &object.Members[len(object.Members)-1].Value
		object.AfterExtra = append(last.AfterExtra, object.AfterExtra...)
		last.AfterExtra = nil
	}
	return nil
}
