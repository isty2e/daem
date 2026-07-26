package build

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/desired"
	"github.com/isty2e/daem/internal/desired/entity"
	hookassetresource "github.com/isty2e/daem/internal/desired/hookasset"
	instructionsresource "github.com/isty2e/daem/internal/desired/instructions"
	skillresource "github.com/isty2e/daem/internal/desired/skill"
	desiredtest "github.com/isty2e/daem/internal/desired/testfixture"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/realization"
	lock "github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/realization/lock/refine"
	"github.com/isty2e/daem/internal/realization/lock/snapshottest"
	"github.com/isty2e/daem/internal/realization/profile"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/supply/artifact/access"
	skillrepair "github.com/isty2e/daem/internal/supply/compat/skill/repair"
	"github.com/isty2e/daem/internal/supply/source"
	"github.com/isty2e/daem/internal/target"
	targetavailability "github.com/isty2e/daem/internal/target/availability"
	targetselection "github.com/isty2e/daem/internal/target/selection"
	"github.com/isty2e/daem/internal/topology"
	topologyprojection "github.com/isty2e/daem/internal/topology/projection"
	resourcetopology "github.com/isty2e/daem/internal/topology/resource"
)

func instructionResource(t *testing.T, name string, sourceSpec source.Source, selected target.Target) instructionsresource.Instructions {
	t.Helper()
	return desiredtest.Instructions(t, instructionsresource.Spec{
		Name:    name,
		Source:  sourceSpec,
		Targets: []target.Target{selected},
		Scope:   target.ScopeProject,
	})
}

func skillResource(t *testing.T, name string, sourceSpec source.Source, selected target.Target) skillresource.Skill {
	t.Helper()
	return desiredtest.Skill(t, skillresource.Spec{
		Name:        name,
		Source:      sourceSpec,
		Targets:     []target.Target{selected},
		Scope:       target.ScopeProject,
		InstallMode: skillresource.InstallModeCopy,
	})
}

func hookAssetResource(t *testing.T, name string, sourceSpec source.Source) hookassetresource.HookAsset {
	t.Helper()
	return desiredtest.HookAsset(t, hookassetresource.Spec{
		Name: name, Source: sourceSpec, ArtifactKind: hookassetresource.ArtifactKindFile,
		Scope: target.ScopeProject, Executable: true,
	})
}

func hostOutputEnvironment(t *testing.T, spec desired.Spec) desired.Environment {
	t.Helper()
	spec.Targets = target.SupportedTargets()
	spec.Defaults = desiredtest.Defaults(t, target.ScopeProject, skillresource.InstallModeCopy)
	return desiredtest.Environment(t, spec)
}

func lockedInstructionsForPath(
	t *testing.T,
	ctx context.Context,
	root string,
	name string,
	sourceSpec source.Source,
	relativePath string,
	selected target.Target,
) []lock.LockedSubjectContract {
	t.Helper()

	identity, _ := resolvedHostOutputArtifact(t, ctx, root, sourceSpec, relativePath)
	supply := lockedInstructionExactSupply(t, name, identity)
	value := instructionResource(t, name, sourceSpec, selected)
	projections, err := refine.InstructionsPathProjections(value)
	if err != nil {
		t.Fatalf("InstructionsPathProjections returned error: %v", err)
	}
	return append([]lock.LockedSubjectContract{supply}, projections...)
}

func lockedInstructionExactSupply(
	t *testing.T,
	name string,
	identity artifact.ExactIdentity,
) lock.LockedSubjectContract {
	t.Helper()
	use, err := lock.NewExactFileUse(target.ScopeProject, false)
	if err != nil {
		t.Fatal(err)
	}
	derivation, err := lock.NewDirectResolutionDerivation(identity)
	if err != nil {
		t.Fatal(err)
	}
	entityID, err := entity.New(entity.KindInstructions, name)
	if err != nil {
		t.Fatal(err)
	}
	subject, err := resourcetopology.Subject(entityID)
	if err != nil {
		t.Fatal(err)
	}
	contract, err := lock.NewExactSupplySubjectContract(lock.ExactSupplySubjectInput{
		EntityID: entityID, SubjectID: subject,
		ExactSupply: identity, ExactFileUse: &use, Derivation: derivation,
	})
	if err != nil {
		t.Fatal(err)
	}
	return contract
}

func lockedDirectExactSupply(
	t *testing.T,
	kind entity.Kind,
	name string,
	identity artifact.ExactIdentity,
) lock.LockedSubjectContract {
	t.Helper()
	return snapshottest.ExactSupply(t, snapshottest.ExactSupplyInput{
		Kind:         kind,
		Name:         name,
		SourceID:     identity.SourceID(),
		ResolvedRef:  identity.ResolvedRef(),
		ArtifactKind: identity.Kind(),
		ContentHash:  identity.ContentHash(),
	})
}

func resolvedHostOutputArtifact(
	t *testing.T,
	ctx context.Context,
	root string,
	sourceSpec source.Source,
	relativePath string,
) (artifact.ExactIdentity, access.View) {
	t.Helper()

	sourceID, err := source.SourceIDFor(sourceSpec)
	if err != nil {
		t.Fatalf("SourceIDFor returned error: %v", err)
	}
	contentPath := filepath.Join(root, relativePath)
	view, err := access.OpenView(contentPath)
	if err != nil {
		t.Fatalf("OpenView returned error: %v", err)
	}
	contentHash, err := view.Hash(ctx)
	if err != nil {
		t.Fatalf("View.Hash returned error: %v", err)
	}
	identity, err := artifact.NewExactIdentity(sourceID, "", view.Kind(), contentHash)
	if err != nil {
		t.Fatalf("NewExactIdentity returned error: %v", err)
	}
	return identity, view
}

func lockedSkillResourceFromRecipe(
	t *testing.T,
	name string,
	result skillrepair.Result,
) []lock.LockedSubjectContract {
	t.Helper()
	recipe, ok := result.Recipe()
	if !ok {
		t.Fatal("repaired result is missing recipe")
	}
	derivation, err := lock.NewDeterministicTransformDerivation(lock.DeterministicTransform{
		InputIdentity:          recipe.Input(),
		RecipeHash:             recipe.Hash(),
		AlgorithmID:            skillrepair.DerivationAlgorithmID,
		AlgorithmVersion:       fmt.Sprintf("v%d", recipe.Version()),
		ExecutionDomain:        skillrepair.DerivationExecutionDomain,
		ExpectedOutputIdentity: recipe.Output(),
	})
	if err != nil {
		t.Fatalf("NewDeterministicTransformDerivation returned error: %v", err)
	}
	entityID, err := entity.New(entity.KindSkill, name)
	if err != nil {
		t.Fatalf("entity.New returned error: %v", err)
	}
	subjectID, err := resourcetopology.Subject(entityID)
	if err != nil {
		t.Fatalf("resource topology subject: %v", err)
	}
	locked, err := lock.NewExactSupplySubjectContract(lock.ExactSupplySubjectInput{
		EntityID:     entityID,
		SubjectID:    subjectID,
		ExactSupply:  result.Identity(),
		Derivation:   derivation,
		RepairRecipe: &recipe,
	})
	if err != nil {
		t.Fatalf("NewExactSupplySubjectContract returned error: %v", err)
	}
	placements, err := profile.ManagedPathPlacementsFor(
		entity.KindSkill,
		target.ScopeProject,
		[]target.Target{target.TargetCodex},
	)
	if err != nil || len(placements) != 1 {
		t.Fatalf("ManagedPathPlacementsFor = %#v, %v", placements, err)
	}
	placement := placements[0]
	destination, err := placement.ChildDestination(name)
	if err != nil {
		t.Fatalf("ChildDestination returned error: %v", err)
	}
	writeRoute, err := profile.ManagedPathOperationRoute(placement, profile.OperationWrite)
	if err != nil {
		t.Fatalf("write route: %v", err)
	}
	removeRoute, err := profile.ManagedPathOperationRoute(placement, profile.OperationRemove)
	if err != nil {
		t.Fatalf("remove route: %v", err)
	}
	spec, err := placement.Realize(destination, realization.PathProjectionCopy, writeRoute)
	if err != nil {
		t.Fatalf("Realize returned error: %v", err)
	}
	projectionSubject, err := topologyprojection.Subject(entityID, placement.ID())
	if err != nil {
		t.Fatalf("projection subject: %v", err)
	}
	projection, err := lock.NewManagedPathSubjectContract(lock.ManagedPathSubjectInput{
		EntityID: entityID, SubjectID: projectionSubject, Realization: spec,
		WriteRouteID: writeRoute.RouteID(), RemoveRouteID: removeRoute.RouteID(),
	})
	if err != nil {
		t.Fatalf("NewManagedPathSubjectContract returned error: %v", err)
	}
	return []lock.LockedSubjectContract{locked, projection}
}

func skillProjectionSubject(t *testing.T, name string, selected target.Target) topology.SubjectID {
	t.Helper()
	entityID, err := entity.New(entity.KindSkill, name)
	if err != nil {
		t.Fatalf("entity.New returned error: %v", err)
	}
	placements, err := profile.ManagedPathPlacementsFor(
		entity.KindSkill,
		target.ScopeProject,
		[]target.Target{selected},
	)
	if err != nil || len(placements) != 1 {
		t.Fatalf("ManagedPathPlacementsFor = %#v, %v", placements, err)
	}
	subject, err := topologyprojection.Subject(entityID, placements[0].ID())
	if err != nil {
		t.Fatalf("projection subject: %v", err)
	}
	return subject
}

func hostOutputTestPaths(t *testing.T, root string) daempaths.Paths {
	t.Helper()

	paths, err := daempaths.Resolve(filepath.Join(root, "daem.toml"))
	if err != nil {
		t.Fatalf("paths.Resolve returned error: %v", err)
	}
	return paths
}

func mustHostOutputSelection(t *testing.T, environment desired.Environment, requested ...string) targetselection.Selection {
	t.Helper()

	availableTargets := targetavailability.FromEnvironment(environment)
	selection, err := targetselection.ForAvailableTargets(availableTargets, requested)
	if err != nil {
		t.Fatalf("ForAvailableTargets returned error: %v", err)
	}
	return selection
}

func writeHostOutputTestFile(t *testing.T, root string, relativePath string, content string) {
	t.Helper()

	path := filepath.Join(root, relativePath)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
}
