package lock

import (
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/realization"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
	"github.com/isty2e/daem/test/outputtest"
)

func TestLockedSectionRejectsDuplicateManagedPathOccupancyBeforeProfileAdmission(t *testing.T) {
	contracts := []LockedSubjectContract{
		testPathProjectionContractWith(
			t, entity.KindSkill, "skill", "skill.project.test", "shared/path",
			realization.PathProjectionDirectory, realization.PathPermissionsNone,
		),
		testPathProjectionContractWith(
			t, entity.KindInstructions, "instructions", "instructions.project.test", "shared/path",
			realization.PathProjectionFile, realization.PathPermissionsExecutableClass,
		),
		testPathProjectionContractWith(
			t, entity.KindHookAsset, "hook-asset", "hook-asset.project.test", "shared/path",
			realization.PathProjectionFile, realization.PathPermissionsExact,
		),
	}

	for leftIndex := range contracts {
		for rightIndex := leftIndex + 1; rightIndex < len(contracts); rightIndex++ {
			left, right := contracts[leftIndex], contracts[rightIndex]
			var canonicalError string
			for _, subjects := range [][]LockedSubjectContract{{right, left}, {left, right}} {
				_, err := NewLockedSection(subjects, nil)
				if err == nil || !strings.Contains(err.Error(), "duplicate managed path occupancy") {
					t.Fatalf("NewLockedSection error = %v, want duplicate managed path occupancy", err)
				}
				if !strings.Contains(err.Error(), left.SubjectID().String()) || !strings.Contains(err.Error(), right.SubjectID().String()) {
					t.Fatalf("NewLockedSection error = %q, want both subject identities", err)
				}
				if canonicalError == "" {
					canonicalError = err.Error()
					continue
				}
				if err.Error() != canonicalError {
					t.Fatalf("duplicate diagnostic depends on input order:\nfirst:  %q\nsecond: %q", canonicalError, err.Error())
				}
			}
		}
	}
}

func TestLockedSectionRejectsInstructionsPathOutsideCurrentProfileRefinement(t *testing.T) {
	contract := testPathProjectionContract(t, "review", "codex.project.instructions", "AGENTS.md")
	supply := testExactSupplyContract(t, entity.KindInstructions, "review", artifact.ArtifactKindFile)

	_, err := NewLockedSection([]LockedSubjectContract{supply, contract}, nil)
	if err == nil || !strings.Contains(err.Error(), "is not selected by its consumers") {
		t.Fatalf("NewLockedSection error = %v, want profile refinement diagnostic", err)
	}
}

func TestLockedSectionIndexesCanonicalSubjectAndEntityViews(t *testing.T) {
	firstInput := testMCPProjectionInput(
		t,
		mustTestMCPPlacement(t, aggregate.MCPPlacementCodexProject),
		nil,
	)
	firstInput.CanonicalProjection = "command = \"npx\"\n"
	first, err := NewMCPProjectionSubjectContract(firstInput)
	if err != nil {
		t.Fatal(err)
	}
	secondInput := testMCPProjectionInput(
		t,
		mustTestMCPPlacement(t, aggregate.MCPPlacementCodexGlobal),
		nil,
	)
	secondInput.CanonicalProjection = "command = \"npx\"\n"
	second, err := NewMCPProjectionSubjectContract(secondInput)
	if err != nil {
		t.Fatal(err)
	}
	section, err := NewLockedSection([]LockedSubjectContract{second, first}, nil)
	if err != nil {
		t.Fatal(err)
	}

	got, ok := section.Subject(second.SubjectID())
	if !ok || !got.Equal(second) {
		t.Fatalf("Subject = %#v, %t; want second contract", got, ok)
	}
	if _, ok := section.Subject(topology.SubjectID{}); ok {
		t.Fatal("Subject accepted zero identity")
	}
	entitySubjects := section.Subjects()
	if len(entitySubjects) != 2 ||
		entitySubjects[0].CompareIdentity(entitySubjects[1]) >= 0 {
		t.Fatalf("Subjects = %#v, want two canonically ordered contracts", entitySubjects)
	}
	entitySubjects[0] = LockedSubjectContract{}
	if got := section.Subjects(); len(got) != 2 || got[0].SubjectID().IsZero() {
		t.Fatalf("Subjects mutation changed section: %#v", got)
	}
}

func TestLockedSectionDoesNotDispatchUnknownAggregateToMCPRefinement(t *testing.T) {
	contract := testAggregateProjectionContract(t, "review", "future.project.hook")

	_, err := NewLockedSection([]LockedSubjectContract{contract}, nil)
	if err == nil || !strings.Contains(err.Error(), "subject has no current topology refinement") {
		t.Fatalf("NewLockedSection error = %v, want unadmitted refinement diagnostic", err)
	}
	if strings.Contains(err.Error(), "MCP") {
		t.Fatalf("NewLockedSection error = %q, unknown aggregate must not enter MCP refinement", err)
	}
}

func TestLockedCollectionAllowsSharedAggregateSubjectsWithOneStaticContract(t *testing.T) {
	left := testAggregateProjectionContractWith(
		t, "left", "future.project.hook", "hook.project", "/hooks", aggregate.ContributionSharedSet, "hook-project-v1",
	)
	right := testAggregateProjectionContractWith(
		t, "right", "future.project.hook", "hook.project", "/hooks", aggregate.ContributionSharedSet, "hook-project-v1",
	)
	if _, err := validateLockedCollection([]LockedSubjectContract{right, left}); err != nil {
		t.Fatalf("validateLockedCollection returned error: %v", err)
	}
}

func TestLockedCollectionRejectsExclusiveAndConflictingPhysicalAggregateOccupancy(t *testing.T) {
	tests := []struct {
		name  string
		left  LockedSubjectContract
		right LockedSubjectContract
		want  string
	}{
		{
			name: "exclusive",
			left: testAggregateProjectionContractWith(
				t, "left", "future.project.hook", "hook.project", "/hooks", aggregate.ContributionExclusive, "hook-project-v1",
			),
			right: testAggregateProjectionContractWith(
				t, "right", "future.project.hook", "hook.project", "/hooks", aggregate.ContributionExclusive, "hook-project-v1",
			),
			want: "duplicate exclusive managed aggregate occupancy",
		},
		{
			name: "placement alias",
			left: testAggregateProjectionContractWith(
				t, "left", "future.project.hook", "hook.project", "/hooks", aggregate.ContributionSharedSet, "hook-project-v1",
			),
			right: testAggregateProjectionContractWith(
				t, "right", "future.project.hook", "hook.alias", "/hooks", aggregate.ContributionSharedSet, "hook-project-v1",
			),
			want: "conflicting managed aggregate occupancy",
		},
		{
			name: "codec drift",
			left: testAggregateProjectionContractWith(
				t, "left", "future.project.hook", "hook.project", "/hooks", aggregate.ContributionSharedSet, "hook-project-v1",
			),
			right: testAggregateProjectionContractWith(
				t, "right", "future.project.hook", "hook.project", "/hooks", aggregate.ContributionSharedSet, "hook-project-v2",
			),
			want: "conflicting managed aggregate occupancy",
		},
		{
			name: "target alias",
			left: testAggregateProjectionContractAtTarget(
				t, "left", "future.project.hook", target.TargetCodex, "hook.project", "/hooks", aggregate.ContributionSharedSet, "hook-project-v1",
			),
			right: testAggregateProjectionContractAtTarget(
				t, "right", "future.project.hook", target.TargetClaudeCode, "hook.project", "/hooks", aggregate.ContributionSharedSet, "hook-project-v1",
			),
			want: "conflicting managed aggregate occupancy",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := validateLockedCollection([]LockedSubjectContract{test.right, test.left})
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("validateLockedCollection error = %v, want containing %q", err, test.want)
			}
			if !strings.Contains(err.Error(), test.left.SubjectID().String()) || !strings.Contains(err.Error(), test.right.SubjectID().String()) {
				t.Fatalf("collision error does not identify both subjects: %q", err)
			}
		})
	}
}

func testPathProjectionContract(
	t *testing.T,
	name string,
	namespace string,
	destination string,
) LockedSubjectContract {
	return testPathProjectionContractWith(
		t,
		entity.KindInstructions,
		name,
		namespace,
		destination,
		realization.PathProjectionFile,
		realization.PathPermissionsExecutableClass,
	)
}

func testPathProjectionContractWith(
	t *testing.T,
	kind entity.Kind,
	name string,
	namespace string,
	destination string,
	contentKind realization.PathProjectionContentKind,
	permissionPolicy realization.PathPermissionPolicy,
) LockedSubjectContract {
	t.Helper()
	exactMode := realization.ExactPathPermissionMode{}
	if permissionPolicy == realization.PathPermissionsExact {
		var err error
		exactMode, err = realization.NewExactPathPermissionMode(0o640)
		if err != nil {
			t.Fatal(err)
		}
	}
	realization, err := realization.NewManagedPathProjection(realization.ManagedPathProjectionInput{
		PlacementID: "instructions.project", ConsumerTargets: []target.Target{target.TargetCodex}, Scope: target.ScopeProject,
		Destination: outputtest.Parse(t, destination), ContentKind: contentKind,
		PlacementMode:          realization.PathProjectionCopy,
		PermissionPolicy:       permissionPolicy,
		ExactPermissionMode:    exactMode,
		AdapterContractVersion: "managed-path-v1",
	})
	if err != nil {
		t.Fatal(err)
	}
	return mustLockedSubjectContract(t, LockedSubjectContractInput{
		EntityID:    mustContractEntityID(t, kind, name),
		SubjectID:   mustTopologySubjectID(t, topology.SubjectProjection, namespace, name),
		Realization: &realization,
		Ownership:   OwnershipManifest, OnAbsent: OnAbsentApply,
		Replay:             mustContractReplay(t, ReplayExact, ReplayExact, ReplayNotApplicable),
		OperationContracts: contractProjectionOperations(t, "managed-path-v1"),
	})
}

func testExactSupplyContract(
	t *testing.T,
	kind entity.Kind,
	name string,
	artifactKind artifact.ArtifactKind,
) LockedSubjectContract {
	t.Helper()
	exact := mustContractExactIdentityOfKind(t, name, artifactKind)
	direct, err := NewDirectResolutionDerivation(exact)
	if err != nil {
		t.Fatal(err)
	}
	input := ExactSupplySubjectInput{
		EntityID:    mustContractEntityID(t, kind, name),
		SubjectID:   mustTopologySubjectID(t, topology.SubjectResource, string(kind), name),
		ExactSupply: exact,
		Derivation:  direct,
	}
	if kind == entity.KindInstructions {
		use, useErr := NewExactFileUse(target.ScopeProject, false)
		if useErr != nil {
			t.Fatal(useErr)
		}
		input.ExactFileUse = &use
	}
	contract, err := NewExactSupplySubjectContract(input)
	if err != nil {
		t.Fatal(err)
	}
	return contract
}

func testAggregateProjectionContract(t *testing.T, name string, namespace string) LockedSubjectContract {
	return testAggregateProjectionContractWith(
		t, name, namespace, "hook.project", "/hooks/"+name, aggregate.ContributionSharedSet, "hook-project-v1",
	)
}

func testAggregateProjectionContractWith(
	t *testing.T,
	name string,
	namespace string,
	placementID string,
	contentPath string,
	cardinality aggregate.ContributionCardinality,
	codecContract aggregate.CodecContractID,
) LockedSubjectContract {
	return testAggregateProjectionContractAtTarget(
		t, name, namespace, target.TargetCodex, placementID, contentPath, cardinality, codecContract,
	)
}

func testAggregateProjectionContractAtTarget(
	t *testing.T,
	name string,
	namespace string,
	selectedTarget target.Target,
	placementID string,
	contentPath string,
	cardinality aggregate.ContributionCardinality,
	codecContract aggregate.CodecContractID,
) LockedSubjectContract {
	t.Helper()
	realization, err := realization.NewManagedAggregateContribution(aggregate.ManagedContributionInput{
		PlacementID: placementID, Target: selectedTarget, Scope: target.ScopeProject,
		AggregateRoot: outputtest.Parse(t, "settings.json"), ContentPath: contentPath,
		MergeUnit: "entry", Cardinality: cardinality,
		SiblingRetention:    aggregate.PreserveUnmanagedSiblings,
		SiblingPreservation: aggregate.PreserveSiblingsSemantic,
		Equivalence:         aggregate.EquivalenceCanonicalSemantic, CanonicalContribution: `{}`,
		CodecContractID: codecContract, ComparedFields: []string{"command"},
	})
	if err != nil {
		t.Fatal(err)
	}
	return mustLockedSubjectContract(t, LockedSubjectContractInput{
		EntityID:    mustContractEntityID(t, entity.KindHook, name),
		SubjectID:   mustTopologySubjectID(t, topology.SubjectProjection, namespace, name),
		Realization: &realization, Ownership: OwnershipManifest, OnAbsent: OnAbsentApply,
		Replay:             mustContractReplay(t, ReplayExact, ReplayExact, ReplayNotApplicable),
		OperationContracts: contractProjectionOperations(t, string(codecContract)),
	})
}
