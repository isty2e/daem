package publication

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/observe"
	"github.com/isty2e/daem/internal/desired/entity"
	journalrecord "github.com/isty2e/daem/internal/effect/journal/record"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/output/ownership"
	"github.com/isty2e/daem/internal/realization"
	"github.com/isty2e/daem/internal/target"
	topologyprojection "github.com/isty2e/daem/internal/topology/projection"
)

const promotionOperationID = "20260625T000000.000000000Z-apply"

func promotionRecordForTest(t *testing.T, manifestPath, namespacePath string, intent ownership.ProvisionalAcquireIntent) []byte {
	t.Helper()
	manifest := captureFixtureProvenance(t, manifestPath)
	global := captureFixtureProvenance(t, namespacePath)
	id, err := entity.New(entity.KindInstructions, "global")
	if err != nil {
		t.Fatal(err)
	}
	subject, err := topologyprojection.Subject(id, "instructions.global.codex")
	if err != nil {
		t.Fatal(err)
	}
	hash := testContentHash("after")
	state, err := durable.NewManagedPathState(subject, []target.Target{target.TargetCodex}, target.ScopeGlobal, intent.Destination(), hash, realization.PathProjectionFile, realization.PathPermissionsExecutableClass, 0)
	if err != nil {
		t.Fatal(err)
	}
	mutation, err := journalrecord.NewManagedPathCreateMutation(subject, state.ConsumerTargets(), state.Scope(), state.Destination(), hash, state.ContentKind(), 0o600, nil)
	if err != nil {
		t.Fatal(err)
	}
	evidence, err := observe.NewManagedPathEvidence(subject, state.Destination(), false, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	values, err := journalrecord.NormalizeCaptureMutations([]journalrecord.ManagedPathMutation{mutation}, nil, []observe.ManagedPathEvidence{evidence})
	if err != nil {
		t.Fatal(err)
	}
	expected := recovery.ExpectedPathState{Existed: true, Kind: recovery.PathKindFile, ContentHash: string(hash), PathMode: recovery.NewPermissionMode(0o600)}
	removalState, err := recovery.NewExpectedRemovalState(expected)
	if err != nil {
		t.Fatal(err)
	}
	demand, err := recovery.NewRemovalDemand(state.Scope(), state.Destination(), []recovery.RemovalState{removalState})
	if err != nil {
		t.Fatal(err)
	}
	demands, err := recovery.NewRemovalDemandSet([]recovery.RemovalDemand{demand})
	if err != nil {
		t.Fatal(err)
	}
	names, err := mutationfs.NewLogicalRemovalNames(".daem-tombstone-00000000000000000000000000000001", ".daem-cleanup-00000000000000000000000000000001")
	if err != nil {
		t.Fatal(err)
	}
	parent, err := recovery.NewRootProvenance(global.PhysicalRoot(), global.ObjectFingerprint(), global.MountFingerprint())
	if err != nil {
		t.Fatal(err)
	}
	namespace, err := recovery.NewExistingParentAuthority(parent, names)
	if err != nil {
		t.Fatal(err)
	}
	removal, err := recovery.NewRemovalIntent(demand, namespace)
	if err != nil {
		t.Fatal(err)
	}
	before, after := durable.EmptySnapshot(), statefileFor(state)
	draft, err := journalrecord.NewOrdinaryDraft(promotionOperationID, time.Date(2026, time.June, 25, 0, 0, 0, 0, time.UTC), before, after, manifest, values, demands)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := draft.BeginEntry(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := draft.SupplyBefore(recovery.BeforePathState{}); err != nil {
		t.Fatal(err)
	}
	physical := filepath.Join(global.PhysicalRoot(), filepath.Base(intent.Path().CandidateKey()))
	if err := draft.SupplyGlobalBinding(journalrecord.NewGlobalBinding(physical, global)); err != nil {
		t.Fatal(err)
	}
	if err := draft.CloseEntries(); err != nil {
		t.Fatal(err)
	}
	if err := draft.SupplyRemovalIntents([]recovery.RemovalIntent{removal}); err != nil {
		t.Fatal(err)
	}
	if err := draft.BeginClaimCoverage(nil, []ownership.ProvisionalAcquireIntent{intent}); err != nil {
		t.Fatal(err)
	}
	if _, present, err := draft.NextClaimAuthority(t.Context()); err != nil || present {
		t.Fatal("provisional capture requested exact ownership", err)
	}
	material, err := draft.Finish()
	if err != nil {
		t.Fatal(err)
	}
	beforeBytes, afterBytes, err := journalrecord.EncodeSnapshots(before, after, testStateCodec())
	if err != nil {
		t.Fatal(err)
	}
	content, err := material.EncodeOrdinary(beforeBytes, afterBytes)
	if err != nil {
		t.Fatal(err)
	}
	return content
}

func captureFixtureProvenance(t *testing.T, path string) rootedpath.AuthorityProvenance {
	t.Helper()
	root, err := rootedpath.CaptureRoot(path)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	authority, err := root.Authority()
	if err != nil {
		t.Fatal(err)
	}
	provenance, err := authority.Provenance()
	if err != nil {
		t.Fatal(err)
	}
	return provenance
}
