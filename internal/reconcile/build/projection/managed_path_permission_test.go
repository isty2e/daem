package projection

import (
	"os"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/observe"
	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization"
	lock "github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/realization/lock/snapshottest"
	"github.com/isty2e/daem/internal/realization/profile"
	"github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
	topologyprojection "github.com/isty2e/daem/internal/topology/projection"
	"github.com/isty2e/daem/test/outputtest"
)

func TestBuildManagedPathDecisionsEnforcesExactPermissionPolicy(t *testing.T) {
	locked, supply, projection, destination, contentHash := managedHookAssetPathLock(t)
	state := managedFileState(
		t,
		projection,
		destination,
		contentHash,
		realization.PathPermissionsExact,
		0o700,
	)

	tests := []struct {
		name           string
		states         []durable.ManagedPathState
		liveMode       os.FileMode
		manageExisting bool
		wantKind       reconcile.ManagedPathDecisionKind
		wantReason     reconcile.ActionReason
		wantDetail     string
	}{
		{
			name: "managed exact mode drift is repaired", states: []durable.ManagedPathState{state}, liveMode: 0o755,
			wantKind: reconcile.ManagedPathReplace, wantReason: reconcile.ReasonFileModeChanged,
		},
		{
			name: "exact current mode is a no-op", states: []durable.ManagedPathState{state}, liveMode: 0o700,
			wantKind: reconcile.ManagedPathNoOp, wantReason: reconcile.ReasonAlreadyCurrent,
		},
		{
			name: "manage-existing refuses same executable class with wrong exact mode", liveMode: 0o755,
			manageExisting: true, wantKind: reconcile.ManagedPathBlocked, wantReason: reconcile.ReasonUnmanagedOutputExists,
			wantDetail: "different file mode",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			evidence, err := observe.NewManagedPathEvidence(
				projection.SubjectID(), destination, true, contentHash, test.liveMode,
			)
			if err != nil {
				t.Fatal(err)
			}
			decisions, err := BuildManagedPathDecisions(ManagedPathInput{
				Locked:                 locked,
				Expectations:           []ManagedPathExpectation{managedPathExpectation(t, projection)},
				SelectedTargets:        planSelectedTargets(t, target.TargetCodex),
				SupplyObservations:     []observe.ExactSupplyObservation{exactSupplyObservation(t, supply, false)},
				States:                 test.states,
				Evidence:               []observe.ManagedPathEvidence{evidence},
				ManageUnmanagedMatches: test.manageExisting,
			})
			if err != nil {
				t.Fatal(err)
			}
			if len(decisions) != 1 || decisions[0].Kind() != test.wantKind || decisions[0].Reason() != test.wantReason {
				t.Fatalf("decision = %#v, want %s/%s", decisions, test.wantKind, test.wantReason)
			}
			if test.wantDetail != "" && !strings.Contains(decisions[0].Detail(), test.wantDetail) {
				t.Fatalf("detail = %q, want fragment %q", decisions[0].Detail(), test.wantDetail)
			}
		})
	}
}

func TestManagedPathExpectationAndModeSelectionRetainRealizationOwnedExactMode(t *testing.T) {
	entityID, err := entity.New(entity.KindExtension, "future-exact")
	if err != nil {
		t.Fatal(err)
	}
	subject, err := topologyprojection.Subject(entityID, "future.project.exact")
	if err != nil {
		t.Fatal(err)
	}
	exactMode, err := realization.NewExactPathPermissionMode(0o640)
	if err != nil {
		t.Fatal(err)
	}
	spec, err := realization.NewManagedPathProjection(realization.ManagedPathProjectionInput{
		PlacementID: "future.project.exact", ConsumerTargets: []target.Target{target.TargetCodex},
		Scope: target.ScopeProject, Destination: outputtest.Parse(t, ".daem/future-exact"), ContentKind: realization.PathProjectionFile,
		PlacementMode: realization.PathProjectionCopy, PermissionPolicy: realization.PathPermissionsExact,
		ExactPermissionMode: exactMode, AdapterContractVersion: "future-exact-v1",
	})
	if err != nil {
		t.Fatal(err)
	}
	projection, err := lock.NewManagedPathSubjectContract(lock.ManagedPathSubjectInput{
		EntityID: entityID, SubjectID: subject, Realization: spec,
		WriteRouteID: "future-exact.write", RemoveRouteID: "managed-path.remove",
	})
	if err != nil {
		t.Fatal(err)
	}
	expectation, err := NewManagedPathExpectation(projection)
	if err != nil {
		t.Fatal(err)
	}
	facts := expectation.decisionInput()
	if facts.DesiredFileMode != 0o640 {
		t.Fatalf("expectation exact mode = %04o, want 0640", facts.DesiredFileMode)
	}
	if got := managedFileDesiredMode(realization.PathPermissionsExact, facts.DesiredFileMode, false); got != 0o640 {
		t.Fatalf("exact desired mode = %04o, want realization-owned 0640", got)
	}
	if got := managedFileDesiredMode(realization.PathPermissionsExecutableClass, 0, false); got != 0o600 {
		t.Fatalf("non-executable publish mode = %04o, want 0600", got)
	}
	if got := managedFileDesiredMode(realization.PathPermissionsExecutableClass, 0, true); got != 0o700 {
		t.Fatalf("executable publish mode = %04o, want 0700", got)
	}
}

func TestReconcileManagedPathDesiredRepairsArbitraryExactModeDrift(t *testing.T) {
	subject := syntheticManagedFileSubject(t, "future-exact")
	contentHash := artifact.HashFileContent([]byte("future\n"))
	state, err := durable.NewManagedPathState(
		subject,
		[]target.Target{target.TargetCodex},
		target.ScopeProject,
		outputtest.Parse(t, ".daem/future-exact"),
		contentHash,
		realization.PathProjectionFile,
		realization.PathPermissionsExact,
		0o640,
	)
	if err != nil {
		t.Fatal(err)
	}
	facts := reconcile.ManagedPathDecisionInput{
		Subject: subject, ConsumerTargets: []target.Target{target.TargetCodex},
		Scope: target.ScopeProject, Destination: outputtest.Parse(t, ".daem/future-exact"), DesiredHash: contentHash,
		ContentKind: realization.PathProjectionFile, PlacementMode: realization.PathProjectionCopy,
		PermissionPolicy: realization.PathPermissionsExact, DesiredFileMode: 0o640,
	}
	for _, test := range []struct {
		name     string
		liveMode os.FileMode
		wantKind reconcile.ManagedPathDecisionKind
	}{
		{name: "drift", liveMode: 0o600, wantKind: reconcile.ManagedPathReplace},
		{name: "converged", liveMode: 0o640, wantKind: reconcile.ManagedPathNoOp},
	} {
		t.Run(test.name, func(t *testing.T) {
			current, err := observe.NewManagedPathEvidence(
				subject,
				outputtest.Parse(t, ".daem/future-exact"),
				true,
				contentHash,
				test.liveMode,
			)
			if err != nil {
				t.Fatal(err)
			}
			decision := reconcileManagedPathDesired(facts, current, state, true, nil, false)
			if decision.Kind() != test.wantKind {
				t.Fatalf("decision kind = %s, want %s", decision.Kind(), test.wantKind)
			}
			if decision.DesiredFileMode() != 0o640 {
				t.Fatalf("desired exact mode = %04o, want 0640", decision.DesiredFileMode())
			}
		})
	}
}

func TestBuildManagedPathDecisionsBlocksStateOnlyExactModeDrift(t *testing.T) {
	_, _, projection, destination, contentHash := managedHookAssetPathLock(t)
	state := managedFileState(
		t,
		projection,
		destination,
		contentHash,
		realization.PathPermissionsExact,
		0o700,
	)
	evidence, err := observe.NewManagedPathEvidence(
		projection.SubjectID(), destination, true, contentHash, 0o755,
	)
	if err != nil {
		t.Fatal(err)
	}

	decisions, err := BuildManagedPathDecisions(ManagedPathInput{
		Locked:          snapshottest.Section(t),
		SelectedTargets: planSelectedTargets(t, target.TargetCodex),
		States:          []durable.ManagedPathState{state},
		Evidence:        []observe.ManagedPathEvidence{evidence},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(decisions) != 1 || decisions[0].Kind() != reconcile.ManagedPathBlocked || decisions[0].Reason() != reconcile.ReasonDriftedOutput {
		t.Fatalf("decisions = %#v, want state-only mode-drift block", decisions)
	}
}

func TestReconcileManagedPathDesiredExecutableClassIgnoresReadWriteBits(t *testing.T) {
	subject := syntheticManagedFileSubject(t, "synthetic-file")
	contentHash := artifact.HashFileContent([]byte("instructions\n"))
	state, err := durable.NewManagedPathState(
		subject,
		[]target.Target{target.TargetCodex},
		target.ScopeProject,
		outputtest.Parse(t, "AGENTS.md"),
		contentHash,
		realization.PathProjectionFile,
		realization.PathPermissionsExecutableClass,
		0,
	)
	if err != nil {
		t.Fatal(err)
	}
	current, err := observe.NewManagedPathEvidence(subject, outputtest.Parse(t, "AGENTS.md"), true, contentHash, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	facts := reconcile.ManagedPathDecisionInput{
		Subject: subject, ConsumerTargets: []target.Target{target.TargetCodex},
		Scope: target.ScopeProject, Destination: outputtest.Parse(t, "AGENTS.md"), DesiredHash: contentHash,
		ContentKind: realization.PathProjectionFile, PlacementMode: realization.PathProjectionCopy,
		PermissionPolicy: realization.PathPermissionsExecutableClass, DesiredFileMode: 0o600,
	}

	decision := reconcileManagedPathDesired(facts, current, state, true, nil, false)
	if decision.Kind() != reconcile.ManagedPathNoOp || decision.Reason() != reconcile.ReasonAlreadyCurrent {
		t.Fatalf("decision = %#v, want executable-class no-op", decision)
	}

	decision = reconcileManagedPathDesired(facts, current, durable.ManagedPathState{}, false, nil, false)
	if strings.Contains(decision.Detail(), "different file mode") {
		t.Fatalf("compatible executable-class mode produced exact-mode diagnostic: %q", decision.Detail())
	}
}

func TestReconcileManagedPathPermissionPolicyTransitionsAreStateOnly(t *testing.T) {
	subject := syntheticManagedFileSubject(t, "policy-transition")
	contentHash := artifact.HashFileContentWithExecutable([]byte("payload\n"), true)

	tests := []struct {
		name          string
		statePolicy   realization.PathPermissionPolicy
		stateMode     os.FileMode
		desiredPolicy realization.PathPermissionPolicy
		desiredMode   os.FileMode
		liveMode      os.FileMode
	}{
		{
			name: "executable class to exact", statePolicy: realization.PathPermissionsExecutableClass,
			desiredPolicy: realization.PathPermissionsExact, desiredMode: 0o700, liveMode: 0o700,
		},
		{
			name: "exact to executable class", statePolicy: realization.PathPermissionsExact, stateMode: 0o700,
			desiredPolicy: realization.PathPermissionsExecutableClass, desiredMode: 0o700, liveMode: 0o755,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			state, err := durable.NewManagedPathState(
				subject, []target.Target{target.TargetCodex}, target.ScopeProject, outputtest.Parse(t, "managed.bin"),
				contentHash, realization.PathProjectionFile, test.statePolicy, test.stateMode,
			)
			if err != nil {
				t.Fatal(err)
			}
			current, err := observe.NewManagedPathEvidence(subject, outputtest.Parse(t, "managed.bin"), true, contentHash, test.liveMode)
			if err != nil {
				t.Fatal(err)
			}
			facts := reconcile.ManagedPathDecisionInput{
				Subject: subject, ConsumerTargets: []target.Target{target.TargetCodex},
				Scope: target.ScopeProject, Destination: outputtest.Parse(t, "managed.bin"), DesiredHash: contentHash,
				ContentKind: realization.PathProjectionFile, PlacementMode: realization.PathProjectionCopy,
				PermissionPolicy: test.desiredPolicy, DesiredFileMode: test.desiredMode, LiveFileMode: test.liveMode,
			}

			decision := reconcileManagedPathDesired(facts, current, state, true, nil, false)
			if decision.Kind() != reconcile.ManagedPathRecord ||
				decision.Reason() != reconcile.ReasonStateStale ||
				decision.Kind().MutatesHost() {
				t.Fatalf("decision = %#v, want state-only policy transition", decision)
			}
		})
	}
}

func TestBuildManagedPathDecisionsPartialRemovalPreservesExactModeAuthority(t *testing.T) {
	subject := syntheticManagedFileSubject(t, "shared-exact")
	contentHash := artifact.HashFileContentWithExecutable([]byte("payload\n"), true)
	state, err := durable.NewManagedPathState(
		subject,
		[]target.Target{target.TargetClaudeCode, target.TargetCodex},
		target.ScopeProject,
		outputtest.Parse(t, "managed.bin"),
		contentHash,
		realization.PathProjectionFile,
		realization.PathPermissionsExact,
		0o700,
	)
	if err != nil {
		t.Fatal(err)
	}
	evidence, err := observe.NewManagedPathEvidence(subject, outputtest.Parse(t, "managed.bin"), true, contentHash, 0o700)
	if err != nil {
		t.Fatal(err)
	}

	decisions, err := BuildManagedPathDecisions(ManagedPathInput{
		Locked:          snapshottest.Section(t),
		SelectedTargets: planSelectedTargets(t, target.TargetCodex),
		States:          []durable.ManagedPathState{state},
		Evidence:        []observe.ManagedPathEvidence{evidence},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(decisions) != 1 || decisions[0].Kind() != reconcile.ManagedPathRecord ||
		decisions[0].Reason() != reconcile.ReasonStateStale || decisions[0].DesiredFileMode() != 0o700 ||
		len(decisions[0].ConsumerTargets()) != 1 || decisions[0].ConsumerTargets()[0] != target.TargetClaudeCode {
		t.Fatalf("decisions = %#v, want exact-mode state record for remaining consumer", decisions)
	}
}

func syntheticManagedFileSubject(t *testing.T, name string) topology.SubjectID {
	t.Helper()
	entityID, err := entity.New(entity.KindExtension, name)
	if err != nil {
		t.Fatal(err)
	}
	subject, err := topologyprojection.Subject(entityID, "test.project.managed-file")
	if err != nil {
		t.Fatal(err)
	}
	return subject
}

func managedHookAssetPathLock(
	t *testing.T,
) (
	lock.LockedSection,
	lock.LockedSubjectContract,
	lock.LockedSubjectContract,
	output.Destination,
	artifact.ContentHash,
) {
	t.Helper()
	content := []byte("#!/bin/sh\nexit 0\n")
	hash := artifact.HashFileContentWithExecutable(content, true)
	fileUse, err := lock.NewExactFileUse(target.ScopeProject, true)
	if err != nil {
		t.Fatal(err)
	}
	supply := snapshottest.ExactSupplyContract(t, snapshottest.ExactSupplyInput{
		Kind: entity.KindHookAsset, Name: "guard", SourceID: "local:hooks/guard?mode=vendor",
		ArtifactKind: artifact.ArtifactKindFile, ContentHash: hash, ExactFileUse: &fileUse,
	})
	placement, err := profile.HookAssetPlacementFor(target.ScopeProject, []target.Target{target.TargetCodex})
	if err != nil {
		t.Fatal(err)
	}
	writeRoute, err := profile.HookAssetOperationRoute(placement, profile.OperationWrite)
	if err != nil {
		t.Fatal(err)
	}
	removeRoute, err := profile.HookAssetOperationRoute(placement, profile.OperationRemove)
	if err != nil {
		t.Fatal(err)
	}
	spec, err := placement.Realize("guard", hash, true, writeRoute)
	if err != nil {
		t.Fatal(err)
	}
	entityID, err := entity.New(entity.KindHookAsset, "guard")
	if err != nil {
		t.Fatal(err)
	}
	subject, err := topologyprojection.Subject(entityID, placement.ID())
	if err != nil {
		t.Fatal(err)
	}
	projection, err := lock.NewManagedPathSubjectContract(lock.ManagedPathSubjectInput{
		EntityID: entityID, SubjectID: subject, Realization: spec,
		WriteRouteID: writeRoute.RouteID(), RemoveRouteID: removeRoute.RouteID(),
	})
	if err != nil {
		t.Fatal(err)
	}
	locked := snapshottest.Section(t, supply, projection)
	path, _ := spec.ManagedPathProjection()
	return locked, supply, projection, path.Destination(), hash
}

func managedFileState(
	t *testing.T,
	projection lock.LockedSubjectContract,
	destination output.Destination,
	contentHash artifact.ContentHash,
	policy realization.PathPermissionPolicy,
	fileMode os.FileMode,
) durable.ManagedPathState {
	t.Helper()
	state, err := durable.NewManagedPathState(
		projection.SubjectID(),
		[]target.Target{target.TargetCodex},
		target.ScopeProject,
		destination,
		contentHash,
		realization.PathProjectionFile,
		policy,
		fileMode,
	)
	if err != nil {
		t.Fatal(err)
	}
	return state
}
