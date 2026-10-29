package configdocument

import "context"

func quotedEscapeExtra(character rune) int64 {
	switch character {
	case '\b', '\f', '\n', '\r', '\t', '\\', '"':
		return 1
	default:
		if character < ' ' {
			return 5
		}
		return 0
	}
}

func quotedSizeWithin(ctx context.Context, value string, maximum int64) (int64, error) {
	if maximum < 2 || int64(len(value)) > maximum-2 {
		return 0, ErrLimitExceeded
	}
	size, checkAt := int64(len(value))+2, 0
	for offset, character := range value {
		if offset >= checkAt {
			if err := ctx.Err(); err != nil {
				return 0, err
			}
			checkAt = offset + 4096
		}
		extra := quotedEscapeExtra(character)
		if extra > maximum-size {
			return 0, ErrLimitExceeded
		}
		size += extra
	}
	return size, ctx.Err()
}
