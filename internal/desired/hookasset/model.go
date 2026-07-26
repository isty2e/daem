package hookasset

import (
	"fmt"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/supply/source"
	"github.com/isty2e/daem/internal/target"
)

// ArtifactKind identifies an admitted structural hook-asset form.
type ArtifactKind string

const ArtifactKindFile ArtifactKind = "file"

// ParseArtifactKind validates a hook-asset kind.
func ParseArtifactKind(value string) (ArtifactKind, error) {
	if ArtifactKind(value) != ArtifactKindFile {
		return "", fmt.Errorf("unsupported hook asset kind %q", value)
	}
	return ArtifactKindFile, nil
}

// Spec is constructor input for one canonical HookAsset.
type Spec struct {
	Name         string
	Source       source.Source
	ArtifactKind ArtifactKind
	Scope        target.Scope
	Executable   bool
}

// HookAsset is one immutable desired source-backed hook asset.
type HookAsset struct {
	id           entity.ID
	source       source.Source
	artifactKind ArtifactKind
	scope        target.Scope
	executable   bool
}

// New constructs and validates a canonical HookAsset.
func New(spec Spec) (HookAsset, error) {
	if err := ValidateName(spec.Name); err != nil {
		return HookAsset{}, err
	}
	id, err := entity.New(entity.KindHookAsset, spec.Name)
	if err != nil {
		return HookAsset{}, err
	}
	if _, err := source.SourceIDFor(spec.Source); err != nil {
		return HookAsset{}, fmt.Errorf("hook asset %q source: %w", spec.Name, err)
	}
	artifactKind, err := ParseArtifactKind(string(spec.ArtifactKind))
	if err != nil {
		return HookAsset{}, fmt.Errorf("hook asset %q: %w", spec.Name, err)
	}
	scope, err := target.ParseScope(string(spec.Scope))
	if err != nil {
		return HookAsset{}, fmt.Errorf("hook asset %q: %w", spec.Name, err)
	}
	if local, ok := spec.Source.Local(); ok && scope == target.ScopeGlobal && !filepath.IsAbs(local.Path()) {
		return HookAsset{}, fmt.Errorf("hook asset %q source: global local filesystem sources must use an absolute path", spec.Name)
	}

	return HookAsset{
		id:           id,
		source:       spec.Source,
		artifactKind: artifactKind,
		scope:        scope,
		executable:   spec.Executable,
	}, nil
}

// Validate rejects a zero or invalid HookAsset value.
func (asset HookAsset) Validate() error {
	if asset.id.Kind() != entity.KindHookAsset {
		return fmt.Errorf("hook asset has entity kind %q", asset.id.Kind())
	}
	_, err := New(Spec{
		Name:         asset.id.Name(),
		Source:       asset.source,
		ArtifactKind: asset.artifactKind,
		Scope:        asset.scope,
		Executable:   asset.executable,
	})
	return err
}

// ValidateName rejects HookAsset names that can be confused with paths or tokens.
func ValidateName(id string) error {
	if !utf8.ValidString(id) {
		return fmt.Errorf("hook asset id must be valid UTF-8")
	}
	if strings.TrimSpace(id) != id || id == "" {
		return fmt.Errorf("hook asset id must be a non-empty trimmed segment")
	}
	if id == "." || id == ".." || strings.ContainsAny(id, `/\\{}:`) {
		return fmt.Errorf("hook asset id %q must not be path-like or contain placeholder delimiters", id)
	}
	if strings.IndexFunc(id, isUnsafeControl) >= 0 {
		return fmt.Errorf("hook asset id %q must not contain control characters", id)
	}
	return nil
}

func isUnsafeControl(value rune) bool {
	return unicode.IsControl(value) || unicode.Is(unicode.Bidi_Control, value)
}

func (asset HookAsset) ID() entity.ID              { return asset.id }
func (asset HookAsset) Source() source.Source      { return asset.source }
func (asset HookAsset) ArtifactKind() ArtifactKind { return asset.artifactKind }
func (asset HookAsset) Scope() target.Scope        { return asset.scope }
func (asset HookAsset) Executable() bool           { return asset.executable }
