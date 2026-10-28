package configdocument

import (
	"context"
	"strings"

	"github.com/tailscale/hujson"
)

// DisplayValue renders a bounded canonical value as a host-syntax literal.
// Callers own disclosure policy, redaction and terminal safety.
func DisplayValue(ctx context.Context, syntax Syntax, canonical string, limits Limits) (string, error) {
	value, err := DecodeValue(ctx, syntax, canonical, limits)
	if err != nil {
		return "", err
	}
	if syntax == TOML {
		var result strings.Builder
		if err := appendTOMLValue(&result, value); err != nil {
			return "", err
		}
		return result.String(), ctx.Err()
	}
	tree, err := toJSONTree(value)
	if err != nil {
		return "", err
	}
	result := hujson.Value{Value: tree}
	return string(result.Pack()), ctx.Err()
}
