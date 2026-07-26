package instructions

import (
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/supply/source"
	"github.com/isty2e/daem/internal/target"
)

// RenderMode identifies how instruction content should be placed for a target.
type RenderMode string

const (
	RenderModeCopy    RenderMode = "copy"
	RenderModeSymlink RenderMode = "symlink"
)

// ParseRenderMode validates a canonical instruction rendering mode.
func ParseRenderMode(value string) (RenderMode, error) {
	switch RenderMode(value) {
	case RenderModeCopy, RenderModeSymlink:
		return RenderMode(value), nil
	default:
		return "", fmt.Errorf("unknown render mode %q", value)
	}
}

// Rendering is an immutable target-specific instruction rendering request.
// RenderTo remains opaque until Realization interprets it for a host profile.
type Rendering struct {
	renderTo string
	mode     RenderMode
}

// NewRendering constructs a canonical rendering request.
func NewRendering(renderTo string, mode RenderMode) (Rendering, error) {
	if !utf8.ValidString(renderTo) {
		return Rendering{}, fmt.Errorf("render path must be valid UTF-8")
	}
	if strings.IndexFunc(renderTo, isBidiControl) >= 0 {
		return Rendering{}, fmt.Errorf("render path must not contain bidirectional control characters")
	}
	parsedMode, err := ParseRenderMode(string(mode))
	if err != nil {
		return Rendering{}, err
	}
	return Rendering{renderTo: strings.TrimSpace(renderTo), mode: parsedMode}, nil
}

// RenderTo returns the opaque requested host-relative rendering location.
func (rendering Rendering) RenderTo() string { return rendering.renderTo }

// Mode returns the requested rendering mode.
func (rendering Rendering) Mode() RenderMode { return rendering.mode }

// Spec is constructor input for one canonical Instructions value.
type Spec struct {
	Name       string
	Source     source.Source
	Targets    []target.Target
	Scope      target.Scope
	Renderings map[target.Target]Rendering
}

// Instructions is one immutable canonical desired instruction source.
type Instructions struct {
	id         entity.ID
	source     source.Source
	targets    target.Set
	scope      target.Scope
	renderings map[target.Target]Rendering
}

// New constructs and validates canonical desired instructions.
func New(spec Spec) (Instructions, error) {
	if strings.TrimSpace(spec.Name) == "" {
		return Instructions{}, fmt.Errorf("instructions name is required")
	}
	if strings.IndexFunc(spec.Name, isUnsafeControl) >= 0 {
		return Instructions{}, fmt.Errorf("instructions name must not contain control characters")
	}
	id, err := entity.New(entity.KindInstructions, spec.Name)
	if err != nil {
		return Instructions{}, err
	}
	if err := validateSource(spec.Source, spec.Scope); err != nil {
		return Instructions{}, fmt.Errorf("instructions %q source: %w", spec.Name, err)
	}
	targets, err := target.NewSet(spec.Targets)
	if err != nil {
		return Instructions{}, fmt.Errorf("instructions %q targets: %w", spec.Name, err)
	}
	scope, err := target.ParseScope(string(spec.Scope))
	if err != nil {
		return Instructions{}, fmt.Errorf("instructions %q: %w", spec.Name, err)
	}
	renderings, err := validateRenderings(spec.Name, targets, spec.Renderings)
	if err != nil {
		return Instructions{}, err
	}

	return Instructions{
		id:         id,
		source:     spec.Source,
		targets:    targets,
		scope:      scope,
		renderings: renderings,
	}, nil
}

func isUnsafeControl(value rune) bool {
	return unicode.IsControl(value) || isBidiControl(value)
}

func isBidiControl(value rune) bool {
	return unicode.Is(unicode.Bidi_Control, value)
}

func validateSource(value source.Source, scope target.Scope) error {
	if _, err := source.SourceIDFor(value); err != nil {
		return err
	}
	if _, ok := value.Git(); ok {
		return fmt.Errorf("git instruction sources are not supported")
	}
	if local, ok := value.Local(); ok {
		if local.Mode() != source.LocalSourceModeVendor {
			return fmt.Errorf("instruction local sources must use vendor mode")
		}
		if scope == target.ScopeGlobal && !filepath.IsAbs(local.Path()) {
			return fmt.Errorf("global local filesystem sources must use an absolute path")
		}
	}
	if s3, ok := value.S3(); ok && s3.Format() != source.S3ObjectFormatFile {
		return fmt.Errorf("instruction S3 sources must use file format")
	}
	return nil
}

func validateRenderings(name string, targets target.Set, values map[target.Target]Rendering) (map[target.Target]Rendering, error) {
	renderings := make(map[target.Target]Rendering, len(values))
	keys := make([]target.Target, 0, len(values))
	for selected := range values {
		keys = append(keys, selected)
	}
	slices.Sort(keys)
	for _, selected := range keys {
		rendering := values[selected]
		parsedTarget, err := target.ParseTarget(string(selected))
		if err != nil {
			return nil, fmt.Errorf("instructions %q target %q: %w", name, selected, err)
		}
		if !targets.Contains(parsedTarget) {
			return nil, fmt.Errorf("instructions %q target %q is not declared for instructions", name, selected)
		}
		canonical, err := NewRendering(rendering.renderTo, rendering.mode)
		if err != nil {
			return nil, fmt.Errorf("instructions %q target %q: %w", name, selected, err)
		}
		renderings[parsedTarget] = canonical
	}
	return renderings, nil
}

// Validate rejects a zero or invalid Instructions value.
func (instructions Instructions) Validate() error {
	if instructions.id.Kind() != entity.KindInstructions {
		return fmt.Errorf("instructions has entity kind %q", instructions.id.Kind())
	}
	_, err := New(Spec{
		Name:       instructions.id.Name(),
		Source:     instructions.source,
		Targets:    instructions.targets.Values(),
		Scope:      instructions.scope,
		Renderings: instructions.renderings,
	})
	return err
}

// ID returns the authored desired identity.
func (instructions Instructions) ID() entity.ID { return instructions.id }

// Source returns the unresolved instruction source.
func (instructions Instructions) Source() source.Source { return instructions.source }

// Targets returns a defensive copy in authored order.
func (instructions Instructions) Targets() []target.Target { return instructions.targets.Values() }

// Scope returns the requested placement scope.
func (instructions Instructions) Scope() target.Scope { return instructions.scope }

// Renderings returns a defensive copy keyed by declared target.
func (instructions Instructions) Renderings() map[target.Target]Rendering {
	result := make(map[target.Target]Rendering, len(instructions.renderings))
	maps.Copy(result, instructions.renderings)
	return result
}
