package realization

import (
	"reflect"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/realization/aggregate"
	hostrelation "github.com/isty2e/daem/internal/realization/relation"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/test/outputtest"
)

func TestRealizationSpecVariantsRemainClosedAndValidated(t *testing.T) {
	pathSpec, err := NewManagedPathProjection(ManagedPathProjectionInput{
		PlacementID:            "instructions.project.agents",
		ConsumerTargets:        []target.Target{target.TargetCodex},
		Scope:                  target.ScopeProject,
		Destination:            outputtest.Parse(t, "AGENTS.md"),
		ContentKind:            PathProjectionFile,
		PlacementMode:          PathProjectionCopy,
		PermissionPolicy:       PathPermissionsExecutableClass,
		AdapterContractVersion: "managed-instruction-file-v1",
	})
	if err != nil {
		t.Fatalf("NewManagedPathProjection: %v", err)
	}
	if pathSpec.Kind() != RealizationManagedPathProjection || pathSpec.Validate() != nil {
		t.Fatalf("path spec = %#v", pathSpec)
	}

	aggregateSpec, err := NewManagedAggregateContribution(aggregate.ManagedContributionInput{
		PlacementID:           "codex.project.hooks",
		Target:                target.TargetCodex,
		Scope:                 target.ScopeProject,
		AggregateRoot:         outputtest.Parse(t, "settings.json"),
		ContentPath:           "/hooks",
		MergeUnit:             "hook-set",
		Cardinality:           aggregate.ContributionSharedSet,
		SiblingRetention:      aggregate.PreserveUnmanagedSiblings,
		SiblingPreservation:   aggregate.PreserveSiblingsSemantic,
		Equivalence:           aggregate.EquivalenceCanonicalSemantic,
		CanonicalContribution: `{"command":"review"}`,
		CodecContractID:       "codex-project-hooks-v1",
		ComparedFields:        []string{"command", "event"},
	})
	if err != nil {
		t.Fatalf("NewManagedAggregateContribution: %v", err)
	}
	if aggregateSpec.Kind() != RealizationManagedAggregateContribution || aggregateSpec.Validate() != nil {
		t.Fatalf("aggregate spec = %#v", aggregateSpec)
	}

	expected := testExpectedRelation(t)
	delegatedSpec, err := NewDelegatedRelation(DelegatedRelationInput{
		PlacementID:            "codex-plugin",
		Target:                 target.TargetCodex,
		Scope:                  target.ScopeGlobal,
		SourceNamespace:        "github",
		ExpectedRelation:       expected,
		RouteID:                "codex.plugin-carrier.install",
		RouteContractVersion:   "codex-plugin-carrier-v1",
		CanonicalRequestHash:   "sha256:" + strings.Repeat("a", 64),
		VerifiedRelationFields: []string{"target", "scope", "target"},
	})
	if err != nil {
		t.Fatalf("NewDelegatedRelation: %v", err)
	}
	if delegatedSpec.Kind() != RealizationDelegatedRelation || delegatedSpec.Validate() != nil {
		t.Fatalf("delegated spec = %#v", delegatedSpec)
	}
	if pathSpec.Equal(aggregateSpec) || aggregateSpec.Equal(delegatedSpec) {
		t.Fatal("different realization variants compare equal")
	}
	if (RealizationSpec{}).Validate() == nil {
		t.Fatal("zero realization spec validated")
	}
}

func TestManagedPathProjectionRejectsInvalidConsumerTarget(t *testing.T) {
	_, err := NewManagedPathProjection(ManagedPathProjectionInput{
		PlacementID:            "instructions.project.agents",
		ConsumerTargets:        []target.Target{"future"},
		Scope:                  target.ScopeProject,
		Destination:            outputtest.Parse(t, "AGENTS.md"),
		ContentKind:            PathProjectionFile,
		PlacementMode:          PathProjectionCopy,
		PermissionPolicy:       PathPermissionsExecutableClass,
		AdapterContractVersion: "managed-instruction-file-v1",
	})
	if err == nil || !strings.Contains(err.Error(), `target[0]: unknown target "future"`) {
		t.Fatalf("NewManagedPathProjection error = %v", err)
	}
}

func TestRealizationSpecAccessorsReturnDefensiveCopies(t *testing.T) {
	spec, err := NewManagedPathProjection(ManagedPathProjectionInput{
		PlacementID: "skill.project.agents", ConsumerTargets: []target.Target{target.TargetCodex},
		Scope: target.ScopeProject, Destination: outputtest.Parse(t, ".agents/skills/review"),
		ContentKind: PathProjectionDirectory, PlacementMode: PathProjectionCopy,
		PermissionPolicy: PathPermissionsNone, AdapterContractVersion: "managed-directory-v1",
	})
	if err != nil {
		t.Fatal(err)
	}
	targets := spec.ConsumerTargets()
	targets[0] = target.TargetPi
	if !reflect.DeepEqual(spec.ConsumerTargets(), []target.Target{target.TargetCodex}) {
		t.Fatal("caller mutation changed realization targets")
	}
}

func testExpectedRelation(t *testing.T) hostrelation.ExpectedRelation {
	t.Helper()
	subject, err := hostrelation.NewSubjectKey("market@plugin")
	if err != nil {
		t.Fatal(err)
	}
	managed, err := hostrelation.NewManagedInstanceKey("managed:plugin")
	if err != nil {
		t.Fatal(err)
	}
	expected, err := hostrelation.NewExpectedRelation(subject, managed)
	if err != nil {
		t.Fatal(err)
	}
	return expected
}
