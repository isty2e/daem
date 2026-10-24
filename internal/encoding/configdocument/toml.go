package configdocument

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/pelletier/go-toml/v2"
	"github.com/pelletier/go-toml/v2/unstable"
	"github.com/tailscale/hujson"

	"github.com/isty2e/daem/internal/encoding/tomlstrict"
)

func parseTOML(ctx context.Context, content []byte, limits Limits) (Value, []string, error) {
	if uint64(len(content)) > math.MaxUint32 {
		return Value{}, nil, ErrLimitExceeded
	}
	structure := tomlstrict.StandardLimits()
	structure.MaximumDepth = limits.MaximumDepth
	if err := tomlstrict.Admit(ctx, content, structure); err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return Value{}, nil, err
		}
		if errors.Is(err, tomlstrict.ErrMalformed) {
			return Value{}, nil, ErrInvalidDocument
		}
		return Value{}, nil, ErrLimitExceeded
	}
	var decoded map[string]any
	if err := toml.Unmarshal(content, &decoded); err != nil {
		return Value{}, nil, ErrInvalidDocument
	}
	value, err := fromTOML(ctx, decoded, limits.MaximumDepth, 0)
	if err != nil {
		return Value{}, nil, err
	}
	index, err := indexTOML(ctx, content)
	if err != nil {
		return Value{}, nil, err
	}
	comments := make([]string, 0, len(index.comments))
	for _, span := range index.comments {
		comments = append(comments, string(content[span.start:span.end]))
	}
	return value, comments, nil
}

func fromTOML(ctx context.Context, decoded any, maximumDepth, depth int) (Value, error) {
	if err := ctx.Err(); err != nil {
		return Value{}, err
	}
	switch value := decoded.(type) {
	case map[string]any:
		if depth >= maximumDepth {
			return Value{}, ErrLimitExceeded
		}
		members := make([]objectMember, 0, len(value))
		for key, child := range value {
			converted, err := fromTOML(ctx, child, maximumDepth, depth+1)
			if err != nil {
				return Value{}, err
			}
			members = append(members, objectMember{key: key, value: converted})
		}
		return newObject(members)
	case []any:
		if depth >= maximumDepth {
			return Value{}, ErrLimitExceeded
		}
		values := make([]Value, 0, len(value))
		for _, child := range value {
			converted, err := fromTOML(ctx, child, maximumDepth, depth+1)
			if err != nil {
				return Value{}, err
			}
			values = append(values, converted)
		}
		return newArray(values)
	case string:
		return newString(value)
	case bool:
		return Value{data: boolNode(value)}, nil
	case int64:
		return Value{data: integerNode(value)}, nil
	case float64:
		return Value{data: floatNode(value)}, nil
	case time.Time:
		return newTemporal("offset-date-time", value.Format(time.RFC3339Nano))
	case toml.LocalDate:
		return newTemporal("local-date", value.String())
	case toml.LocalTime:
		return newTemporal("local-time", value.String())
	case toml.LocalDateTime:
		return newTemporal("local-date-time", value.String())
	default:
		return Value{}, ErrInvalidValue
	}
}

type sourceSpan struct{ start, end int }

type tomlExpression struct {
	root string
	span sourceSpan
}

type tomlIndex struct {
	expressions []tomlExpression
	comments    []sourceSpan
}

func indexTOML(ctx context.Context, content []byte) (tomlIndex, error) {
	var parser unstable.Parser
	parser.KeepComments = true
	parser.Reset(content)
	var index tomlIndex
	var table []string
	for parser.NextExpression() {
		if err := ctx.Err(); err != nil {
			return tomlIndex{}, err
		}
		node := parser.Expression()
		collectTOMLComments(node, &index.comments)
		switch node.Kind {
		case unstable.Table, unstable.ArrayTable:
			table = nil
			keys := node.Key()
			var first, last unstable.Range
			for keys.Next() {
				key := keys.Node()
				if len(table) == 0 {
					first = key.Raw
				}
				last = key.Raw
				table = append(table, string(key.Data))
			}
			start := bytes.LastIndexByte(content[:first.Offset], '\n') + 1
			for start < int(first.Offset) && (content[start] == ' ' || content[start] == '\t' || content[start] == '\r') {
				start++
			}
			end := int(last.Offset) + int(last.Length)
			closers := 1
			if node.Kind == unstable.ArrayTable {
				closers = 2
			}
			for end < len(content) && closers > 0 {
				if content[end] == ']' {
					closers--
				}
				end++
			}
			index.expressions = append(index.expressions, tomlExpression{root: table[0], span: sourceSpan{start, end}})
		case unstable.KeyValue:
			root := ""
			if len(table) > 0 {
				root = table[0]
			} else {
				keys := node.Key()
				keys.Next()
				root = string(keys.Node().Data)
			}
			index.expressions = append(index.expressions, tomlExpression{
				root: root, span: sourceSpan{int(node.Raw.Offset), int(node.Raw.Offset) + int(node.Raw.Length)},
			})
		}
	}
	if parser.Error() != nil {
		return tomlIndex{}, ErrInvalidDocument
	}
	slices.SortFunc(index.comments, func(left, right sourceSpan) int { return left.start - right.start })
	return index, ctx.Err()
}

func collectTOMLComments(node *unstable.Node, spans *[]sourceSpan) {
	for node.Valid() {
		if node.Kind == unstable.Comment {
			*spans = append(*spans, sourceSpan{int(node.Raw.Offset), int(node.Raw.Offset) + int(node.Raw.Length)})
		}
		collectTOMLComments(node.Child(), spans)
		node = node.Next()
	}
}

// Rebuild only touched top-level values. This avoids TOML's inline/dotted/table
// extension restrictions without changing unrelated native values. Disjoint
// array-table sections are removed individually; their intervening peers stay.
func editTOML(ctx context.Context, document Document, expected Value, changes *changeTree) ([]byte, error) {
	var selected []objectMember
	for _, member := range expected.data.(objectNode) {
		if _, found := changes.children[member.key]; found {
			selected = append(selected, member)
		}
	}
	if err := checkRenderBudget(ctx, TOML, Value{data: objectNode(selected)}, document.limits); err != nil {
		return nil, err
	}
	index, err := indexTOML(ctx, document.content)
	if err != nil {
		return nil, err
	}
	var result strings.Builder
	for _, member := range selected {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		result.Write(hujson.String(member.key))
		result.WriteString(" = ")
		if err := appendTOMLValue(&result, member.value); err != nil {
			return nil, err
		}
		result.WriteByte('\n')
	}
	position := 0
	commentIndex := 0
	for _, expression := range index.expressions {
		if _, selected := changes.children[expression.root]; !selected {
			continue
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		result.Write(document.content[position:expression.span.start])
		for commentIndex < len(index.comments) && index.comments[commentIndex].start < expression.span.end {
			comment := index.comments[commentIndex]
			if comment.start >= expression.span.start {
				result.WriteByte('\n')
				result.Write(document.content[comment.start:comment.end])
				result.WriteByte('\n')
			}
			commentIndex++
		}
		position = expression.span.end
		lineEnd := position
		for lineEnd < len(document.content) && (document.content[lineEnd] == ' ' || document.content[lineEnd] == '\t' || document.content[lineEnd] == '\r') {
			if lineEnd%4096 == 0 {
				if err := ctx.Err(); err != nil {
					return nil, err
				}
			}
			lineEnd++
		}
		if lineEnd < len(document.content) && document.content[lineEnd] == '\n' {
			position = lineEnd + 1
		}

		if int64(result.Len()) > document.limits.MaximumBytes {
			return nil, ErrLimitExceeded
		}
	}
	result.Write(document.content[position:])
	return []byte(result.String()), nil
}

func appendTOMLValue(result *strings.Builder, value Value) error {
	if scalar, ok := renderTOMLScalar(value); ok {
		result.WriteString(scalar)
		return nil
	}
	switch node := value.data.(type) {
	case arrayNode:
		result.WriteByte('[')
		for index, child := range node {
			if index > 0 {
				result.WriteString(", ")
			}
			if err := appendTOMLValue(result, child); err != nil {
				return err
			}
		}
		result.WriteByte(']')
	case objectNode:
		result.WriteByte('{')
		for index, member := range node {
			if index > 0 {
				result.WriteString(", ")
			}
			result.Write(hujson.String(member.key))
			result.WriteString(" = ")
			if err := appendTOMLValue(result, member.value); err != nil {
				return err
			}
		}
		result.WriteByte('}')
	default:
		return fmt.Errorf("%w: native type is not representable in TOML", ErrInvalidValue)
	}
	return nil
}

func renderTOMLScalar(value Value) (string, bool) {
	switch node := value.data.(type) {
	case boolNode:
		return strconv.FormatBool(bool(node)), true
	case stringNode:
		return string(hujson.String(string(node))), true
	case integerNode:
		return strconv.FormatInt(int64(node), 10), true
	case floatNode:
		text := canonicalFloat(float64(node))
		if !strings.ContainsAny(text, ".einfa") {
			text += ".0"
		}
		return text, true
	case temporalNode:
		return node.text, true
	default:
		return "", false
	}
}
