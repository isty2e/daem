package reconcile_test

import (
	"reflect"
	"strings"
	"testing"

	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/delegate"
	"github.com/isty2e/daem/internal/realization/lock/snapshottest"
	reconcile "github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
	"github.com/isty2e/daem/test/outputtest"
)

func TestResultOwnsAllDecisionFamiliesAndDefensiveCopies(t *testing.T) {
	managed := resultManagedPath(t, "oracle", outputtest.Parse(t, "skills/oracle"))
	aggregateDecision, projectionSubject := resultAggregate(t, "context7")
	relationSubject := mustSubject(t, "context7-plugin", "managed/context7-plugin")
	relation := mustPlan(t, relationSubject, correlationFor(t, relationSubject, observerelation.InventorySpec{
		Availability: observerelation.InventorySupported,
		Freshness:    observerelation.EvidenceFresh,
	}), blockedAdmission(t))
	delegateAction := resultDelegate(t, projectionSubject, reconcile.DelegateScheduled)

	managedInput := []reconcile.ManagedPathDecision{managed}
	aggregateInput := []reconcile.AggregateDecision{aggregateDecision}
	relationInput := []reconcile.RelationAction{relation}
	delegateInput := []reconcile.DelegateAction{delegateAction}
	result, err := reconcile.NewResult(reconcile.ResultInput{
		Context:      reconcile.ContextApply,
		ManagedPaths: managedInput,
		Aggregates:   aggregateInput,
		Relations:    relationInput,
		Delegates:    delegateInput,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.DecisionCount() != 4 || result.ProjectionDecisionCount() != 2 {
		t.Fatalf("decision counts = %d total, %d projections", result.DecisionCount(), result.ProjectionDecisionCount())
	}

	managedInput[0] = reconcile.ManagedPathDecision{}
	aggregateInput[0] = reconcile.AggregateDecision{}
	relationInput[0] = reconcile.RelationAction{}
	delegateInput[0] = reconcile.DelegateAction{}
	returnedManaged := result.ManagedPaths()
	returnedAggregates := result.Aggregates()
	returnedRelations := result.Relations()
	returnedDelegates := result.Delegates()
	returnedManaged[0] = reconcile.ManagedPathDecision{}
	returnedAggregates[0] = reconcile.AggregateDecision{}
	returnedRelations[0] = reconcile.RelationAction{}
	returnedDelegates[0] = reconcile.DelegateAction{}

	if result.ManagedPaths()[0].Subject() != managed.Subject() ||
		result.Aggregates()[0].Subjects()[0] != projectionSubject ||
		result.Relations()[0].Subject() != relation.Subject() ||
		result.Delegates()[0].Subject() != delegateAction.Subject() {
		t.Fatal("Result leaked a mutable family slice")
	}

	dependencies := result.Delegates()[0].Dependencies()
	dependencies[0] = reconcile.DelegateDependency{}
	args := result.Delegates()[0].Plan().Command().Args()
	args[0] = "mutated"
	if result.Delegates()[0].Dependencies()[0].Subject != projectionSubject ||
		result.Delegates()[0].Plan().Command().Args()[0] == "mutated" {
		t.Fatal("Result leaked mutable delegate internals")
	}
}

func TestResultRejectsDuplicateAndCrossFamilyIdentities(t *testing.T) {
	managed := resultManagedPath(t, "context7", outputtest.Parse(t, "skills/context7"))
	aggregateDecision, aggregateSubject := resultAggregate(t, "context7")
	relationSubject := mustSubject(t, "context7-plugin", "managed/context7-plugin")
	relation := mustPlan(t, relationSubject, correlationFor(t, relationSubject, observerelation.InventorySpec{
		Availability: observerelation.InventorySupported,
		Freshness:    observerelation.EvidenceFresh,
	}), blockedAdmission(t))
	delegateAction := resultDelegate(t, aggregateSubject, reconcile.DelegateScheduled)

	tests := []struct {
		name  string
		input reconcile.ResultInput
		want  string
	}{
		{name: "managed path", input: reconcile.ResultInput{Context: reconcile.ContextInspect, ManagedPaths: []reconcile.ManagedPathDecision{managed, managed}}, want: "duplicate managed path"},
		{name: "aggregate", input: reconcile.ResultInput{Context: reconcile.ContextInspect, Aggregates: []reconcile.AggregateDecision{aggregateDecision, aggregateDecision}}, want: "duplicate aggregate"},
		{name: "relation", input: reconcile.ResultInput{Context: reconcile.ContextInspect, Relations: []reconcile.RelationAction{relation, relation}}, want: "duplicate relation"},
		{name: "delegate", input: reconcile.ResultInput{Context: reconcile.ContextApply, Aggregates: []reconcile.AggregateDecision{aggregateDecision}, Delegates: []reconcile.DelegateAction{delegateAction, delegateAction}}, want: "duplicate delegate"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := reconcile.NewResult(test.input)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("NewResult error = %v, want %q", err, test.want)
			}
		})
	}

	conflictingManaged := resultManagedPathForSubject(t, aggregateSubject, outputtest.Parse(t, "skills/context7"))
	_, err := reconcile.NewResult(reconcile.ResultInput{
		Context:      reconcile.ContextInspect,
		ManagedPaths: []reconcile.ManagedPathDecision{conflictingManaged},
		Aggregates:   []reconcile.AggregateDecision{aggregateDecision},
	})
	if err == nil || !strings.Contains(err.Error(), "conflicting projection decisions") {
		t.Fatalf("cross-family conflict error = %v", err)
	}
}

func TestResultRejectsContextAndDependencyContradictions(t *testing.T) {
	aggregateDecision, projectionSubject := resultAggregate(t, "context7")
	scheduled := resultDelegate(t, projectionSubject, reconcile.DelegateScheduled)
	skipped := resultDelegate(t, projectionSubject, reconcile.DelegateSkipped)

	tests := []struct {
		name  string
		input reconcile.ResultInput
		want  string
	}{
		{name: "inspect delegate", input: reconcile.ResultInput{Context: reconcile.ContextInspect, Aggregates: []reconcile.AggregateDecision{aggregateDecision}, Delegates: []reconcile.DelegateAction{scheduled}}, want: "inspect reconciliation result"},
		{name: "dry run scheduled", input: reconcile.ResultInput{Context: reconcile.ContextDryRun, Aggregates: []reconcile.AggregateDecision{aggregateDecision}, Delegates: []reconcile.DelegateAction{scheduled}}, want: "must be skipped"},
		{name: "apply skipped", input: reconcile.ResultInput{Context: reconcile.ContextApply, Aggregates: []reconcile.AggregateDecision{aggregateDecision}, Delegates: []reconcile.DelegateAction{skipped}}, want: "must be scheduled or blocked"},
		{name: "missing projection", input: reconcile.ResultInput{Context: reconcile.ContextApply, Delegates: []reconcile.DelegateAction{scheduled}}, want: "references missing projection"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := reconcile.NewResult(test.input)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("NewResult error = %v, want %q", err, test.want)
			}
		})
	}

	plan := resultDelegatePlan(t)
	_, err := reconcile.NewDelegateAction(reconcile.DelegateActionInput{
		Subject: projectionSubject, Target: target.TargetClaudeCode, Scope: target.ScopeProject,
		Plan: plan, Disposition: reconcile.DelegateScheduled,
		Dependencies: []reconcile.DelegateDependency{{Kind: "delegate", Subject: projectionSubject}},
	})
	if err == nil || !strings.Contains(err.Error(), "kind \"delegate\" is unsupported") {
		t.Fatalf("back-edge dependency error = %v", err)
	}
}

func TestResultRejectsContradictoryDelegateDependencyState(t *testing.T) {
	projection := resultManagedPath(t, "blocked", outputtest.Parse(t, "skills/blocked"))
	blockedProjection := resultBlockedManagedPathForSubject(t, projection.Subject(), outputtest.Parse(t, "skills/blocked"))

	scheduled := resultDelegate(t, projection.Subject(), reconcile.DelegateScheduled)
	_, err := reconcile.NewResult(reconcile.ResultInput{
		Context:      reconcile.ContextApply,
		ManagedPaths: []reconcile.ManagedPathDecision{blockedProjection},
		Delegates:    []reconcile.DelegateAction{scheduled},
	})
	if err == nil || !strings.Contains(err.Error(), "no precondition-blocked risk") {
		t.Fatalf("scheduled blocked-dependency error = %v", err)
	}

	wrongRisk := resultDelegateWithRisks(t, projection.Subject(), reconcile.DelegateBlocked, []reconcile.DelegateRisk{{
		Code: reconcile.DelegateRiskMissingRunner, Severity: reconcile.DelegateRiskBlock, Subject: "node",
	}})
	_, err = reconcile.NewResult(reconcile.ResultInput{
		Context:      reconcile.ContextApply,
		ManagedPaths: []reconcile.ManagedPathDecision{blockedProjection},
		Delegates:    []reconcile.DelegateAction{wrongRisk},
	})
	if err == nil || !strings.Contains(err.Error(), "no precondition-blocked risk") {
		t.Fatalf("mismatched blocked-dependency risk error = %v", err)
	}

	riskSubject := string(reconcile.DelegateDependencyProjection) + ":" + projection.Subject().String()
	blocked := resultDelegateWithRisks(t, projection.Subject(), reconcile.DelegateBlocked, []reconcile.DelegateRisk{{
		Code: reconcile.DelegateRiskPreconditionBlocked, Severity: reconcile.DelegateRiskBlock, Subject: riskSubject,
	}})
	if _, err := reconcile.NewResult(reconcile.ResultInput{
		Context:      reconcile.ContextApply,
		ManagedPaths: []reconcile.ManagedPathDecision{blockedProjection},
		Delegates:    []reconcile.DelegateAction{blocked},
	}); err != nil {
		t.Fatalf("coherent blocked dependency: %v", err)
	}
}

func TestResultCanonicalizesFamilyOrder(t *testing.T) {
	managedZ := resultManagedPath(t, "zeta", outputtest.Parse(t, "skills/zeta"))
	managedA := resultManagedPath(t, "alpha", outputtest.Parse(t, "skills/alpha"))
	aggregateGlobal, _ := resultAggregateAt(t, aggregate.MCPPlacementClaudeGlobal, "zeta")
	aggregateProject, _ := resultAggregateAt(t, aggregate.MCPPlacementClaudeProject, "alpha")

	result, err := reconcile.NewResult(reconcile.ResultInput{
		Context:      reconcile.ContextInspect,
		ManagedPaths: []reconcile.ManagedPathDecision{managedZ, managedA},
		Aggregates:   []reconcile.AggregateDecision{aggregateProject, aggregateGlobal},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := []topology.SubjectID{result.ManagedPaths()[0].Subject(), result.ManagedPaths()[1].Subject()}; !reflect.DeepEqual(got, []topology.SubjectID{managedA.Subject(), managedZ.Subject()}) {
		t.Fatalf("managed path order = %v", got)
	}
	if got := []output.Destination{result.Aggregates()[0].DocumentAddress().AggregateRoot(), result.Aggregates()[1].DocumentAddress().AggregateRoot()}; !reflect.DeepEqual(got, []output.Destination{aggregateGlobal.DocumentAddress().AggregateRoot(), aggregateProject.DocumentAddress().AggregateRoot()}) {
		t.Fatalf("aggregate order = %v", got)
	}
}

func TestResultPreservesRelationAndDelegatePlannerOrder(t *testing.T) {
	codexGlobal, codexGlobalSubject := resultAggregateAt(t, aggregate.MCPPlacementCodexGlobal, "zeta")
	codexProject, codexProjectSubject := resultAggregateAt(t, aggregate.MCPPlacementCodexProject, "alpha")
	claudeGlobal, claudeGlobalSubject := resultAggregateAt(t, aggregate.MCPPlacementClaudeGlobal, "alpha")

	result, err := reconcile.NewResult(reconcile.ResultInput{
		Context:    reconcile.ContextApply,
		Aggregates: []reconcile.AggregateDecision{claudeGlobal, codexProject, codexGlobal},
		Relations: []reconcile.RelationAction{
			resultRelationAt(t, "claude-project", target.TargetClaudeCode, target.ScopeProject),
			resultRelationAt(t, "codex-global", target.TargetCodex, target.ScopeGlobal),
			resultRelationAt(t, "claude-global", target.TargetClaudeCode, target.ScopeGlobal),
		},
		Delegates: []reconcile.DelegateAction{
			resultDelegateAt(t, claudeGlobalSubject, target.TargetClaudeCode, target.ScopeGlobal),
			resultDelegateAt(t, codexProjectSubject, target.TargetCodex, target.ScopeProject),
			resultDelegateAt(t, codexGlobalSubject, target.TargetCodex, target.ScopeGlobal),
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	if got := relationTargetScopes(result.Relations()); !reflect.DeepEqual(got, []string{
		"claude-code/global",
		"claude-code/project",
		"codex/global",
	}) {
		t.Fatalf("relation order = %v", got)
	}
	if got := delegateTargetScopes(result.Delegates()); !reflect.DeepEqual(got, []string{
		"codex/global",
		"codex/project",
		"claude-code/global",
	}) {
		t.Fatalf("delegate order = %v", got)
	}
}

func relationTargetScopes(actions []reconcile.RelationAction) []string {
	result := make([]string, 0, len(actions))
	for _, action := range actions {
		result = append(result, string(action.Target())+"/"+string(action.Scope()))
	}
	return result
}

func delegateTargetScopes(actions []reconcile.DelegateAction) []string {
	result := make([]string, 0, len(actions))
	for _, action := range actions {
		result = append(result, string(action.Target())+"/"+string(action.Scope()))
	}
	return result
}

func resultManagedPath(t *testing.T, name string, destination output.Destination) reconcile.ManagedPathDecision {
	t.Helper()
	subject, err := topology.NewSubjectID(topology.SubjectProjection, "test.skill", name)
	if err != nil {
		t.Fatal(err)
	}
	return resultManagedPathForSubject(t, subject, destination)
}

func resultManagedPathForSubject(t *testing.T, subject topology.SubjectID, destination output.Destination) reconcile.ManagedPathDecision {
	t.Helper()
	decision, err := reconcile.NewManagedPathDecision(reconcile.ManagedPathDecisionInput{
		Kind: reconcile.ManagedPathNoOp, Subject: subject,
		ConsumerTargets: []target.Target{target.TargetCodex}, Scope: target.ScopeProject,
		Destination: destination, ContentKind: realization.PathProjectionDirectory,
		PlacementMode: realization.PathProjectionCopy, PermissionPolicy: realization.PathPermissionsNone,
		Reason: reconcile.ReasonAlreadyCurrent,
	})
	if err != nil {
		t.Fatalf("NewManagedPathDecision: %v", err)
	}
	return decision
}

func resultBlockedManagedPathForSubject(t *testing.T, subject topology.SubjectID, destination output.Destination) reconcile.ManagedPathDecision {
	t.Helper()
	decision, err := reconcile.NewManagedPathDecision(reconcile.ManagedPathDecisionInput{
		Kind: reconcile.ManagedPathBlocked, Subject: subject,
		ConsumerTargets: []target.Target{target.TargetCodex}, Scope: target.ScopeProject,
		Destination: destination, ContentKind: realization.PathProjectionDirectory,
		PlacementMode: realization.PathProjectionCopy, PermissionPolicy: realization.PathPermissionsNone,
		Reason: reconcile.ReasonUnmanagedOutputExists,
	})
	if err != nil {
		t.Fatalf("NewManagedPathDecision: %v", err)
	}
	return decision
}

func resultAggregate(t *testing.T, serverID string) (reconcile.AggregateDecision, topology.SubjectID) {
	t.Helper()
	return resultAggregateAt(t, aggregate.MCPPlacementClaudeGlobal, serverID)
}

func resultAggregateAt(
	t *testing.T,
	placement aggregate.MCPPlacementID,
	serverID string,
) (reconcile.AggregateDecision, topology.SubjectID) {
	t.Helper()
	locked := snapshottest.MCPProjection(t, snapshottest.MCPProjectionInput{
		PlacementID: placement, ServerID: serverID,
		LauncherCommand: "npx", CanonicalProjection: `{"args":[],"command":"npx","type":"stdio"}`,
	})
	item, present, err := locked.ManagedAggregateContribution()
	if err != nil || !present {
		t.Fatalf("ManagedAggregateContribution = %#v, %t, %v", item, present, err)
	}
	contract := item.Contribution().Contract()
	decision, err := reconcile.NewAggregateDecision(reconcile.AggregateDecisionInput{
		Kind: reconcile.AggregateNoOp, Reason: reconcile.ReasonAlreadyCurrent,
		DocumentAddress: contract.Address().Document(), CodecContractID: contract.CodecContractID(),
		Projections: []reconcile.AggregateProjectionDecisionInput{{
			Kind: reconcile.AggregateNoOp, Reason: reconcile.ReasonAlreadyCurrent, Contract: contract,
			Subjects: []reconcile.AggregateSubjectDecisionInput{{
				Subject: item.SubjectID(), Contract: contract,
				Kind: reconcile.AggregateNoOp, Reason: reconcile.ReasonAlreadyCurrent,
			}},
		}},
	})
	if err != nil {
		t.Fatalf("NewAggregateDecision: %v", err)
	}
	return decision, item.SubjectID()
}

func resultDelegate(t *testing.T, subject topology.SubjectID, disposition reconcile.DelegateDisposition) reconcile.DelegateAction {
	t.Helper()
	plan := resultDelegatePlan(t)
	risks := []reconcile.DelegateRisk(nil)
	if disposition == reconcile.DelegateSkipped {
		risks = []reconcile.DelegateRisk{{
			Code: reconcile.DelegateRiskDryRunDisclosure, Severity: reconcile.DelegateRiskInfo,
			Subject: plan.IdentityKey(),
		}}
	}
	action, err := reconcile.NewDelegateAction(reconcile.DelegateActionInput{
		Subject: subject, Target: target.TargetClaudeCode, Scope: target.ScopeProject,
		Plan: plan, Disposition: disposition, Risks: risks,
		Dependencies: []reconcile.DelegateDependency{{Kind: reconcile.DelegateDependencyProjection, Subject: subject}},
	})
	if err != nil {
		t.Fatalf("NewDelegateAction: %v", err)
	}
	return action
}

func resultDelegateWithRisks(
	t *testing.T,
	subject topology.SubjectID,
	disposition reconcile.DelegateDisposition,
	risks []reconcile.DelegateRisk,
) reconcile.DelegateAction {
	t.Helper()
	plan := resultDelegatePlan(t)
	action, err := reconcile.NewDelegateAction(reconcile.DelegateActionInput{
		Subject: subject, Target: target.TargetClaudeCode, Scope: target.ScopeProject,
		Plan: plan, Disposition: disposition, Risks: risks,
		Dependencies: []reconcile.DelegateDependency{{Kind: reconcile.DelegateDependencyProjection, Subject: subject}},
	})
	if err != nil {
		t.Fatalf("NewDelegateAction: %v", err)
	}
	return action
}

func resultDelegateAt(
	t *testing.T,
	subject topology.SubjectID,
	actionTarget target.Target,
	scope target.Scope,
) reconcile.DelegateAction {
	t.Helper()
	plan := resultDelegatePlan(t)
	action, err := reconcile.NewDelegateAction(reconcile.DelegateActionInput{
		Subject: subject, Target: actionTarget, Scope: scope,
		Plan: plan, Disposition: reconcile.DelegateScheduled,
		Dependencies: []reconcile.DelegateDependency{{Kind: reconcile.DelegateDependencyProjection, Subject: subject}},
	})
	if err != nil {
		t.Fatalf("NewDelegateAction: %v", err)
	}
	return action
}

func resultRelationAt(
	t *testing.T,
	key string,
	actionTarget target.Target,
	scope target.Scope,
) reconcile.RelationAction {
	t.Helper()
	namespace := "claude-code.plugin-carrier"
	if actionTarget == target.TargetCodex {
		namespace = "codex.plugin-carrier"
	}
	subject, err := topology.NewSubjectID(topology.SubjectHostRelation, namespace, key)
	if err != nil {
		t.Fatalf("NewSubjectID: %v", err)
	}
	carrierIdentity := mustCarrierIdentity(
		t,
		subject,
		actionTarget,
		scope,
		key+"@official",
		key,
	)
	expected := carrierIdentity.ExpectedRelation()
	input := validInput(t, expected, correlationFor(t, expected, observerelation.InventorySpec{
		Availability: observerelation.InventorySupported,
		Freshness:    observerelation.EvidenceFresh,
	}), blockedAdmission(t))
	input.CarrierIdentity = carrierIdentity
	action, err := reconcile.NewRelationAction(input)
	if err != nil {
		t.Fatalf("NewRelationAction: %v", err)
	}
	return action
}

func resultDelegatePlan(t *testing.T) delegate.DelegatePlan {
	t.Helper()
	runner, err := delegate.NewRunner(delegate.RunnerPlain)
	if err != nil {
		t.Fatal(err)
	}
	command, err := delegate.NewCommandSpec("node", []string{"server.js"})
	if err != nil {
		t.Fatal(err)
	}
	env, err := delegate.NewEnvBindingSet(nil)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := delegate.NewDelegatePlan(delegate.DelegatePlanSpec{
		Runner: runner, Command: command, Env: env, PinPolicy: delegate.PinNotApplicable,
	})
	if err != nil {
		t.Fatal(err)
	}
	return plan
}
