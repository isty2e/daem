package commandhook

import (
	"fmt"
	"sort"
	"strings"

	"github.com/isty2e/daem/internal/desired/entity"
	desiredhook "github.com/isty2e/daem/internal/desired/hook"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/topology"
	topologyhook "github.com/isty2e/daem/internal/topology/hook"
	topologyprojection "github.com/isty2e/daem/internal/topology/projection"
)

// ContributionInput contains the semantic fields of one native Hook handler.
// Concrete host codecs own the serialized representation.
type ContributionInput struct {
	Event         string
	Matcher       string
	Type          string
	Command       string
	Condition     string
	Timeout       int
	StatusMessage string
}

// ContributionEncoder renders one validated semantic Hook contribution.
type ContributionEncoder func(ContributionInput) (string, error)

type assetPathMode uint8

const (
	assetPathsPortable assetPathMode = iota
	assetPathsAvailable
)

// PortableContributions refines complete Hook topology with portable desired
// commands for lock construction.
func PortableContributions(
	values []desiredhook.Hook,
	lowered topologyhook.Model,
	encoder ContributionEncoder,
) ([]aggregate.SubjectContribution, error) {
	return refineContributions(values, lowered, nil, assetPathsPortable, encoder)
}

// ContributionsWithAvailablePaths resolves a Hook command only when every
// consumed HookAsset subject has a current locked path. Otherwise it retains
// the portable command so the missing path contract can block independently.
func ContributionsWithAvailablePaths(
	values []desiredhook.Hook,
	lowered topologyhook.Model,
	assetPaths map[topology.SubjectID]string,
	encoder ContributionEncoder,
) ([]aggregate.SubjectContribution, error) {
	return refineContributions(values, lowered, assetPaths, assetPathsAvailable, encoder)
}

// Contribution refines one topology-correlated Hook projection with the
// supplied portable or resolved command.
func Contribution(
	value desiredhook.Hook,
	projection topologyhook.Projection,
	command string,
	encoder ContributionEncoder,
) (aggregate.SubjectContribution, error) {
	if err := value.Validate(); err != nil {
		return aggregate.SubjectContribution{}, err
	}
	selectedTarget := projection.Target()
	if projection.EntityID() != value.ID() || projection.Scope() != value.Scope() {
		return aggregate.SubjectContribution{}, fmt.Errorf("hook %q topology projection does not match desired identity or scope", value.ID().Name())
	}
	effectiveMatch, err := value.EffectiveMatch(selectedTarget)
	if err != nil {
		return aggregate.SubjectContribution{}, err
	}
	placement, admitted := aggregate.HookPlacementFor(selectedTarget, value.Scope())
	if !admitted || string(placement.ID()) != projection.SubjectID().Namespace() {
		return aggregate.SubjectContribution{}, fmt.Errorf("hook %q target %q has no matching native aggregate placement", value.ID().Name(), selectedTarget)
	}

	matcher := effectiveMatch.Matcher()
	condition := effectiveMatch.Condition()
	if err := ValidateShape(value.ID().Name(), selectedTarget, value.Event(), matcher, condition); err != nil {
		return aggregate.SubjectContribution{}, err
	}
	if encoder == nil {
		return aggregate.SubjectContribution{}, fmt.Errorf(
			"lower hook %q target %q contribution: Hook contribution encoder is required",
			value.ID().Name(),
			selectedTarget,
		)
	}
	canonical, err := encoder(ContributionInput{
		Event:         strings.TrimSpace(value.Event()),
		Matcher:       matcher,
		Type:          string(value.Type()),
		Command:       strings.TrimSpace(command),
		Condition:     condition,
		Timeout:       value.TimeoutSeconds(),
		StatusMessage: strings.TrimSpace(value.StatusMessage()),
	})
	if err != nil {
		return aggregate.SubjectContribution{}, fmt.Errorf("lower hook %q target %q contribution: %w", value.ID().Name(), selectedTarget, err)
	}
	contribution, err := placement.Contribution(canonical)
	if err != nil {
		return aggregate.SubjectContribution{}, fmt.Errorf("lower hook %q target %q realization: %w", value.ID().Name(), selectedTarget, err)
	}
	item, err := aggregate.NewSubjectContribution(projection.SubjectID(), contribution)
	if err != nil {
		return aggregate.SubjectContribution{}, err
	}
	return item, nil
}

func refineContributions(
	values []desiredhook.Hook,
	lowered topologyhook.Model,
	assetPaths map[topology.SubjectID]string,
	pathMode assetPathMode,
	encoder ContributionEncoder,
) ([]aggregate.SubjectContribution, error) {
	valuesByEntity := make(map[entity.ID]desiredhook.Hook, len(values))
	for index, value := range values {
		if err := value.Validate(); err != nil {
			return nil, fmt.Errorf("hook[%d]: %w", index, err)
		}
		if _, duplicate := valuesByEntity[value.ID()]; duplicate {
			return nil, fmt.Errorf("hook[%d]: duplicate Hook identity %q", index, value.ID())
		}
		valuesByEntity[value.ID()] = value
	}
	if err := validateAssetPaths(lowered, assetPaths, pathMode); err != nil {
		return nil, err
	}

	result := make([]aggregate.SubjectContribution, 0, len(lowered.Projections()))
	for _, projection := range lowered.Projections() {
		value, present := valuesByEntity[projection.EntityID()]
		if !present {
			return nil, fmt.Errorf("Hook topology subject %q has no desired Hook", projection.SubjectID())
		}
		command := value.Command()
		assetSubjects := lowered.AssetSubjectsOf(projection.SubjectID())
		resolveCommand := pathMode == assetPathsAvailable && allAssetPathsAvailable(assetSubjects, assetPaths)
		if resolveCommand && len(assetSubjects) != 0 {
			replacements := make(map[desiredhook.AssetReference]string)
			referencesByID := make(map[string]desiredhook.AssetReference, len(value.AssetReferences()))
			for _, reference := range value.AssetReferences() {
				referencesByID[reference.ID()] = reference
			}
			for _, assetSubject := range assetSubjects {
				assetID, entityBacked := topologyprojection.EntityID(assetSubject)
				if !entityBacked || assetID.Kind() != entity.KindHookAsset {
					return nil, fmt.Errorf("Hook topology subject %q consumes non-HookAsset subject %q", projection.SubjectID(), assetSubject)
				}
				reference, present := referencesByID[assetID.Name()]
				if !present {
					return nil, fmt.Errorf(
						"Hook topology subject %q consumes HookAsset %q absent from desired Hook references",
						projection.SubjectID(),
						assetID.Name(),
					)
				}
				replacements[reference] = assetPaths[assetSubject]
			}
			rendered, err := value.RenderCommand(replacements)
			if err != nil {
				return nil, fmt.Errorf("render Hook %q target %q command: %w", value.ID().Name(), projection.Target(), err)
			}
			command = rendered
		}
		item, err := Contribution(value, projection, command, encoder)
		if err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	sort.Slice(result, func(left int, right int) bool {
		return topology.CompareSubjectID(result[left].SubjectID(), result[right].SubjectID()) < 0
	})
	return result, nil
}

func validateAvailableAssetPaths(lowered topologyhook.Model, assetPaths map[topology.SubjectID]string) error {
	expected := make(map[topology.SubjectID]struct{}, len(lowered.AssetProjections()))
	for _, projection := range lowered.AssetProjections() {
		expected[projection.SubjectID()] = struct{}{}
	}
	for subject, path := range assetPaths {
		if _, present := expected[subject]; !present {
			return fmt.Errorf("resolved HookAsset path %q is outside Hook topology", subject)
		}
		if strings.TrimSpace(path) == "" {
			return fmt.Errorf("HookAsset subject %q has an empty resolved path", subject)
		}
	}
	return nil
}

func allAssetPathsAvailable(subjects []topology.SubjectID, paths map[topology.SubjectID]string) bool {
	for _, subject := range subjects {
		if _, present := paths[subject]; !present {
			return false
		}
	}
	return true
}

func validateAssetPaths(
	lowered topologyhook.Model,
	assetPaths map[topology.SubjectID]string,
	pathMode assetPathMode,
) error {
	switch pathMode {
	case assetPathsPortable:
		if len(assetPaths) != 0 {
			return fmt.Errorf("portable Hook refinement must not carry resolved HookAsset paths")
		}
		return nil
	case assetPathsAvailable:
		return validateAvailableAssetPaths(lowered, assetPaths)
	default:
		return fmt.Errorf("unsupported HookAsset path refinement mode %d", pathMode)
	}
}
