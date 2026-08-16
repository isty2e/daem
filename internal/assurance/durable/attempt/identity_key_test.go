package attempt

import (
	"testing"
	"time"

	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	"github.com/isty2e/daem/internal/desired/entity"
	lock "github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
	topologyprojection "github.com/isty2e/daem/internal/topology/projection"
)

const testRouteRequestHash = "sha256:1111111111111111111111111111111111111111111111111111111111111111"

func TestAttemptSemanticKeysRejectZeroAndForgedValues(t *testing.T) {
	if err := (DelegateAttemptKey{}).Validate(); err == nil {
		t.Fatal("zero delegate attempt key is valid")
	}
	if err := (HostRouteAttemptKey{}).Validate(); err == nil {
		t.Fatal("zero host-route attempt key is valid")
	}

	delegate := testDelegateAttempt(t, time.Date(2026, 7, 26, 1, 2, 3, 0, time.UTC))
	if err := delegate.SemanticKey().Validate(); err != nil {
		t.Fatalf("canonical delegate attempt key is invalid: %v", err)
	}
	hostRoute := testHostRouteAttempt(t, time.Date(2026, 7, 26, 1, 2, 3, 0, time.UTC))
	if err := hostRoute.SemanticKey().Validate(); err != nil {
		t.Fatalf("canonical host-route attempt key is invalid: %v", err)
	}

	for name, key := range map[string]DelegateAttemptKey{
		"host relation subject": {
			subject: testHostRelationSubject(t, "context7"),
			target:  target.TargetClaudeCode,
			scope:   target.ScopeProject,
		},
		"unknown target": {
			subject: delegate.Subject(),
			target:  "future",
			scope:   target.ScopeProject,
		},
		"unknown scope": {
			subject: delegate.Subject(),
			target:  target.TargetClaudeCode,
			scope:   "ambient",
		},
	} {
		t.Run("delegate "+name, func(t *testing.T) {
			if err := key.Validate(); err == nil {
				t.Fatalf("forged delegate attempt key is valid: %#v", key)
			}
		})
	}

	for name, key := range map[string]HostRouteAttemptKey{
		"projection subject": {
			subject:   delegate.Subject(),
			target:    target.TargetClaudeCode,
			scope:     target.ScopeProject,
			operation: lock.OperationInstall,
			routeID:   "claude-code.plugin-carrier.install",
		},
		"unknown operation": {
			subject:   hostRoute.Subject(),
			target:    target.TargetClaudeCode,
			scope:     target.ScopeProject,
			operation: "inspect",
			routeID:   "claude-code.plugin-carrier.install",
		},
		"empty route id": {
			subject:   hostRoute.Subject(),
			target:    target.TargetClaudeCode,
			scope:     target.ScopeProject,
			operation: lock.OperationInstall,
		},
	} {
		t.Run("host-route "+name, func(t *testing.T) {
			if err := key.Validate(); err == nil {
				t.Fatalf("forged host-route attempt key is valid: %#v", key)
			}
		})
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

func testHostRelationSubject(t *testing.T, name string) topology.SubjectID {
	t.Helper()
	subject, err := topology.NewSubjectID(
		topology.SubjectHostRelation,
		"claude-code.plugin-carrier",
		name,
	)
	if err != nil {
		t.Fatal(err)
	}
	return subject
}

func testDelegateAttempt(t *testing.T, observedAt time.Time) DelegateAttempt {
	t.Helper()
	attempt, err := NewDelegateAttempt(testDelegateAttemptInput(t, observedAt))
	if err != nil {
		t.Fatal(err)
	}
	return attempt
}

func testDelegateAttemptInput(t *testing.T, observedAt time.Time) DelegateAttemptInput {
	t.Helper()
	return DelegateAttemptInput{
		Subject: testProjectionSubject(
			t,
			entity.KindMCPServer,
			"context7",
			"mcp-server.project.claude-code",
		),
		Target:          target.TargetClaudeCode,
		Scope:           target.ScopeProject,
		PlanIdentityKey: "delegate:identity",
		ObservedAt:      observedAt,
		Status:          DelegateStatusSucceeded,
		Reason:          DelegateReasonNone,
		AttemptObserved: true,
		ProcessReason:   DelegateProcessReasonNone,
		Observation:     observerelation.ObservationPresent,
		Postcondition:   observerelation.PostconditionNotObserved,
		Redacted:        true,
	}
}

func testHostRouteAttempt(t *testing.T, observedAt time.Time) HostRouteAttempt {
	t.Helper()
	attempt, err := NewHostRouteAttempt(testHostRouteAttemptInput(t, observedAt))
	if err != nil {
		t.Fatal(err)
	}
	return attempt
}

func testHostRouteAttemptInput(t *testing.T, observedAt time.Time) HostRouteAttemptInput {
	t.Helper()
	return HostRouteAttemptInput{
		Subject:          testHostRelationSubject(t, "context7"),
		Target:           target.TargetClaudeCode,
		Scope:            target.ScopeProject,
		Operation:        lock.OperationInstall,
		RouteID:          "claude-code.plugin-carrier.install",
		RouteRequestHash: testRouteRequestHash,
		ObservedAt:       observedAt,
		ResultClass:      HostRouteResultAttemptedUnverified,
		Reason:           HostRouteReasonObservationUnavailable,
		AttemptObserved:  true,
		Observation:      observerelation.ObservationNotObserved,
		Postcondition:    observerelation.PostconditionUnknown,
	}
}

func TestAttemptModelComparisonsPreserveEveryTieBreaker(t *testing.T) {
	observedAt := time.Date(2026, 7, 26, 1, 2, 3, 0, time.UTC)
	projectionAlpha := testProjectionSubject(t, entity.KindMCPServer, "alpha", "mcp-server.project.claude-code")
	projectionBeta := testProjectionSubject(t, entity.KindMCPServer, "beta", "mcp-server.project.claude-code")
	hostAlpha, err := topology.NewSubjectID(topology.SubjectHostRelation, "claude-code.plugin-carrier", "alpha")
	if err != nil {
		t.Fatal(err)
	}
	hostBeta, err := topology.NewSubjectID(topology.SubjectHostRelation, "claude-code.plugin-carrier", "beta")
	if err != nil {
		t.Fatal(err)
	}

	delegateAttempt := func(
		subject topology.SubjectID,
		selectedTarget target.Target,
		scope target.Scope,
		planIdentity string,
	) DelegateAttempt {
		attempt, err := NewDelegateAttempt(DelegateAttemptInput{
			Subject:         subject,
			Target:          selectedTarget,
			Scope:           scope,
			PlanIdentityKey: planIdentity,
			ObservedAt:      observedAt,
			Status:          DelegateStatusSucceeded,
			Reason:          DelegateReasonNone,
			AttemptObserved: true,
			ProcessReason:   DelegateProcessReasonNone,
			Observation:     observerelation.ObservationNotObserved,
			Postcondition:   observerelation.PostconditionNotObserved,
			Redacted:        true,
		})
		if err != nil {
			t.Fatal(err)
		}
		return attempt
	}
	delegateBase := delegateAttempt(
		projectionAlpha,
		target.TargetClaudeCode,
		target.ScopeProject,
		"plan.a",
	)
	assertModelOrder(
		t, "delegate subject",
		delegateAttempt(projectionAlpha, target.TargetClaudeCode, target.ScopeProject, "plan.a"),
		delegateAttempt(projectionBeta, target.TargetClaudeCode, target.ScopeProject, "plan.a"),
		DelegateAttempt.Compare,
	)
	assertModelOrder(
		t, "delegate target",
		delegateAttempt(projectionAlpha, target.TargetAntigravityCLI, target.ScopeProject, "plan.a"),
		delegateBase,
		DelegateAttempt.Compare,
	)
	assertModelOrder(
		t, "delegate scope",
		delegateAttempt(projectionAlpha, target.TargetClaudeCode, target.ScopeGlobal, "plan.a"),
		delegateBase,
		DelegateAttempt.Compare,
	)
	assertModelOrder(
		t, "delegate plan identity",
		delegateBase,
		delegateAttempt(projectionAlpha, target.TargetClaudeCode, target.ScopeProject, "plan.b"),
		DelegateAttempt.Compare,
	)

	hostRouteAttempt := func(
		subject topology.SubjectID,
		selectedTarget target.Target,
		scope target.Scope,
		operation lock.OperationKind,
		routeID string,
		hash string,
	) HostRouteAttempt {
		attempt, err := NewHostRouteAttempt(HostRouteAttemptInput{
			Subject:          subject,
			Target:           selectedTarget,
			Scope:            scope,
			Operation:        operation,
			RouteID:          routeID,
			RouteRequestHash: hash,
			ObservedAt:       observedAt,
			ResultClass:      HostRouteResultAttemptedUnverified,
			Reason:           HostRouteReasonObservationUnavailable,
			AttemptObserved:  true,
			Observation:      observerelation.ObservationNotObserved,
			Postcondition:    observerelation.PostconditionUnknown,
		})
		if err != nil {
			t.Fatal(err)
		}
		return attempt
	}
	hashA := "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	hashB := "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	hostBase := hostRouteAttempt(
		hostAlpha,
		target.TargetClaudeCode,
		target.ScopeProject,
		lock.OperationInstall,
		"route.a",
		hashA,
	)
	assertModelOrder(
		t, "host-route subject",
		hostBase,
		hostRouteAttempt(hostBeta, target.TargetClaudeCode, target.ScopeProject, lock.OperationInstall, "route.a", hashA),
		HostRouteAttempt.Compare,
	)
	assertModelOrder(
		t, "host-route target",
		hostRouteAttempt(hostAlpha, target.TargetAntigravityCLI, target.ScopeProject, lock.OperationInstall, "route.a", hashA),
		hostBase,
		HostRouteAttempt.Compare,
	)
	assertModelOrder(
		t, "host-route scope",
		hostRouteAttempt(hostAlpha, target.TargetClaudeCode, target.ScopeGlobal, lock.OperationInstall, "route.a", hashA),
		hostBase,
		HostRouteAttempt.Compare,
	)
	assertModelOrder(
		t, "host-route operation",
		hostBase,
		hostRouteAttempt(hostAlpha, target.TargetClaudeCode, target.ScopeProject, lock.OperationRemove, "route.a", hashA),
		HostRouteAttempt.Compare,
	)
	assertModelOrder(
		t, "host-route route id",
		hostBase,
		hostRouteAttempt(hostAlpha, target.TargetClaudeCode, target.ScopeProject, lock.OperationInstall, "route.b", hashA),
		HostRouteAttempt.Compare,
	)
	assertModelOrder(
		t, "host-route request hash",
		hostBase,
		hostRouteAttempt(hostAlpha, target.TargetClaudeCode, target.ScopeProject, lock.OperationInstall, "route.a", hashB),
		HostRouteAttempt.Compare,
	)
}

func assertModelOrder[T any](
	t *testing.T,
	name string,
	left T,
	right T,
	compare func(T, T) int,
) {
	t.Helper()
	t.Run(name, func(t *testing.T) {
		if order := compare(left, right); order >= 0 {
			t.Fatalf("left.Compare(right) = %d, want negative", order)
		}
		if order := compare(right, left); order <= 0 {
			t.Fatalf("right.Compare(left) = %d, want positive", order)
		}
		if order := compare(left, left); order != 0 {
			t.Fatalf("left.Compare(left) = %d, want zero", order)
		}
	})
}
