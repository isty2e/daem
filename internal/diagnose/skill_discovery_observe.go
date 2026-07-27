package diagnose

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/desired/entity"
	skillresource "github.com/isty2e/daem/internal/desired/skill"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/output/hostpath"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/realization/profile"
	"github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/target"
	targetselection "github.com/isty2e/daem/internal/target/selection"
	topologyprojection "github.com/isty2e/daem/internal/topology/projection"
)

type skillDiscoveryCoverage struct {
	entityID    entity.ID
	target      target.Target
	scope       target.Scope
	destination output.Destination
}

type skillDiscoveryCoverageKey struct {
	entityID entity.ID
	target   target.Target
	scope    target.Scope
}

type skillDiscoveryCoverageIndex map[skillDiscoveryCoverageKey][]output.Destination

type skillDiscoveryObserver struct {
	stat func(string) (fs.FileInfo, error)
}

func inspectRetainedSkillDiscoveries(
	ctx context.Context,
	paths daempaths.Paths,
	skills []skillresource.Skill,
	selection targetselection.Selection,
	coverage []skillDiscoveryCoverage,
	observer skillDiscoveryObserver,
) []skillDiscoveryFinding {
	if ctx == nil || observer.stat == nil {
		return nil
	}
	resolver := hostpath.NewResolver(paths.ManifestRoot)
	coverageIndex := indexSkillDiscoveryCoverage(coverage)
	canonicalSkills := append([]skillresource.Skill(nil), skills...)
	sort.Slice(canonicalSkills, func(left int, right int) bool {
		return canonicalSkills[left].ID().String() < canonicalSkills[right].ID().String()
	})

	result := make([]skillDiscoveryFinding, 0)
	for _, skill := range canonicalSkills {
		if ctx.Err() != nil {
			break
		}
		for _, selectedTarget := range SelectedSkillTargets(skill, selection) {
			result = append(result, inspectSkillTargetDiscoveries(
				skill,
				selectedTarget,
				resolver,
				coverageIndex,
				observer,
			)...)
		}
	}
	sort.Slice(result, func(left int, right int) bool {
		return result[left].sortKey() < result[right].sortKey()
	})
	return result
}

func inspectSkillTargetDiscoveries(
	skill skillresource.Skill,
	selectedTarget target.Target,
	resolver hostpath.Resolver,
	coverage skillDiscoveryCoverageIndex,
	observer skillDiscoveryObserver,
) []skillDiscoveryFinding {
	selectedDestination, err := selectedSkillDestination(skill, selectedTarget)
	if err != nil {
		return []skillDiscoveryFinding{newSkillDiscoveryObservationFailure(
			skill,
			selectedTarget,
			"",
			"",
			err,
		)}
	}
	selectedPath, err := resolver.Resolve(selectedDestination)
	if err != nil {
		return []skillDiscoveryFinding{newSkillDiscoveryObservationFailure(
			skill,
			selectedTarget,
			selectedDestination.String(),
			selectedDestination.String(),
			err,
		)}
	}
	selectedInfo, selectedErr := observer.stat(selectedPath)
	if selectedErr != nil && !errors.Is(selectedErr, fs.ErrNotExist) {
		return []skillDiscoveryFinding{newSkillDiscoveryObservationFailure(
			skill,
			selectedTarget,
			selectedPath,
			selectedPath,
			selectedErr,
		)}
	}

	result := make([]skillDiscoveryFinding, 0)
	observed := make([]fs.FileInfo, 0)
	for _, location := range profile.Profile(selectedTarget).DiscoveryLocations(entity.KindSkill, skill.Scope()) {
		candidateDestination, err := output.Parse(path.Join(location.Path(), skill.InstallName()))
		if err != nil {
			result = append(result, newSkillDiscoveryObservationFailure(
				skill,
				selectedTarget,
				selectedPath,
				location.Path(),
				err,
			))
			continue
		}
		if candidateDestination.String() == selectedDestination.String() ||
			skillDiscoveryCovered(coverage, skill.ID(), selectedTarget, skill.Scope(), candidateDestination) {
			continue
		}
		candidatePath, err := resolver.Resolve(candidateDestination)
		if err != nil {
			result = append(result, newSkillDiscoveryObservationFailure(
				skill,
				selectedTarget,
				selectedPath,
				candidateDestination.String(),
				err,
			))
			continue
		}
		if filepath.Clean(candidatePath) == filepath.Clean(selectedPath) {
			continue
		}
		candidateInfo, err := observer.stat(candidatePath)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			result = append(result, newSkillDiscoveryObservationFailure(
				skill,
				selectedTarget,
				selectedPath,
				candidatePath,
				err,
			))
			continue
		}
		if !candidateInfo.IsDir() {
			continue
		}
		if selectedErr == nil && os.SameFile(selectedInfo, candidateInfo) {
			continue
		}
		if skillDiscoveryPhysicallyCovered(
			coverage,
			skill.ID(),
			selectedTarget,
			skill.Scope(),
			candidatePath,
			candidateInfo,
			resolver,
			observer,
		) {
			continue
		}
		if sameObservedFile(observed, candidateInfo) {
			continue
		}
		observed = append(observed, candidateInfo)
		result = append(result, newRetainedSkillDiscoveryFinding(
			skill,
			selectedTarget,
			selectedPath,
			candidatePath,
		))
	}
	return result
}

func selectedSkillDestination(
	skill skillresource.Skill,
	selectedTarget target.Target,
) (output.Destination, error) {
	requestedRoots := make(map[target.Target]string)
	if requested, ok := skill.TargetPlacements()[selectedTarget]; ok {
		requestedRoots[selectedTarget] = requested.InstallTo()
	}
	placements, err := profile.ManagedPathPlacementsForSelections(
		entity.KindSkill,
		skill.Scope(),
		[]target.Target{selectedTarget},
		requestedRoots,
	)
	if err != nil {
		return output.Destination{}, err
	}
	if len(placements) != 1 {
		return output.Destination{}, fmt.Errorf(
			"skill %q target %q selected %d write placements",
			skill.ID().Name(),
			selectedTarget,
			len(placements),
		)
	}
	return placements[0].ChildDestination(skill.InstallName())
}

func skillDiscoveryPhysicallyCovered(
	coverage skillDiscoveryCoverageIndex,
	entityID entity.ID,
	selectedTarget target.Target,
	scope target.Scope,
	candidatePath string,
	candidateInfo fs.FileInfo,
	resolver hostpath.Resolver,
	observer skillDiscoveryObserver,
) bool {
	key := skillDiscoveryCoverageKey{entityID: entityID, target: selectedTarget, scope: scope}
	for _, destination := range coverage[key] {
		coveredPath, err := resolver.Resolve(destination)
		if err != nil {
			continue
		}
		if filepath.Clean(coveredPath) == filepath.Clean(candidatePath) {
			return true
		}
		coveredInfo, err := observer.stat(coveredPath)
		if err == nil && os.SameFile(coveredInfo, candidateInfo) {
			return true
		}
	}
	return false
}

func skillDiscoveryCovered(
	coverage skillDiscoveryCoverageIndex,
	entityID entity.ID,
	selectedTarget target.Target,
	scope target.Scope,
	destination output.Destination,
) bool {
	key := skillDiscoveryCoverageKey{entityID: entityID, target: selectedTarget, scope: scope}
	for _, candidate := range coverage[key] {
		if candidate.String() == destination.String() {
			return true
		}
	}
	return false
}

func indexSkillDiscoveryCoverage(values []skillDiscoveryCoverage) skillDiscoveryCoverageIndex {
	result := make(skillDiscoveryCoverageIndex)
	for _, value := range values {
		key := skillDiscoveryCoverageKey{
			entityID: value.entityID,
			target:   value.target,
			scope:    value.scope,
		}
		result[key] = append(result[key], value.destination)
	}
	return result
}

func skillDiscoveryCoverageFromState(snapshot durable.Snapshot) []skillDiscoveryCoverage {
	result := make([]skillDiscoveryCoverage, 0)
	for _, state := range snapshot.ManagedPaths() {
		entityID, entityBacked := topologyprojection.EntityID(state.Subject())
		if !entityBacked || entityID.Kind() != entity.KindSkill {
			continue
		}
		for _, selectedTarget := range state.ConsumerTargets() {
			result = append(result, skillDiscoveryCoverage{
				entityID:    entityID,
				target:      selectedTarget,
				scope:       state.Scope(),
				destination: state.Destination(),
			})
		}
	}
	return result
}

func skillDiscoveryCoverageFromPlan(planned reconcile.Result) []skillDiscoveryCoverage {
	result := make([]skillDiscoveryCoverage, 0)
	for _, decision := range planned.ManagedPaths() {
		entityID, entityBacked := topologyprojection.EntityID(decision.Subject())
		if !entityBacked || entityID.Kind() != entity.KindSkill {
			continue
		}
		switch decision.Kind() {
		case reconcile.ManagedPathRemove:
			result = appendSkillDiscoveryCoverage(
				result,
				entityID,
				decision.ConsumerTargets(),
				decision.Scope(),
				decision.Destination(),
			)
		case reconcile.ManagedPathReplace:
			previous, present := decision.PreviousState()
			if !present || previous.Destination().String() == decision.Destination().String() {
				continue
			}
			result = appendSkillDiscoveryCoverage(
				result,
				entityID,
				previous.ConsumerTargets(),
				previous.Scope(),
				previous.Destination(),
			)
		}
	}
	return result
}

func appendSkillDiscoveryCoverage(
	result []skillDiscoveryCoverage,
	entityID entity.ID,
	targets []target.Target,
	scope target.Scope,
	destination output.Destination,
) []skillDiscoveryCoverage {
	for _, selectedTarget := range targets {
		result = append(result, skillDiscoveryCoverage{
			entityID:    entityID,
			target:      selectedTarget,
			scope:       scope,
			destination: destination,
		})
	}
	return result
}

func sameObservedFile(observed []fs.FileInfo, candidate fs.FileInfo) bool {
	for _, existing := range observed {
		if os.SameFile(existing, candidate) {
			return true
		}
	}
	return false
}
