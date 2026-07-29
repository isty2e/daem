package refine

import (
	"reflect"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/desired/instructions"
	desiredtest "github.com/isty2e/daem/internal/desired/testfixture"
	"github.com/isty2e/daem/internal/realization"
	"github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/supply/source"
	"github.com/isty2e/daem/internal/supply/source/sourcetest"
	"github.com/isty2e/daem/internal/target"
	resourcetopology "github.com/isty2e/daem/internal/topology/resource"
)

func TestInstructionsPathProjectionContractsCoalesceSharedPhysicalFile(t *testing.T) {
	value := desiredtest.Instructions(t, instructions.Spec{
		Name: "shared", Source: sourcetest.Local(t, "instructions/shared.md", source.LocalSourceModeVendor),
		Targets: []target.Target{
			target.TargetPi,
			target.TargetCodex,
			target.TargetAntigravityCLI,
			target.TargetOpenCode,
		},
		Scope: target.ScopeProject,
	})
	contracts, err := InstructionsPathProjections(value)
	if err != nil {
		t.Fatal(err)
	}
	if len(contracts) != 1 {
		t.Fatalf("contracts = %#v, want one shared projection", contracts)
	}
	spec, _ := contracts[0].Realization()
	projection, _ := spec.ManagedPathProjection()
	wantTargets := []target.Target{
		target.TargetAntigravityCLI,
		target.TargetCodex,
		target.TargetOpenCode,
		target.TargetPi,
	}
	if projection.PlacementID() != "instructions.project.agents" ||
		projection.Destination().String() != "AGENTS.md" ||
		projection.ContentKind() != realization.PathProjectionFile ||
		projection.PlacementMode() != realization.PathProjectionCopy ||
		!reflect.DeepEqual(projection.ConsumerTargets(), wantTargets) {
		t.Fatalf("projection = %#v consumers=%#v", projection, projection.ConsumerTargets())
	}

	supply := exactInstructionsSupplyContract(t, value.ID().Name(), "instructions/shared.md", "shared")
	if _, err := lock.NewLockedSection(append([]lock.LockedSubjectContract{supply}, contracts...), nil); err != nil {
		t.Fatalf("NewLockedSection rejected canonical Instructions projection: %v", err)
	}
}

func TestLockedSectionRejectsInstructionsSupplyWithoutProjection(t *testing.T) {
	supply := exactInstructionsSupplyContract(t, "orphan", "instructions/orphan.md", "orphan")

	_, err := lock.NewLockedSection([]lock.LockedSubjectContract{supply}, nil)
	if err == nil || !strings.Contains(err.Error(), "has no managed file projection") {
		t.Fatalf("NewLockedSection error = %v, want missing Instructions projection diagnostic", err)
	}
}

func exactInstructionsSupplyContract(
	t *testing.T,
	name string,
	sourcePath string,
	content string,
) lock.LockedSubjectContract {
	t.Helper()
	entityID, err := entity.New(entity.KindInstructions, name)
	if err != nil {
		t.Fatalf("entity.New: %v", err)
	}
	identity, err := artifact.NewExactIdentity(
		artifact.SourceID("local:"+sourcePath+"?mode=vendor"),
		"",
		artifact.ArtifactKindFile,
		artifact.HashFileContent([]byte(content)),
	)
	if err != nil {
		t.Fatalf("artifact.NewExactIdentity: %v", err)
	}
	derivation, err := lock.NewDirectResolutionDerivation(identity)
	if err != nil {
		t.Fatalf("lock.NewDirectResolutionDerivation: %v", err)
	}
	fileUse, err := lock.NewExactFileUse(target.ScopeProject, false)
	if err != nil {
		t.Fatalf("lock.NewExactFileUse: %v", err)
	}
	subjectID, err := resourcetopology.Subject(entityID)
	if err != nil {
		t.Fatalf("resource topology subject: %v", err)
	}
	contract, err := lock.NewExactSupplySubjectContract(lock.ExactSupplySubjectInput{
		EntityID:     entityID,
		SubjectID:    subjectID,
		ExactSupply:  identity,
		ExactFileUse: &fileUse,
		Derivation:   derivation,
	})
	if err != nil {
		t.Fatalf("lock.NewExactSupplySubjectContract: %v", err)
	}
	return contract
}

func TestInstructionsPathProjectionContractsKeepDistinctAndNonDefaultPlacements(t *testing.T) {
	gemini, err := instructions.NewRendering("GEMINI.md", instructions.RenderModeCopy)
	if err != nil {
		t.Fatal(err)
	}
	value := desiredtest.Instructions(t, instructions.Spec{
		Name: "mixed", Source: sourcetest.Local(t, "instructions/mixed.md", source.LocalSourceModeVendor),
		Targets: []target.Target{target.TargetClaudeCode, target.TargetAntigravityCLI},
		Scope:   target.ScopeProject,
		Renderings: map[target.Target]instructions.Rendering{
			target.TargetAntigravityCLI: gemini,
		},
	})
	contracts, err := InstructionsPathProjections(value)
	if err != nil {
		t.Fatal(err)
	}
	if len(contracts) != 2 {
		t.Fatalf("len(contracts) = %d, want 2", len(contracts))
	}
	want := map[string]string{
		"instructions.project.claude": "CLAUDE.md",
		"instructions.project.gemini": "GEMINI.md",
	}
	for _, contract := range contracts {
		spec, _ := contract.Realization()
		projection, _ := spec.ManagedPathProjection()
		if want[projection.PlacementID()] != projection.Destination().String() {
			t.Fatalf("projection = %#v, want destinations %#v", projection, want)
		}
	}
}

func TestInstructionsPathProjectionContractsRejectConflictingSharedModeAndDiscoveryPath(t *testing.T) {
	symlink, err := instructions.NewRendering("", instructions.RenderModeSymlink)
	if err != nil {
		t.Fatal(err)
	}
	conflicting := desiredtest.Instructions(t, instructions.Spec{
		Name: "conflict", Source: sourcetest.Local(t, "instructions/conflict.md", source.LocalSourceModeVendor),
		Targets: []target.Target{target.TargetCodex, target.TargetOpenCode}, Scope: target.ScopeProject,
		Renderings: map[target.Target]instructions.Rendering{target.TargetOpenCode: symlink},
	})
	if _, err := InstructionsPathProjections(conflicting); err == nil || !strings.Contains(err.Error(), "conflicting render modes") {
		t.Fatalf("conflicting mode error = %v", err)
	}

	discovery, err := instructions.NewRendering("CLAUDE.md", instructions.RenderModeCopy)
	if err != nil {
		t.Fatal(err)
	}
	invalid := desiredtest.Instructions(t, instructions.Spec{
		Name: "discovery", Source: sourcetest.Local(t, "instructions/discovery.md", source.LocalSourceModeVendor),
		Targets: []target.Target{target.TargetOpenCode}, Scope: target.ScopeProject,
		Renderings: map[target.Target]instructions.Rendering{target.TargetOpenCode: discovery},
	})
	if _, err := InstructionsPathProjections(invalid); err == nil || !strings.Contains(err.Error(), "not an admitted file placement") {
		t.Fatalf("discovery path error = %v", err)
	}
}
