package antigravityplugin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"unicode"

	"github.com/isty2e/daem/internal/encoding/jsonstrict"
)

const maximumInventoryDepth = 32

func decodeImportsContext(ctx context.Context, content []byte) ([]string, error) {
	if ctx == nil {
		return nil, fmt.Errorf("Antigravity CLI import manifest context is required")
	}
	if err := jsonstrict.ValidateContext(
		ctx,
		content,
		"Antigravity CLI import manifest",
		maximumInventoryDepth,
	); err != nil {
		return nil, err
	}
	var document map[string]json.RawMessage
	if err := json.Unmarshal(content, &document); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	rawImports, present := document["imports"]
	if !present {
		return nil, fmt.Errorf("imports field is required")
	}
	if bytes.Equal(bytes.TrimSpace(rawImports), []byte("null")) {
		return nil, nil
	}
	var entries []json.RawMessage
	if err := json.Unmarshal(rawImports, &entries); err != nil {
		return nil, fmt.Errorf("imports must be an array or null: %w", err)
	}
	imports := make([]string, 0, len(entries))
	for index, raw := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		var entry map[string]json.RawMessage
		if err := json.Unmarshal(raw, &entry); err != nil {
			return nil, fmt.Errorf("imports[%d] must be an object: %w", index, err)
		}
		rawName, present := entry["name"]
		if !present {
			return nil, fmt.Errorf("imports[%d].name is required", index)
		}
		var name string
		if err := json.Unmarshal(rawName, &name); err != nil {
			return nil, fmt.Errorf("imports[%d].name must be a string", index)
		}
		if err := validateImportName(name); err != nil {
			return nil, fmt.Errorf("imports[%d].name: %w", index, err)
		}
		imports = append(imports, name)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return imports, nil
}

func validateImportName(name string) error {
	if name == "" || strings.TrimSpace(name) != name {
		return fmt.Errorf("must be non-empty and trimmed")
	}
	if strings.IndexFunc(name, func(character rune) bool {
		return unicode.IsControl(character) || unicode.Is(unicode.Bidi_Control, character)
	}) >= 0 {
		return fmt.Errorf("must not contain control characters")
	}
	return nil
}

func validatePluginManifestContext(ctx context.Context, content []byte, plugin string) error {
	if ctx == nil {
		return fmt.Errorf("Antigravity CLI plugin manifest context is required")
	}
	if err := jsonstrict.ValidateContext(
		ctx,
		content,
		"Antigravity CLI plugin manifest",
		maximumInventoryDepth,
	); err != nil {
		return err
	}
	var document map[string]json.RawMessage
	if err := json.Unmarshal(content, &document); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	rawName, present := document["name"]
	if !present {
		return fmt.Errorf("name field is required")
	}
	var name string
	if err := json.Unmarshal(rawName, &name); err != nil {
		return fmt.Errorf("name must be a string")
	}
	if name != plugin {
		return fmt.Errorf("name %q does not match selected plugin %q", name, plugin)
	}
	return ctx.Err()
}
