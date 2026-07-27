package durable

import (
	"reflect"
	"strings"
	"testing"
	"time"

	durableattempt "github.com/isty2e/daem/internal/assurance/durable/attempt"
	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization"
	"github.com/isty2e/daem/internal/realization/aggregate"
	hookcodec "github.com/isty2e/daem/internal/realization/aggregate/codec/hook"
	commandhook "github.com/isty2e/daem/internal/realization/aggregate/hook"
	lock "github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
	topologyprojection "github.com/isty2e/daem/internal/topology/projection"
	"github.com/isty2e/daem/test/outputtest"
)

func TestManagedPathStateCanonicalizesConsumersAndDefendsCopies(t *testing.T) {
	state := testManagedPath(t, "oracle", ".agents/skills/oracle")
	wantTargets := []target.Target{target.TargetAntigravityCLI, target.TargetCodex}
	if !reflect.DeepEqual(state.ConsumerTargets(), wantTargets) {
		t.Fatalf("consumer targets = %#v, want %#v", state.ConsumerTargets(), wantTargets)
	}

	mutated := state.ConsumerTargets()
	mutated[0] = target.TargetPi
	if reflect.DeepEqual(mutated, state.ConsumerTargets()) {
		t.Fatal("managed path state exposed mutable consumer targets")
	}
}

func TestManagedPathStateRejectsInvalidAndForgedConsumerSets(t *testing.T) {
	subject := testProjectionSubject(t, entity.KindSkill, "oracle", "skill.project.agents")
	_, err := NewManagedPathState(
		subject,
		[]target.Target{target.TargetCodex, "future"},
		target.ScopeProject,
		outputtest.Parse(t, ".agents/skills/oracle"),
		artifact.HashFileContent([]byte("oracle")),
		realization.PathProjectionDirectory,
		realization.PathPermissionsNone,
		0,
	)
	if err == nil || !strings.Contains(err.Error(), `target[1]: unknown target "future"`) {
		t.Fatalf("invalid consumer error = %v", err)
	}

	state := testManagedPath(t, "oracle", ".agents/skills/oracle")
	state.consumerTargets = []target.Target{target.TargetCodex, target.TargetAntigravityCLI}
	if err := state.validate(); err == nil || !strings.Contains(err.Error(), "consumer targets are not canonical") {
		t.Fatalf("forged consumer order error = %v", err)
	}
}

func TestManagedPathStateRejectsNonEntityProjectionAndInvalidPermissionState(t *testing.T) {
	nonEntity, err := topology.NewSubjectID(topology.SubjectProjection, "test.projection", "oracle")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewManagedPathState(
		nonEntity,
		[]target.Target{target.TargetCodex},
		target.ScopeProject,
		outputtest.Parse(t, ".agents/skills/oracle"),
		artifact.HashFileContent([]byte("oracle")),
		realization.PathProjectionDirectory,
		realization.PathPermissionsNone,
		0,
	); err == nil || !strings.Contains(err.Error(), "entity-backed") {
		t.Fatalf("non-entity error = %v", err)
	}

	subject := testProjectionSubject(t, entity.KindSkill, "oracle", "skill.project.agents")
	if _, err := NewManagedPathState(
		subject,
		[]target.Target{target.TargetCodex},
		target.ScopeProject,
		outputtest.Parse(t, ".agents/skills/oracle"),
		"sha256:short",
		realization.PathProjectionDirectory,
		realization.PathPermissionsNone,
		0,
	); err == nil || !strings.Contains(err.Error(), "lowercase SHA-256") {
		t.Fatalf("malformed hash error = %v", err)
	}
	if _, err := NewManagedPathState(
		subject,
		[]target.Target{target.TargetCodex},
		target.ScopeProject,
		outputtest.Parse(t, ".agents/skills/oracle"),
		artifact.HashFileContent([]byte("oracle")),
		realization.PathProjectionDirectory,
		realization.PathPermissionsExact,
		0,
	); err == nil {
		t.Fatal("exact permission baseline accepted an empty mode")
	}
	for _, test := range []struct {
		scope       target.Scope
		destination output.Destination
	}{
		{scope: target.ScopeProject},
		{scope: target.ScopeProject, destination: outputtest.Parse(t, "~/agents/skills/oracle")},
		{scope: target.ScopeGlobal, destination: outputtest.Parse(t, ".agents/skills/oracle")},
	} {
		if _, err := NewManagedPathState(
			subject,
			[]target.Target{target.TargetCodex},
			test.scope,
			test.destination,
			artifact.HashFileContent([]byte("oracle")),
			realization.PathProjectionDirectory,
			realization.PathPermissionsNone,
			0,
		); err == nil {
			t.Fatalf("managed path state accepted scope %q destination %q", test.scope, test.destination)
		}
	}
}

func TestSnapshotRejectsDuplicateManagedOwnership(t *testing.T) {
	path := testManagedPath(t, "oracle", ".agents/skills/oracle")

	if _, err := NewSnapshot(SnapshotInput{
		ManagedPaths: []ManagedPathState{path, path},
	}); err == nil || !strings.Contains(err.Error(), "already belongs") {
		t.Fatalf("duplicate path error = %v", err)
	}
}

func TestSnapshotRejectsOverlappingManagedOwnership(t *testing.T) {
	aggregateState := testManagedAggregate(t, "guard", "echo guard")
	aggregateRoot := aggregateState.Contribution().AggregateRoot()
	pathState := testManagedPath(t, "same-root", aggregateRoot.String())
	if _, err := NewSnapshot(SnapshotInput{
		ManagedPaths:      []ManagedPathState{pathState},
		ManagedAggregates: []ManagedAggregateState{aggregateState},
	}); err == nil || !strings.Contains(err.Error(), "overlaps managed path ownership") {
		t.Fatalf("overlapping ownership error = %v", err)
	}
}

func TestDurableFactConstructorsRejectCrossKindSubjects(t *testing.T) {
	locked := testLockedHostRelation(t)
	hostRelationSubject := locked.SubjectID()
	projectionSubject := testProjectionSubject(
		t,
		entity.KindMCPServer,
		"context7",
		"mcp-server.project.claude-code",
	)

	aggregateState := testManagedAggregate(t, "guard", "echo guard")
	if _, err := NewManagedAggregateState(
		hostRelationSubject,
		aggregateState.Contribution(),
	); err == nil || !strings.Contains(err.Error(), "projection subject") {
		t.Fatalf("managed aggregate cross-kind error = %v", err)
	}

	delegateInput := testDelegateAttemptInput(t, time.Now())
	delegateInput.Subject = hostRelationSubject
	if _, err := durableattempt.NewDelegateAttempt(delegateInput); err == nil ||
		!strings.Contains(err.Error(), "projection subject") {
		t.Fatalf("delegate cross-kind error = %v", err)
	}

	hostRouteInput := testHostRouteAttemptInput(t, time.Now())
	hostRouteInput.Subject = projectionSubject
	if _, err := durableattempt.NewHostRouteAttempt(hostRouteInput); err == nil ||
		!strings.Contains(err.Error(), "host_relation subject") {
		t.Fatalf("host route cross-kind error = %v", err)
	}
}

func TestSnapshotAllowsOnlyCompatibleSharedAggregateContributions(t *testing.T) {
	left := testManagedAggregate(t, "guard", "echo guard")
	right := testManagedAggregate(t, "audit", "echo audit")
	snapshot, err := NewSnapshot(SnapshotInput{
		ManagedAggregates: []ManagedAggregateState{right, left},
	})
	if err != nil {
		t.Fatal(err)
	}
	got := snapshot.ManagedAggregates()
	if len(got) != 2 || got[0].Subject().String() >= got[1].Subject().String() {
		t.Fatalf("managed aggregates are not canonical: %#v", got)
	}

	mutated := snapshot.ManagedAggregates()
	mutated[0] = ManagedAggregateState{}
	if snapshot.ManagedAggregates()[0].Subject().IsZero() {
		t.Fatal("snapshot exposed mutable aggregate state")
	}
}

func TestSnapshotRejectsDuplicateHistoryKeysEvenWhenTimestampsDiffer(t *testing.T) {
	first := testDelegateAttempt(t, time.Date(2026, 7, 18, 1, 2, 3, 0, time.UTC))
	second := testDelegateAttempt(t, time.Date(2026, 7, 18, 1, 2, 4, 0, time.UTC))
	if _, err := NewSnapshot(SnapshotInput{
		DelegateAttempts: []durableattempt.DelegateAttempt{first, second},
	}); err == nil || !strings.Contains(err.Error(), "duplicate semantic key") {
		t.Fatalf("duplicate delegate history error = %v", err)
	}

	hostFirst := testHostRouteAttempt(t, time.Date(2026, 7, 18, 1, 2, 3, 0, time.UTC))
	hostSecondInput := testHostRouteAttemptInput(t, time.Date(2026, 7, 18, 1, 2, 4, 0, time.UTC))
	hostSecondInput.RouteRequestHash = "sha256:2222222222222222222222222222222222222222222222222222222222222222"
	hostSecond, err := durableattempt.NewHostRouteAttempt(hostSecondInput)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewSnapshot(SnapshotInput{
		HostRouteAttempts: []durableattempt.HostRouteAttempt{hostFirst, hostSecond},
	}); err == nil || !strings.Contains(err.Error(), "duplicate semantic key") {
		t.Fatalf("duplicate host-route history error = %v", err)
	}
}

func TestDelegateAttemptIdentityMatchIsRelevanceOnly(t *testing.T) {
	attempt := testDelegateAttempt(t, time.Date(2026, 7, 18, 1, 2, 3, 0, time.UTC))
	if !attempt.MatchesPlanIdentity("delegate:identity") {
		t.Fatal("matching plan identity was not recognized")
	}
	if attempt.MatchesPlanIdentity("delegate:changed") {
		t.Fatal("changed plan identity was treated as relevant")
	}
}

func TestHostRouteAttemptRejectsSkipLikeAndContradictoryShapesByConstruction(t *testing.T) {
	input := testHostRouteAttemptInput(t, time.Now())
	input.ResultClass = durableattempt.HostRouteResultAttemptedObservedPresent
	input.Reason = durableattempt.HostRouteReasonWorkDirAuthority
	input.AttemptReason = durableattempt.HostRouteAttemptReasonWorkDirAuthority
	if _, err := durableattempt.NewHostRouteAttempt(input); err == nil || !strings.Contains(err.Error(), "must be failed") {
		t.Fatalf("workdir result error = %v", err)
	}

	input = testHostRouteAttemptInput(t, time.Now())
	input.ResultClass = "installed"
	if _, err := durableattempt.NewHostRouteAttempt(input); err == nil || !strings.Contains(err.Error(), "unsupported") {
		t.Fatalf("invented convergence class error = %v", err)
	}

	input = testHostRouteAttemptInput(t, time.Now())
	input.Reason = "probably_installed"
	if _, err := durableattempt.NewHostRouteAttempt(input); err == nil || !strings.Contains(err.Error(), "unsupported") {
		t.Fatalf("invented result reason error = %v", err)
	}
}

func TestSnapshotWithFamilyPreservesUnrelatedFactsAndCanonicalEquality(t *testing.T) {
	path := testManagedPath(t, "oracle", ".agents/skills/oracle")
	delegate := testDelegateAttempt(t, time.Date(2026, 7, 18, 1, 2, 3, 0, time.UTC))
	snapshot, err := NewSnapshot(SnapshotInput{
		ManagedPaths:     []ManagedPathState{path},
		DelegateAttempts: []durableattempt.DelegateAttempt{delegate},
	})
	if err != nil {
		t.Fatal(err)
	}

	next, err := snapshot.WithManagedPaths(nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(next.ManagedPaths()) != 0 || len(next.DelegateAttempts()) != 1 {
		t.Fatalf("family replacement lost unrelated facts: %#v", next)
	}
	reconstructed, err := NewSnapshot(SnapshotInput{
		DelegateAttempts: next.DelegateAttempts(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !next.Equal(reconstructed) {
		t.Fatal("canonical snapshot equality differs after reconstruction")
	}
}

func testManagedPath(t *testing.T, name string, destination string) ManagedPathState {
	t.Helper()
	subject := testProjectionSubject(t, entity.KindSkill, name, "skill.project.agents")
	state, err := NewManagedPathState(
		subject,
		[]target.Target{target.TargetCodex, target.TargetAntigravityCLI, target.TargetCodex},
		target.ScopeProject,
		outputtest.Parse(t, destination),
		artifact.HashFileContent([]byte(name)),
		realization.PathProjectionDirectory,
		realization.PathPermissionsNone,
		0,
	)
	if err != nil {
		t.Fatal(err)
	}
	return state
}

func testManagedAggregate(t *testing.T, name string, command string) ManagedAggregateState {
	t.Helper()
	placement, ok := aggregate.HookPlacementFor(target.TargetCodex, target.ScopeProject)
	if !ok {
		t.Fatal("Codex project Hook placement is missing")
	}
	canonical, err := hookcodec.CanonicalHookContribution(commandhook.ContributionInput{
		Event: "Stop", Type: "command", Command: command,
	})
	if err != nil {
		t.Fatal(err)
	}
	contribution, err := placement.Contribution(canonical)
	if err != nil {
		t.Fatal(err)
	}
	subject := testProjectionSubject(t, entity.KindHook, name, string(placement.ID()))
	state, err := NewManagedAggregateState(subject, contribution)
	if err != nil {
		t.Fatal(err)
	}
	return state
}

func testDelegateAttempt(t *testing.T, observedAt time.Time) durableattempt.DelegateAttempt {
	t.Helper()
	attempt, err := durableattempt.NewDelegateAttempt(testDelegateAttemptInput(t, observedAt))
	if err != nil {
		t.Fatal(err)
	}
	return attempt
}

func testDelegateAttemptInput(t *testing.T, observedAt time.Time) durableattempt.DelegateAttemptInput {
	t.Helper()
	subject := testProjectionSubject(t, entity.KindMCPServer, "context7", "mcp-server.project.claude-code")
	return durableattempt.DelegateAttemptInput{
		Subject:         subject,
		Target:          target.TargetClaudeCode,
		Scope:           target.ScopeProject,
		PlanIdentityKey: "delegate:identity",
		ObservedAt:      observedAt,
		Status:          durableattempt.DelegateStatusSucceeded,
		Reason:          durableattempt.DelegateReasonNone,
		Observation:     observerelation.ObservationPresent,
		Postcondition:   observerelation.PostconditionNotObserved,
		Redacted:        true,
	}
}

func testHostRouteAttempt(t *testing.T, observedAt time.Time) durableattempt.HostRouteAttempt {
	t.Helper()
	attempt, err := durableattempt.NewHostRouteAttempt(testHostRouteAttemptInput(t, observedAt))
	if err != nil {
		t.Fatal(err)
	}
	return attempt
}

func testHostRouteAttemptInput(t *testing.T, observedAt time.Time) durableattempt.HostRouteAttemptInput {
	t.Helper()
	subject, err := topology.NewSubjectID(
		topology.SubjectHostRelation,
		"claude-code.plugin-carrier",
		"context7",
	)
	if err != nil {
		t.Fatal(err)
	}
	return durableattempt.HostRouteAttemptInput{
		Subject:          subject,
		Target:           target.TargetClaudeCode,
		Scope:            target.ScopeProject,
		Operation:        lock.OperationInstall,
		RouteID:          "claude-code.plugin-carrier.install",
		RouteRequestHash: testRouteRequestHash,
		ObservedAt:       observedAt,
		ResultClass:      durableattempt.HostRouteResultAttemptedUnverified,
		Reason:           durableattempt.HostRouteReasonObservationUnavailable,
		AttemptObserved:  true,
		Observation:      observerelation.ObservationNotObserved,
		Postcondition:    observerelation.PostconditionUnknown,
	}
}

func testProjectionSubject(
	t *testing.T,
	kind entity.Kind,
	name string,
	namespace string,
) topology.SubjectID {
	t.Helper()
	id, err := entity.New(kind, name)
	if err != nil {
		t.Fatal(err)
	}
	subject, err := topologyprojection.Subject(id, namespace)
	if err != nil {
		t.Fatal(err)
	}
	return subject
}
