package hook

import (
	"fmt"
	"sort"
	"strings"

	"github.com/isty2e/daem/internal/desired/hookasset"
)

const (
	assetPlaceholderPrefix = "{hook_"
	fileReferencePrefix    = "hook_file:"
	dirReferencePrefix     = "hook_dir:"
)

// AssetReference is one immutable Hook-owned reference to a declared HookAsset.
type AssetReference struct {
	id string
}

// ID returns the referenced HookAsset name.
func (reference AssetReference) ID() string { return reference.id }

// Placeholder returns the exact command token represented by the reference.
func (reference AssetReference) Placeholder() string {
	return "{" + fileReferencePrefix + reference.id + "}"
}

func newAssetReference(id string) (AssetReference, error) {
	if err := hookasset.ValidateName(id); err != nil {
		return AssetReference{}, err
	}
	return AssetReference{id: id}, nil
}

func parseAssetReferences(command string) ([]AssetReference, error) {
	references := make([]AssetReference, 0)
	for offset := 0; offset < len(command); {
		start := strings.Index(command[offset:], assetPlaceholderPrefix)
		if start < 0 {
			break
		}
		start += offset
		end := strings.Index(command[start:], "}")
		if end < 0 {
			return nil, fmt.Errorf("malformed hook asset placeholder at byte %d: missing closing brace", start)
		}
		end += start

		token := command[start+1 : end]
		reference, err := parseAssetReferenceToken(token)
		if err != nil {
			return nil, fmt.Errorf("malformed hook asset placeholder %q: %w", command[start:end+1], err)
		}
		references = append(references, reference)
		offset = end + 1
	}

	sort.Slice(references, func(left int, right int) bool {
		return references[left].id < references[right].id
	})
	return compactAssetReferences(references), nil
}

func parseAssetReferenceToken(token string) (AssetReference, error) {
	switch {
	case strings.HasPrefix(token, fileReferencePrefix):
		return newAssetReference(strings.TrimPrefix(token, fileReferencePrefix))
	case strings.HasPrefix(token, dirReferencePrefix):
		id := strings.TrimPrefix(token, dirReferencePrefix)
		if err := hookasset.ValidateName(id); err != nil {
			return AssetReference{}, err
		}
		return AssetReference{}, fmt.Errorf("hook_dir placeholders are unsupported")
	case strings.HasPrefix(token, "hook_file") || strings.HasPrefix(token, "hook_dir"):
		return AssetReference{}, fmt.Errorf("expected hook_file:<id> or hook_dir:<id>")
	default:
		return AssetReference{}, fmt.Errorf("unknown hook asset placeholder namespace")
	}
}

func compactAssetReferences(values []AssetReference) []AssetReference {
	if len(values) < 2 {
		return values
	}
	result := values[:1]
	for _, value := range values[1:] {
		if value != result[len(result)-1] {
			result = append(result, value)
		}
	}
	return result
}
