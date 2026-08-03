package skill

import (
	"fmt"
	"path/filepath"

	"github.com/isty2e/daem/internal/supply/source"
	"github.com/isty2e/daem/internal/target"
)

// SkillSetSpec is constructor input for one selector-backed SkillSet.
type SkillSetSpec struct {
	Source       source.Source
	Include      []Selector
	Exclude      []Selector
	Targets      []target.Target
	Placements   map[target.Target]TargetPlacement
	Scope        target.Scope
	InstallMode  InstallMode
	Portable     bool
	CompatRepair bool
}

// SkillSet is an identity-less generator of canonical Skills from supplied
// direct-child listing facts.
type SkillSet struct {
	source       source.Source
	include      []Selector
	exclude      []Selector
	targets      target.Set
	placements   map[target.Target]TargetPlacement
	scope        target.Scope
	installMode  InstallMode
	portable     bool
	compatRepair bool
}

// NewSkillSet constructs a selector-backed SkillSet without listing its source.
func NewSkillSet(spec SkillSetSpec) (SkillSet, error) {
	if len(spec.Include) == 0 {
		return SkillSet{}, fmt.Errorf("skill set include selectors are required")
	}
	if err := defaultExpansionLimits().validateDeclaration(spec.Include, spec.Exclude); err != nil {
		return SkillSet{}, err
	}
	include, err := normalizeSelectors(spec.Include, "include")
	if err != nil {
		return SkillSet{}, err
	}
	exclude, err := normalizeSelectors(spec.Exclude, "exclude")
	if err != nil {
		return SkillSet{}, err
	}
	if _, ok := spec.Source.S3(); ok {
		return SkillSet{}, fmt.Errorf("S3 skill sets are unsupported; S3 prefix directory sources are unsupported")
	}
	if _, err := source.SourceIDFor(spec.Source); err != nil {
		return SkillSet{}, fmt.Errorf("skill set source: %w", err)
	}
	targets, err := target.NewSet(spec.Targets)
	if err != nil {
		return SkillSet{}, fmt.Errorf("skill set targets: %w", err)
	}
	scope, err := target.ParseScope(string(spec.Scope))
	if err != nil {
		return SkillSet{}, fmt.Errorf("skill set: %w", err)
	}
	installMode, err := ParseInstallMode(string(spec.InstallMode))
	if err != nil {
		return SkillSet{}, fmt.Errorf("skill set: %w", err)
	}
	if local, ok := spec.Source.Local(); ok {
		if scope == target.ScopeGlobal && !filepath.IsAbs(local.Path()) {
			return SkillSet{}, fmt.Errorf("skill set source: global local filesystem sources must use an absolute path")
		}
		if scope == target.ScopeProject && local.Mode() == source.LocalSourceModeLink && spec.Portable {
			return SkillSet{}, fmt.Errorf("skill set source: project local link sources must set portable = false")
		}
	}
	placements, err := validateTargetPlacements("skill set", targets, scope, spec.Placements)
	if err != nil {
		return SkillSet{}, err
	}

	return SkillSet{
		source:       spec.Source,
		include:      include,
		exclude:      exclude,
		targets:      targets,
		placements:   placements,
		scope:        scope,
		installMode:  installMode,
		portable:     spec.Portable,
		compatRepair: spec.CompatRepair,
	}, nil
}

// Validate rejects a zero or invalid SkillSet value.
func (set SkillSet) Validate() error {
	_, err := NewSkillSet(SkillSetSpec{
		Source:       set.source,
		Include:      set.include,
		Exclude:      set.exclude,
		Targets:      set.targets.Values(),
		Placements:   set.placements,
		Scope:        set.scope,
		InstallMode:  set.installMode,
		Portable:     set.portable,
		CompatRepair: set.compatRepair,
	})
	return err
}

func normalizeSelectors(values []Selector, field string) ([]Selector, error) {
	normalized := make([]Selector, 0, len(values))
	for index, selector := range values {
		parsed, err := ParseSelector(selector.Expression())
		if err != nil || parsed != selector {
			return nil, fmt.Errorf("skill set %s[%d]: invalid selector", field, index)
		}
		normalized = append(normalized, parsed)
	}
	return normalized, nil
}

// Select returns deterministic selected child names from supplied listing facts.
func (set SkillSet) Select(childNames []string) ([]string, error) {
	if err := set.Validate(); err != nil {
		return nil, err
	}
	return selectNames(childNames, set.include, set.exclude, NewExpansionBudget())
}

// Selects reports whether one canonical direct child belongs to this set.
// Unlike Select, a non-matching child is a valid negative membership result.
func (set SkillSet) Selects(name string) (bool, error) {
	if err := set.Validate(); err != nil {
		return false, err
	}
	canonicalName, err := cleanName(name)
	if err != nil || canonicalName != name {
		return false, fmt.Errorf("skill set child name must be a canonical safe single path segment")
	}
	return selectorSetMatches(name, set.include, set.exclude, NewExpansionBudget())
}

// Child constructs one selected canonical Skill using a caller-supplied child
// source locator.
func (set SkillSet) Child(name string, childSource source.Source) (Skill, error) {
	selected, err := set.Selects(name)
	if err != nil {
		return Skill{}, err
	}
	if !selected {
		return Skill{}, fmt.Errorf("skill set child %s is not selected", skillDiagnosticValue(name))
	}
	return set.child(name, childSource)
}

func (set SkillSet) child(name string, childSource source.Source) (Skill, error) {
	if err := set.source.ValidateChild(name, childSource); err != nil {
		return Skill{}, fmt.Errorf("skill set child source: %w", err)
	}
	return New(Spec{
		Name:         name,
		InstallName:  name,
		Source:       childSource,
		Targets:      set.targets.Values(),
		Placements:   set.placements,
		Scope:        set.scope,
		InstallMode:  set.installMode,
		Portable:     set.portable,
		CompatRepair: set.compatRepair,
	})
}

// Source returns the unresolved source-root locator.
func (set SkillSet) Source() source.Source { return set.source }

// Include returns a defensive selector copy.
func (set SkillSet) Include() []Selector { return append([]Selector(nil), set.include...) }

// Exclude returns a defensive selector copy.
func (set SkillSet) Exclude() []Selector { return append([]Selector(nil), set.exclude...) }

// Targets returns a defensive copy in authored order.
func (set SkillSet) Targets() []target.Target { return set.targets.Values() }

// TargetPlacements returns a defensive copy of inherited target root requests.
func (set SkillSet) TargetPlacements() map[target.Target]TargetPlacement {
	return cloneTargetPlacements(set.placements)
}

// Scope returns the inherited child scope.
func (set SkillSet) Scope() target.Scope { return set.scope }

// InstallMode returns the inherited child install mode.
func (set SkillSet) InstallMode() InstallMode { return set.installMode }

// Portable returns the inherited portability policy.
func (set SkillSet) Portable() bool { return set.portable }

// CompatRepair returns the inherited repair policy.
func (set SkillSet) CompatRepair() bool { return set.compatRepair }
