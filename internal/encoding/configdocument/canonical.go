package configdocument

import (
	"context"
	"strconv"
	"strings"

	"github.com/tailscale/hujson"
)

// CanonicalContext bounds the private encoded bytes and semantic value depth.
// It never returns a partial encoding on refusal or cancellation.
func (value Value) CanonicalContext(ctx context.Context, limits Limits) (string, error) {
	if ctx == nil {
		return "", ErrInvalidValue
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if err := limits.Validate(); err != nil {
		return "", err
	}
	if value.data == nil {
		return "", ErrInvalidValue
	}
	encoder := canonicalEncoder{ctx: ctx, bounded: true, remaining: limits.MaximumBytes, maximumDepth: limits.MaximumDepth}
	encoder.raw("v1:")
	encoder.encode(value, 0)
	if encoder.err != nil {
		return "", encoder.err
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return encoder.buffer.String(), nil
}

type canonicalEncoder struct {
	ctx          context.Context
	buffer       strings.Builder
	bounded      bool
	remaining    int64
	maximumDepth int
	err          error
}

func (encoder *canonicalEncoder) admit(size int64) bool {
	if encoder.err != nil {
		return false
	}
	if encoder.err = encoder.ctx.Err(); encoder.err != nil {
		return false
	}
	if encoder.bounded {
		if size > encoder.remaining {
			encoder.err = ErrLimitExceeded
			return false
		}
		encoder.remaining -= size
	}
	return true
}

func (encoder *canonicalEncoder) raw(value string) {
	if encoder.admit(int64(len(value))) {
		encoder.buffer.WriteString(value)
	}
}

func (encoder *canonicalEncoder) quoted(value string) {
	if !encoder.admit(0) {
		return
	}
	if encoder.bounded {
		if _, err := quotedSizeWithin(encoder.ctx, value, encoder.remaining); err != nil {
			encoder.err = err
			return
		}
	}
	encoded := hujson.String(value)
	if encoder.admit(int64(len(encoded))) {
		encoder.buffer.Write(encoded)
	}
}

func (encoder *canonicalEncoder) encode(value Value, depth int) {
	if !encoder.admit(0) {
		return
	}
	if encoder.bounded && (depth > encoder.maximumDepth ||
		(value.IsObject() || value.IsArray()) && depth >= encoder.maximumDepth) {
		encoder.err = ErrLimitExceeded
		return
	}
	encoder.raw("[")
	switch node := value.data.(type) {
	case nullNode:
		encoder.raw(`"null"`)
	case boolNode:
		encoder.raw(`"bool",`)
		encoder.raw(strconv.FormatBool(bool(node)))
	case stringNode:
		encoder.raw(`"string",`)
		encoder.quoted(string(node))
	case jsonNumberNode:
		encoder.raw(`"json-number",`)
		encoder.quoted(string(node))
	case integerNode:
		encoder.raw(`"integer",`)
		encoder.quoted(strconv.FormatInt(int64(node), 10))
	case floatNode:
		encoder.raw(`"float",`)
		encoder.quoted(canonicalFloat(float64(node)))
	case temporalNode:
		encoder.quoted(node.kind)
		encoder.raw(",")
		encoder.quoted(node.text)
	case arrayNode:
		encoder.raw(`"array",[`)
		for index, child := range node {
			if encoder.err != nil {
				return
			}
			if index > 0 {
				encoder.raw(",")
			}
			encoder.encode(child, depth+1)
		}
		encoder.raw("]")
	case objectNode:
		encoder.raw(`"object",[`)
		for index, member := range node {
			if encoder.err != nil {
				return
			}
			if index > 0 {
				encoder.raw(",")
			}
			encoder.raw("[")
			encoder.quoted(member.key)
			encoder.raw(",")
			encoder.encode(member.value, depth+1)
			encoder.raw("]")
		}
		encoder.raw("]")
	default:
		encoder.err = ErrInvalidValue
	}
	encoder.raw("]")
}
