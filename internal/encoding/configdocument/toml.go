package configdocument

import (
	"bytes"
	"context"
	"errors"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/pelletier/go-toml/v2"
	"github.com/pelletier/go-toml/v2/unstable"

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
	path  []string
	kind  unstable.Kind
	span  sourceSpan
	value sourceSpan
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
			index.expressions = append(index.expressions, tomlExpression{
				path: slices.Clone(table), kind: node.Kind, span: sourceSpan{start, end},
			})
		case unstable.KeyValue:
			path := slices.Clone(table)
			keys := node.Key()
			keyEnd := 0
			for keys.Next() {
				key := keys.Node()
				path = append(path, string(key.Data))
				keyEnd = int(key.Raw.Offset) + int(key.Raw.Length)
			}
			end := int(node.Raw.Offset) + int(node.Raw.Length)
			// Array nodes have no Raw span. Locate the value after the final
			// parsed key so quoted keys containing '=' cannot mask the separator.
			separator := keyEnd + bytes.IndexByte(content[keyEnd:end], '=')
			start := end - len(bytes.TrimLeft(content[separator+1:end], " \t"))
			index.expressions = append(index.expressions, tomlExpression{
				path: path, kind: node.Kind,
				span: sourceSpan{int(node.Raw.Offset), end}, value: sourceSpan{start, end},
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
