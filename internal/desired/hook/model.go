package hook

import (
	"fmt"
	"maps"
	"slices"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/target"
)

// Type identifies an admitted hook behavior variant.
type Type string

const TypeCommand Type = "command"

// ParseType validates a hook type.
func ParseType(value string) (Type, error) {
	if Type(value) != TypeCommand {
		return "", fmt.Errorf("unknown hook type %q", value)
	}
	return TypeCommand, nil
}

// TargetOverride carries target-specific desired hook fields. Host-specific
// interpretation remains outside Desired.
type TargetOverride struct {
	condition string
	matcher   string
}

// NewTargetOverride constructs an immutable target override.
func NewTargetOverride(condition string, matcher string) TargetOverride {
	return TargetOverride{condition: condition, matcher: matcher}
}

// Condition returns the target-specific condition text.
func (override TargetOverride) Condition() string { return override.condition }

// Matcher returns the target-specific matcher text.
func (override TargetOverride) Matcher() string { return override.matcher }

// Spec is constructor input for one canonical Hook.
type Spec struct {
	Name            string
	Event           string
	Matcher         string
	Type            Type
	Command         string
	TimeoutSeconds  int
	StatusMessage   string
	Targets         []target.Target
	Scope           target.Scope
	TargetOverrides map[target.Target]TargetOverride
}

// Hook is one immutable canonical desired command hook.
type Hook struct {
	id              entity.ID
	event           string
	matcher         string
	hookType        Type
	command         string
	timeoutSeconds  int
	statusMessage   string
	targets         target.Set
	scope           target.Scope
	targetOverrides map[target.Target]TargetOverride
	assetReferences []AssetReference
}

// New constructs and validates a canonical Hook.
func New(spec Spec) (Hook, error) {
	name := strings.TrimSpace(spec.Name)
	if name == "" {
		return Hook{}, fmt.Errorf("hook name is required")
	}
	if strings.IndexFunc(name, isUnsafeControl) >= 0 {
		return Hook{}, fmt.Errorf("hook name must not contain control characters")
	}
	id, err := entity.New(entity.KindHook, name)
	if err != nil {
		return Hook{}, err
	}
	event := strings.TrimSpace(spec.Event)
	if event == "" {
		return Hook{}, fmt.Errorf("hook %q: event is required", name)
	}
	if !utf8.ValidString(event) {
		return Hook{}, fmt.Errorf("hook %q: event must be valid UTF-8", name)
	}
	if strings.IndexFunc(event, isUnsafeControl) >= 0 {
		return Hook{}, fmt.Errorf("hook %q: event must not contain control characters", name)
	}
	hookType, err := ParseType(string(spec.Type))
	if err != nil {
		return Hook{}, fmt.Errorf("hook %q: %w", name, err)
	}
	command := strings.TrimSpace(spec.Command)
	if command == "" {
		return Hook{}, fmt.Errorf("hook %q: command is required", name)
	}
	assetReferences, err := parseAssetReferences(command)
	if err != nil {
		return Hook{}, fmt.Errorf("hook %q command: %w", name, err)
	}
	for _, text := range []struct {
		label string
		value string
	}{
		{label: "matcher", value: spec.Matcher},
		{label: "command", value: command},
		{label: "status message", value: spec.StatusMessage},
	} {
		if !utf8.ValidString(text.value) {
			return Hook{}, fmt.Errorf("hook %q: %s must be valid UTF-8", name, text.label)
		}
		if strings.IndexFunc(text.value, isBidiControl) >= 0 {
			return Hook{}, fmt.Errorf("hook %q: %s must not contain bidirectional control characters", name, text.label)
		}
	}
	if spec.TimeoutSeconds < 0 {
		return Hook{}, fmt.Errorf("hook %q: timeout must not be negative", name)
	}
	targets, err := target.NewSet(spec.Targets)
	if err != nil {
		return Hook{}, fmt.Errorf("hook %q targets: %w", name, err)
	}
	scope, err := target.ParseScope(string(spec.Scope))
	if err != nil {
		return Hook{}, fmt.Errorf("hook %q: %w", name, err)
	}
	overrides, err := validateOverrides(name, targets, spec.TargetOverrides)
	if err != nil {
		return Hook{}, err
	}

	return Hook{
		id:              id,
		event:           event,
		matcher:         spec.Matcher,
		hookType:        hookType,
		command:         command,
		timeoutSeconds:  spec.TimeoutSeconds,
		statusMessage:   spec.StatusMessage,
		targets:         targets,
		scope:           scope,
		targetOverrides: overrides,
		assetReferences: assetReferences,
	}, nil
}

func validateOverrides(name string, targets target.Set, values map[target.Target]TargetOverride) (map[target.Target]TargetOverride, error) {
	overrides := make(map[target.Target]TargetOverride, len(values))
	keys := make([]target.Target, 0, len(values))
	for selected := range values {
		keys = append(keys, selected)
	}
	slices.Sort(keys)
	for _, selected := range keys {
		override := values[selected]
		parsed, err := target.ParseTarget(string(selected))
		if err != nil {
			return nil, fmt.Errorf("hook %q target override %q: %w", name, selected, err)
		}
		if !targets.Contains(parsed) {
			return nil, fmt.Errorf("hook %q target override %q is not declared for hook", name, selected)
		}
		if !utf8.ValidString(override.condition) || !utf8.ValidString(override.matcher) {
			return nil, fmt.Errorf("hook %q target override %q must contain valid UTF-8", name, selected)
		}
		if strings.IndexFunc(override.condition, isBidiControl) >= 0 || strings.IndexFunc(override.matcher, isBidiControl) >= 0 {
			return nil, fmt.Errorf("hook %q target override %q must not contain bidirectional control characters", name, selected)
		}
		overrides[parsed] = override
	}
	return overrides, nil
}

func isUnsafeControl(value rune) bool {
	return unicode.IsControl(value) || isBidiControl(value)
}

func isBidiControl(value rune) bool {
	return unicode.Is(unicode.Bidi_Control, value)
}

// Validate rejects a zero or invalid Hook value.
func (hook Hook) Validate() error {
	if hook.id.Kind() != entity.KindHook {
		return fmt.Errorf("hook has entity kind %q", hook.id.Kind())
	}
	canonical, err := New(Spec{
		Name:            hook.id.Name(),
		Event:           hook.event,
		Matcher:         hook.matcher,
		Type:            hook.hookType,
		Command:         hook.command,
		TimeoutSeconds:  hook.timeoutSeconds,
		StatusMessage:   hook.statusMessage,
		Targets:         hook.targets.Values(),
		Scope:           hook.scope,
		TargetOverrides: hook.targetOverrides,
	})
	if err != nil {
		return err
	}
	if !slices.Equal(hook.assetReferences, canonical.assetReferences) {
		return fmt.Errorf("hook %q asset references do not match command", hook.id.Name())
	}
	return nil
}

func (hook Hook) ID() entity.ID            { return hook.id }
func (hook Hook) Event() string            { return hook.event }
func (hook Hook) Matcher() string          { return hook.matcher }
func (hook Hook) Type() Type               { return hook.hookType }
func (hook Hook) Command() string          { return hook.command }
func (hook Hook) TimeoutSeconds() int      { return hook.timeoutSeconds }
func (hook Hook) StatusMessage() string    { return hook.statusMessage }
func (hook Hook) Targets() []target.Target { return hook.targets.Values() }
func (hook Hook) Scope() target.Scope      { return hook.scope }

// AssetReferences returns the canonical HookAsset references parsed from Command.
func (hook Hook) AssetReferences() []AssetReference {
	return append([]AssetReference(nil), hook.assetReferences...)
}

// RenderCommand substitutes resolved paths for exact Hook-owned asset references.
func (hook Hook) RenderCommand(replacements map[AssetReference]string) (string, error) {
	if err := hook.Validate(); err != nil {
		return "", err
	}
	if len(replacements) == 0 {
		return hook.command, nil
	}

	owned := make(map[AssetReference]struct{}, len(hook.assetReferences))
	for _, reference := range hook.assetReferences {
		owned[reference] = struct{}{}
	}
	references := make([]AssetReference, 0, len(replacements))
	for reference := range replacements {
		if _, ok := owned[reference]; !ok {
			return "", fmt.Errorf("hook %q does not own hook asset reference %q", hook.id.Name(), reference.id)
		}
		references = append(references, reference)
	}
	sort.Slice(references, func(left int, right int) bool {
		return references[left].id < references[right].id
	})

	pairs := make([]string, 0, 2*len(references))
	for _, reference := range references {
		pairs = append(pairs, reference.Placeholder(), replacements[reference])
	}
	return strings.NewReplacer(pairs...).Replace(hook.command), nil
}

// TargetOverrides returns a defensive copy.
func (hook Hook) TargetOverrides() map[target.Target]TargetOverride {
	result := make(map[target.Target]TargetOverride, len(hook.targetOverrides))
	maps.Copy(result, hook.targetOverrides)
	return result
}
