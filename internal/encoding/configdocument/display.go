package configdocument

import (
	"context"
	"strings"
)

// DisplayValue bounds both private input and the returned host-syntax literal.
// Callers own disclosure policy, redaction and terminal safety.
func DisplayValue(ctx context.Context, syntax Syntax, canonical string, limits Limits) (string, error) {
	value, err := DecodeValue(ctx, syntax, canonical, limits)
	if err != nil {
		return "", err
	}
	renderer := newLiteralRenderer(ctx, syntax, limits)
	var result strings.Builder
	if err := renderer.value(&result, value, 0); err != nil {
		return "", err
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return result.String(), nil
}
