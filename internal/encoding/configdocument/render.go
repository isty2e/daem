package configdocument

import (
	"context"
	"strconv"
	"strings"

	"github.com/tailscale/hujson"
)

// One renderer admits only newly emitted text. Retained syntax is checked by
// the editor's final document ceiling, not re-encoded for a size estimate.
type literalRenderer struct {
	ctx          context.Context
	syntax       Syntax
	maximumDepth int
	remaining    int64
}

func newLiteralRenderer(ctx context.Context, syntax Syntax, limits Limits) literalRenderer {
	return literalRenderer{ctx: ctx, syntax: syntax, maximumDepth: limits.MaximumDepth, remaining: limits.MaximumBytes}
}

func (renderer *literalRenderer) charge(amount int64) error {
	if err := renderer.ctx.Err(); err != nil {
		return err
	}
	if amount > renderer.remaining {
		return ErrLimitExceeded
	}
	renderer.remaining -= amount
	return nil
}

func (renderer *literalRenderer) raw(result *strings.Builder, text string) error {
	if err := renderer.charge(int64(len(text))); err != nil {
		return err
	}
	result.WriteString(text)
	return nil
}

func (renderer *literalRenderer) quoted(result *strings.Builder, text string) error {
	size, err := quotedSizeWithin(renderer.ctx, text, renderer.remaining)
	if err != nil {
		return err
	}
	if renderer.syntax == TOML {
		// JSON permits literal DEL; TOML requires its Unicode escape.
		count := int64(strings.Count(text, "\x7f"))
		extra := int64(len(tomlDELEscape) - 1)
		if count > (renderer.remaining-size)/extra {
			return ErrLimitExceeded
		}
		size += count * extra
	}
	if err := renderer.charge(size); err != nil {
		return err
	}
	encoded := string(hujson.String(text))
	if renderer.syntax == TOML {
		encoded = strings.ReplaceAll(encoded, "\x7f", tomlDELEscape)
	}
	result.WriteString(encoded)
	return nil
}

func (renderer *literalRenderer) key(result *strings.Builder, key string) error {
	if renderer.syntax == TOML && bareTOMLKey(key) {
		return renderer.raw(result, key)
	}
	return renderer.quoted(result, key)
}

func bareTOMLKey(key string) bool {
	if key == "" {
		return false
	}
	for index := 0; index < len(key); index++ {
		char := key[index]
		if (char >= 'A' && char <= 'Z') || (char >= 'a' && char <= 'z') ||
			(char >= '0' && char <= '9') || char == '_' || char == '-' {
			continue
		}
		return false
	}
	return true
}

const tomlDELEscape = `\u007f`

func (renderer *literalRenderer) value(result *strings.Builder, value Value, depth int) error {
	if err := renderer.ctx.Err(); err != nil {
		return err
	}
	separator, assignment := ",", ":"
	if renderer.syntax == TOML {
		separator, assignment = ", ", " = "
	}
	switch node := value.data.(type) {
	case stringNode:
		return renderer.quoted(result, string(node))
	case boolNode:
		return renderer.raw(result, strconv.FormatBool(bool(node)))
	case arrayNode:
		if depth >= renderer.maximumDepth {
			return ErrLimitExceeded
		}
		if err := renderer.raw(result, "["); err != nil {
			return err
		}
		for index, child := range node {
			if index != 0 {
				if err := renderer.raw(result, separator); err != nil {
					return err
				}
			}
			if err := renderer.value(result, child, depth+1); err != nil {
				return err
			}
		}
		return renderer.raw(result, "]")
	case objectNode:
		if depth >= renderer.maximumDepth {
			return ErrLimitExceeded
		}
		if err := renderer.raw(result, "{"); err != nil {
			return err
		}
		for index, member := range node {
			if index != 0 {
				if err := renderer.raw(result, separator); err != nil {
					return err
				}
			}
			if err := renderer.key(result, member.key); err != nil {
				return err
			}
			if err := renderer.raw(result, assignment); err != nil {
				return err
			}
			if err := renderer.value(result, member.value, depth+1); err != nil {
				return err
			}
		}
		return renderer.raw(result, "}")
	default:
		if renderer.syntax == TOML {
			if scalar, ok := renderTOMLScalar(value); ok {
				return renderer.raw(result, scalar)
			}
			return ErrInvalidValue
		}
		switch node := value.data.(type) {
		case nullNode:
			return renderer.raw(result, "null")
		case jsonNumberNode:
			return renderer.raw(result, string(node))
		default:
			return ErrInvalidValue
		}
	}
}
