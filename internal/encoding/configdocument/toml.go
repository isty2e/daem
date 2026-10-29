package configdocument

import (
	"context"
	"errors"
	"math"
	"strconv"
	"strings"

	"github.com/isty2e/daem/internal/encoding/toml"
)

func parseTOML(ctx context.Context, content []byte, limits Limits) (Value, []string, error) {
	if uint64(len(content)) > math.MaxUint32 {
		return Value{}, nil, ErrLimitExceeded
	}
	structure, err := tomlsyntax.DocumentLimits(int64(len(content)), limits.MaximumDepth)
	if err != nil {
		return Value{}, nil, ErrLimitExceeded
	}
	if err := tomlsyntax.Admit(ctx, content, structure); err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return Value{}, nil, err
		}
		if errors.Is(err, tomlsyntax.ErrMalformed) {
			return Value{}, nil, ErrInvalidDocument
		}
		return Value{}, nil, ErrLimitExceeded
	}
	decoded, err := tomlsyntax.Native(ctx, content)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return Value{}, nil, err
		}
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
	case tomlsyntax.Temporal:
		return newTemporal(value.Kind, value.Text)
	default:
		return Value{}, ErrInvalidValue
	}
}

type sourceSpan struct{ start, end int }

type tomlExpression struct {
	path  []string
	kind  tomlsyntax.ExpressionKind
	span  sourceSpan
	value sourceSpan
}

type tomlIndex struct {
	expressions []tomlExpression
	comments    []sourceSpan
}

func indexTOML(ctx context.Context, content []byte) (tomlIndex, error) {
	parsed, err := tomlsyntax.IndexDocument(ctx, content)
	if err != nil {
		return tomlIndex{}, err
	}
	var index tomlIndex
	for _, expression := range parsed.Expressions() {
		index.expressions = append(index.expressions, tomlExpression{
			path: expression.Path, kind: expression.Kind,
			span: sourceSpan{expression.Span.Start, expression.Span.End}, value: sourceSpan{expression.Value.Start, expression.Value.End},
		})
	}
	for _, span := range parsed.Comments() {
		index.comments = append(index.comments, sourceSpan{span.Start, span.End})
	}
	return index, ctx.Err()
}

func editTOML(ctx context.Context, document Document, expected Value) ([]byte, error) {
	index, err := indexTOML(ctx, document.content)
	if err != nil {
		return nil, err
	}
	return patchTOML(ctx, document, expected, index)
}

func renderTOMLScalar(value Value) (string, bool) {
	switch node := value.data.(type) {
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
