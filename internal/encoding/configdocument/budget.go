package configdocument

import "context"

// Admit the semantic result before allocating a new syntax tree. Existing
// trivia adds at most input-proportional work.
func checkRenderBudget(ctx context.Context, syntax Syntax, value Value, limits Limits) error {
	remaining := limits.MaximumBytes
	var visit func(Value, int) error
	charge := func(amount int64) error {
		if amount > remaining {
			return ErrLimitExceeded
		}
		remaining -= amount
		return nil
	}
	visit = func(value Value, depth int) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		switch node := value.data.(type) {
		case objectNode:
			if depth >= limits.MaximumDepth {
				return ErrLimitExceeded
			}
			rootTable := syntax == TOML && depth == 0
			if !rootTable {
				if err := charge(2); err != nil {
					return err
				}
			}
			for index, member := range node {
				punctuation := int64(1)
				if syntax == TOML {
					punctuation = 3
				}
				if rootTable {
					punctuation++
				} else if index > 0 {
					punctuation++
					if syntax == TOML {
						punctuation++
					}
				}
				if err := charge(quotedSize(member.key) + punctuation); err != nil {
					return err
				}
				if err := visit(member.value, depth+1); err != nil {
					return err
				}
			}
		case arrayNode:
			if depth >= limits.MaximumDepth {
				return ErrLimitExceeded
			}
			if err := charge(2); err != nil {
				return err
			}
			for index, child := range node {
				if index > 0 {
					separator := int64(1)
					if syntax == TOML {
						separator++
					}
					if err := charge(separator); err != nil {
						return err
					}
				}
				if err := visit(child, depth+1); err != nil {
					return err
				}
			}
		case stringNode:
			return charge(quotedSize(string(node)))
		default:
			if syntax == TOML {
				scalar, ok := renderTOMLScalar(value)
				if !ok {
					return ErrInvalidValue
				}
				return charge(int64(len(scalar)))
			}
			switch node := value.data.(type) {
			case nullNode:
				return charge(4)
			case boolNode:
				if bool(node) {
					return charge(4)
				}
				return charge(5)
			case jsonNumberNode:
				return charge(int64(len(node)))
			default:
				return ErrInvalidValue
			}
		}
		return nil
	}
	return visit(value, 0)
}

func quotedSize(value string) int64 {
	size := int64(len(value)) + 2
	for _, character := range value {
		switch character {
		case '\b', '\f', '\n', '\r', '\t', '\\', '"':
			size++
		default:
			if character < ' ' {
				size += 5
			}
		}
	}
	return size
}
