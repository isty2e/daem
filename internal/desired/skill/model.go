package skill

import (
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"strings"

	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/supply/source"
	"github.com/isty2e/daem/internal/target"
)

// InstallMode controls how resolved skill content is placed for a target.
type InstallMode string

const (
	InstallModeCopy     InstallMode = "copy"
	InstallModeSymlink  InstallMode = "symlink"
	InstallModeHardlink InstallMode = "hardlink"
)

// ParseInstallMode validates a skill install mode.
func ParseInstallMode(value string) (InstallMode, error) {
	switch InstallMode(value) {
	case InstallModeCopy, InstallModeSymlink, InstallModeHardlink:
		return InstallMode(value), nil
	default:
		return "", fmt.Errorf("unknown install mode %q", value)
	}
}

// Spec is constructor input for one canonical Skill.
type Spec struct {
	Name         string
	InstallName  string
	Source       source.Source
	Targets      []target.Target
	Placements   map[target.Target]TargetPlacement
	Scope        target.Scope
	InstallMode  InstallMode
	Portable     bool
	CompatRepair bool
}

// Skill is one immutable canonical desired skill.
type Skill struct {
	id           entity.ID
	installName  string
	source       source.Source
	targets      target.Set
	placements   map[target.Target]TargetPlacement
	scope        target.Scope
	installMode  InstallMode
	portable     bool
	compatRepair bool
}

// New constructs and validates a canonical desired Skill.
func New(spec Spec) (Skill, error) {
	name, err := cleanName(spec.Name)
	if err != nil {
		return Skill{}, fmt.Errorf("skill name: %w", err)
	}
	installName := spec.InstallName
	if strings.TrimSpace(installName) == "" {
		installName = name
	}
	installName, err = cleanName(installName)
	if err != nil {
		return Skill{}, fmt.Errorf("skill %q install name: %w", name, err)
	}
	id, err := entity.New(entity.KindSkill, name)
	if err != nil {
		return Skill{}, err
	}
	targets, err := target.NewSet(spec.Targets)
	if err != nil {
		return Skill{}, fmt.Errorf("skill %q targets: %w", name, err)
	}
	scope, err := target.ParseScope(string(spec.Scope))
	if err != nil {
		return Skill{}, fmt.Errorf("skill %q: %w", name, err)
	}
	installMode, err := ParseInstallMode(string(spec.InstallMode))
	if err != nil {
		return Skill{}, fmt.Errorf("skill %q: %w", name, err)
	}
	if err := validateSource(spec.Source, scope, spec.Portable); err != nil {
		return Skill{}, fmt.Errorf("skill %q source: %w", name, err)
	}
	placements, err := validateTargetPlacements(fmt.Sprintf("skill %q", name), targets, scope, spec.Placements)
	if err != nil {
		return Skill{}, err
	}

	return Skill{
		id:           id,
		installName:  installName,
		source:       spec.Source,
		targets:      targets,
		placements:   placements,
		scope:        scope,
		installMode:  installMode,
		portable:     spec.Portable,
		compatRepair: spec.CompatRepair,
	}, nil
}

// Validate rejects a zero or otherwise invalid Skill value.
func (skill Skill) Validate() error {
	if skill.id.Kind() != entity.KindSkill {
		return fmt.Errorf("skill has entity kind %q", skill.id.Kind())
	}
	name, err := cleanName(skill.id.Name())
	if err != nil || name != skill.id.Name() {
		return fmt.Errorf("skill identity is invalid")
	}
	if installName, err := cleanName(skill.installName); err != nil || installName != skill.installName {
		return fmt.Errorf("skill %q install name is invalid", skill.id.Name())
	}
	if skill.targets.Len() == 0 {
		return fmt.Errorf("skill %q targets: at least one target is required", skill.id.Name())
	}
	if _, err := target.ParseScope(string(skill.scope)); err != nil {
		return fmt.Errorf("skill %q: %w", skill.id.Name(), err)
	}
	if _, err := ParseInstallMode(string(skill.installMode)); err != nil {
		return fmt.Errorf("skill %q: %w", skill.id.Name(), err)
	}
	if err := validateSource(skill.source, skill.scope, skill.portable); err != nil {
		return fmt.Errorf("skill %q source: %w", skill.id.Name(), err)
	}
	if _, err := validateTargetPlacements(
		fmt.Sprintf("skill %q", skill.id.Name()),
		skill.targets,
		skill.scope,
		skill.placements,
	); err != nil {
		return err
	}
	return nil
}

func validateSource(value source.Source, scope target.Scope, portable bool) error {
	if _, err := source.SourceIDFor(value); err != nil {
		return err
	}
	if local, ok := value.Local(); ok {
		if scope == target.ScopeGlobal && !filepath.IsAbs(local.Path()) {
			return fmt.Errorf("global local filesystem sources must use an absolute path")
		}
		if scope == target.ScopeProject && local.Mode() == source.LocalSourceModeLink && portable {
			return fmt.Errorf("project local link sources must set portable = false")
		}
	}
	if s3, ok := value.S3(); ok && s3.Format() == source.S3ObjectFormatFile {
		return fmt.Errorf("S3 skill sources must use archive format tar or tar.gz")
	}
	return nil
}

// ID returns the authored desired identity.
func (skill Skill) ID() entity.ID { return skill.id }

// InstallName returns the target-visible skill directory name.
func (skill Skill) InstallName() string { return skill.installName }

// Source returns the unresolved source locator.
func (skill Skill) Source() source.Source { return skill.source }

// Targets returns a defensive copy in authored order.
func (skill Skill) Targets() []target.Target { return skill.targets.Values() }

// TargetPlacements returns a defensive copy of explicit target root requests.
func (skill Skill) TargetPlacements() map[target.Target]TargetPlacement {
	return cloneTargetPlacements(skill.placements)
}

// Scope returns the requested placement scope.
func (skill Skill) Scope() target.Scope { return skill.scope }

// InstallMode returns the requested placement mode.
func (skill Skill) InstallMode() InstallMode { return skill.installMode }

// Portable reports whether the declaration is expected to work across machines.
func (skill Skill) Portable() bool { return skill.portable }

// CompatRepair reports whether deterministic compatibility repair is desired.
func (skill Skill) CompatRepair() bool { return skill.compatRepair }

// Equal reports whether two Skills have the same canonical declaration semantics.
func (skill Skill) Equal(other Skill) bool {
	leftTargets, leftErr := target.CanonicalSet(skill.targets.Values())
	rightTargets, rightErr := target.CanonicalSet(other.targets.Values())
	return leftErr == nil && rightErr == nil &&
		skill.id == other.id &&
		skill.installName == other.installName &&
		skill.source == other.source &&
		slices.Equal(leftTargets, rightTargets) &&
		maps.Equal(skill.placements, other.placements) &&
		skill.scope == other.scope &&
		skill.installMode == other.installMode &&
		skill.portable == other.portable &&
		skill.compatRepair == other.compatRepair
}
