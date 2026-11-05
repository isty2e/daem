package execute

import (
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/desired/entity"
	effectpreparation "github.com/isty2e/daem/internal/effect/execute/preparation"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization"
	"github.com/isty2e/daem/internal/realization/profile"
	"github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
	topologyprojection "github.com/isty2e/daem/internal/topology/projection"
	"github.com/isty2e/daem/test/outputtest"
)

func managedPathEffectForTest(t testing.TB, input reconcile.ManagedPathDecisionInput) effectpreparation.ManagedPathEffect {
	t.Helper()
	if input.Reason == "" {
		switch input.Kind {
		case reconcile.ManagedPathCreate:
			input.Reason = reconcile.ReasonMissingOutput
		case reconcile.ManagedPathReplace:
			input.Reason = reconcile.ReasonContentChanged
		case reconcile.ManagedPathRemove:
			input.Reason = reconcile.ReasonRemovedFromManifest
		case reconcile.ManagedPathRecord:
			input.Reason = reconcile.ReasonStateStale
		}
	}
	decision, err := reconcile.NewManagedPathDecision(input)
	if err != nil {
		t.Fatalf("managed path fixture decision: %v", err)
	}
	effects, err := effectpreparation.ManagedPathEffects([]reconcile.ManagedPathDecision{decision})
	if err != nil || len(effects) != 1 {
		t.Fatalf("managed path fixture admission: %d effects, %v", len(effects), err)
	}
	return effects[0]
}

type scopedDestinationForTest struct {
	scope       target.Scope
	destination output.Destination
}

func recordedPathAuthorityEffectForTest(t *testing.T, scope target.Scope, destination output.Destination) effectpreparation.ManagedPathEffect {
	t.Helper()
	owner, err := entity.New(entity.KindInstructions, "ordinary")
	if err != nil {
		t.Fatal(err)
	}
	placement := "instructions.project.agents"
	if scope == target.ScopeGlobal {
		placement = "instructions.global.codex"
	}
	subject, err := topologyprojection.Subject(owner, placement)
	if err != nil {
		t.Fatal(err)
	}
	hash := testArtifactHash("retained ordinary path")
	return managedPathEffectForTest(t, reconcile.ManagedPathDecisionInput{
		Kind: reconcile.ManagedPathRecord, Subject: subject, ConsumerTargets: []target.Target{target.TargetCodex},
		Scope: scope, Destination: destination, DesiredHash: hash, LiveHash: hash,
		ContentKind: realization.PathProjectionFile, PermissionPolicy: realization.PathPermissionsExact,
		DesiredFileMode: 0o600, LiveFileMode: 0o600,
	})
}

func captureScopedDestinationsForTest(paths Paths, selected []scopedDestinationForTest, demands recovery.RemovalDemandSet, borrowed *rootedpath.CapturedRoot, resolver DestinationResolver, filesystem mutationfs.Store) (*mutationAuthority, error) {
	if err := demands.Validate(); err != nil {
		return nil, err
	}
	budget, err := physicalwork.NewBudget(demands.Len())
	if err != nil {
		return nil, err
	}
	project := paths.ManifestRoot != ""
	for _, destination := range selected {
		project = project || destination.scope == target.ScopeProject
	}
	authority, err := captureMutationAuthorityWithPhysicalWorkBudget(paths, project, borrowed, resolver, filesystem, budget)
	if err != nil {
		return nil, err
	}
	if err := authority.prepareRemovalDemands(demands, budget); err != nil {
		_ = authority.close()
		return nil, err
	}
	for _, destination := range selected {
		if err := authority.bindPhysicalAuthority(destination.scope, destination.destination, nil); err != nil {
			_ = authority.close()
			return nil, err
		}
	}
	return authority, nil
}

func testExactManagedPathEffectState(t *testing.T) durable.ManagedPathState {
	t.Helper()
	id, err := entity.New(entity.KindHookAsset, "guard")
	if err != nil {
		t.Fatal(err)
	}
	placement, err := profile.HookAssetPlacementFor(target.ScopeProject, []target.Target{target.TargetCodex})
	if err != nil {
		t.Fatal(err)
	}
	subject, err := topologyprojection.Subject(id, placement.ID())
	if err != nil {
		t.Fatal(err)
	}
	state, err := durable.NewManagedPathState(
		subject, []target.Target{target.TargetCodex}, target.ScopeProject,
		outputtest.Parse(t, ".daem/hook-assets/guard/sha256-test/asset"), testArtifactHash("current"),
		realization.PathProjectionFile, realization.PathPermissionsExact, 0o700,
	)
	if err != nil {
		t.Fatal(err)
	}
	return state
}

func testManagedPathEffectState(t *testing.T, name string, destination output.Destination) durable.ManagedPathState {
	t.Helper()
	return testManagedPathEffectStateAt(t, name, "skill.project.agents", []target.Target{target.TargetCodex}, destination)
}

func testManagedPathEffectStateAt(t *testing.T, name string, placementID string, consumers []target.Target, destination output.Destination) durable.ManagedPathState {
	t.Helper()
	subject := testManagedPathEffectSubject(t, name, placementID)
	state, err := durable.NewManagedPathState(subject, consumers, target.ScopeProject, destination, testArtifactHash("old"), realization.PathProjectionDirectory, realization.PathPermissionsNone, 0)
	if err != nil {
		t.Fatalf("NewManagedPathState returned error: %v", err)
	}
	return state
}

func testManagedPathEffectSubject(t *testing.T, name string, placementID string) topology.SubjectID {
	t.Helper()
	id, err := entity.New(entity.KindSkill, name)
	if err != nil {
		t.Fatalf("entity.New returned error: %v", err)
	}
	subject, err := topologyprojection.Subject(id, placementID)
	if err != nil {
		t.Fatalf("projection.Subject returned error: %v", err)
	}
	return subject
}
