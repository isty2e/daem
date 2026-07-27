package hostroute

import (
	"strings"
	"testing"

	desiredmcp "github.com/isty2e/daem/internal/desired/mcp"
	desiredtest "github.com/isty2e/daem/internal/desired/testfixture"
	"github.com/isty2e/daem/internal/realization/aggregate"
	mcpcodec "github.com/isty2e/daem/internal/realization/aggregate/codec/mcp"
	"github.com/isty2e/daem/internal/realization/delegate"
	mcpdelegate "github.com/isty2e/daem/internal/realization/delegate/mcp"
	"github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/realization/lock/snapshottest"
	reconciliation "github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/reconcile/delegatepolicy"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
	topologymcp "github.com/isty2e/daem/internal/topology/mcp"
)

func TestBuildCreatesScheduledApplyActionWithProjectionDependency(t *testing.T) {
	record := testMCPRecord(t, "context7")

	actions, err := BuildDelegateActions(DelegateInput{
		Locked:          testLockedFile(t, record),
		SelectedTargets: testSelectedTargets(t, target.TargetClaudeCode),
		Context:         reconciliation.ContextApply,
	})
	if err != nil {
		t.Fatalf("BuildDelegateActions returned error: %v", err)
	}

	action := requireOneAction(t, actions)
	if action.Subject() != record.SubjectID() ||
		action.Target() != target.TargetClaudeCode ||
		action.Scope() != target.ScopeProject ||
		action.Disposition() != reconciliation.DelegateScheduled ||
		!action.SchedulesAttempt() {
		t.Fatalf("action = %#v, want scheduled apply delegate action", action)
	}
	assertDependency(t, action, reconciliation.DelegateDependencyProjection, record.SubjectID())
	assertRisk(t, action, reconciliation.DelegateRiskExternalStore)
	assertRisk(t, action, reconciliation.DelegateRiskFloatingPackage)
	if action.Plan().Command().Name() != "npx" {
		t.Fatalf("delegate plan command = %q, want npx", action.Plan().Command().Name())
	}
}

func TestBuildSkipsUnselectedLockedSubject(t *testing.T) {
	actions, err := BuildDelegateActions(DelegateInput{
		Locked:          testLockedFile(t, testMCPRecord(t, "context7")),
		SelectedTargets: testSelectedTargets(t, target.TargetCodex),
		Context:         reconciliation.ContextApply,
	})
	if err != nil {
		t.Fatalf("BuildDelegateActions returned error: %v", err)
	}
	if len(actions) != 0 {
		t.Fatalf("actions = %#v, want unselected subject skipped", actions)
	}
}

func TestBuildAcceptsEmptySelectedTargets(t *testing.T) {
	actions, err := BuildDelegateActions(DelegateInput{
		Locked:  testLockedFile(t, testMCPRecord(t, "context7")),
		Context: reconciliation.ContextApply,
	})
	if err != nil {
		t.Fatalf("BuildDelegateActions empty selected targets returned error: %v", err)
	}
	if len(actions) != 0 {
		t.Fatalf("BuildDelegateActions empty selected targets actions = %#v, want none", actions)
	}
}

func TestBuildDryRunKeepsPlanWithoutScheduling(t *testing.T) {
	record := testMCPRecord(t, "context7")

	actions, err := BuildDelegateActions(DelegateInput{
		Locked:          testLockedFile(t, record),
		SelectedTargets: testSelectedTargets(t, target.TargetClaudeCode),
		Context:         reconciliation.ContextDryRun,
	})
	if err != nil {
		t.Fatalf("BuildDelegateActions returned error: %v", err)
	}

	action := requireOneAction(t, actions)
	if action.Disposition() != reconciliation.DelegateSkipped || action.SchedulesAttempt() {
		t.Fatalf("action disposition = %q, schedules=%t; want skipped dry-run", action.Disposition(), action.SchedulesAttempt())
	}
	assertRisk(t, action, reconciliation.DelegateRiskDryRunDisclosure)
	packageRef, present := action.Plan().PackageRef()
	if !present || packageRef.Name() != "@upstash/context7-mcp" {
		t.Fatalf("delegate plan package = %#v, present=%t", packageRef, present)
	}
}

func TestBuildApplySchedulesAllowedDelegateAction(t *testing.T) {
	record := testMCPRecord(t, "context7")

	actions, err := BuildDelegateActions(DelegateInput{
		Locked:          testLockedFile(t, record),
		SelectedTargets: testSelectedTargets(t, target.TargetClaudeCode),
		Context:         reconciliation.ContextApply,
		Readiness: []DelegateReadinessFact{
			{Subject: record.SubjectID(), Runner: delegatepolicy.RunnerAvailable},
		},
	})
	if err != nil {
		t.Fatalf("BuildDelegateActions returned error: %v", err)
	}

	action := requireOneAction(t, actions)
	if action.Disposition() != reconciliation.DelegateScheduled ||
		!action.SchedulesAttempt() {
		t.Fatalf("action = %#v, want scheduled warned action", action)
	}
	assertRisk(t, action, reconciliation.DelegateRiskExternalStore)
	assertRisk(t, action, reconciliation.DelegateRiskFloatingPackage)
}

func TestBuildApplyBlocksOnPassiveReadinessFacts(t *testing.T) {
	record := testMCPRecord(t, "context7")

	actions, err := BuildDelegateActions(DelegateInput{
		Locked:          testLockedFile(t, record),
		SelectedTargets: testSelectedTargets(t, target.TargetClaudeCode),
		Context:         reconciliation.ContextApply,
		Readiness: []DelegateReadinessFact{
			{
				Subject:        record.SubjectID(),
				Runner:         delegatepolicy.RunnerMissing,
				MissingEnvRefs: []string{"CONTEXT7_API_TOKEN"},
			},
		},
	})
	if err != nil {
		t.Fatalf("BuildDelegateActions returned error: %v", err)
	}

	action := requireOneAction(t, actions)
	if action.Disposition() != reconciliation.DelegateBlocked ||
		action.SchedulesAttempt() {
		t.Fatalf("action = %#v, want blocked action", action)
	}
	assertRisk(t, action, reconciliation.DelegateRiskMissingRunner)
	assertRisk(t, action, reconciliation.DelegateRiskMissingEnvRef)
}

func TestBuildApplyBlocksOnFailedDependencyPrecondition(t *testing.T) {
	record := testMCPRecord(t, "context7")

	actions, err := BuildDelegateActions(DelegateInput{
		Locked:          testLockedFile(t, record),
		SelectedTargets: testSelectedTargets(t, target.TargetClaudeCode),
		Context:         reconciliation.ContextApply,
		Readiness: []DelegateReadinessFact{
			{Subject: record.SubjectID(), Runner: delegatepolicy.RunnerAvailable},
		},
		BlockedDependencies: []DelegateBlockedDependency{
			{Kind: reconciliation.DelegateDependencyProjection, Subject: record.SubjectID()},
		},
	})
	if err != nil {
		t.Fatalf("BuildDelegateActions returned error: %v", err)
	}

	action := requireOneAction(t, actions)
	if action.Disposition() != reconciliation.DelegateBlocked ||
		action.SchedulesAttempt() {
		t.Fatalf("action = %#v, want blocked dependency action", action)
	}
	assertRisk(t, action, reconciliation.DelegateRiskPreconditionBlocked)
}

func TestNewDelegateActionRejectsEffectOutcomesAndIncompatibleDisposition(t *testing.T) {
	record := testMCPRecord(t, "context7")
	delegatePlan := testDelegatePlan(t, delegate.RunnerPlain, "node", []string{"server.js"}, nil, nil, delegate.PinNotApplicable)
	decision := testPolicyDecision(t, delegatePlan, delegatepolicy.ModeApply, delegatepolicy.RunnerMissing, nil)

	for _, disposition := range []reconciliation.DelegateDisposition{"succeeded", "failed", "timeout"} {
		_, err := reconciliation.NewDelegateAction(reconciliation.DelegateActionInput{
			Subject:     record.SubjectID(),
			Target:      target.TargetClaudeCode,
			Scope:       target.ScopeProject,
			Plan:        delegatePlan,
			Disposition: disposition,
			Risks:       decision.Risks(),
		})
		if err == nil || !strings.Contains(err.Error(), "disposition") {
			t.Fatalf("NewDelegateAction(%s) error = %v, want disposition rejection", disposition, err)
		}
	}

	_, err := reconciliation.NewDelegateAction(reconciliation.DelegateActionInput{
		Subject:     record.SubjectID(),
		Target:      target.TargetClaudeCode,
		Scope:       target.ScopeProject,
		Plan:        delegatePlan,
		Disposition: reconciliation.DelegateScheduled,
		Risks:       decision.Risks(),
	})
	if err == nil || !strings.Contains(err.Error(), "incompatible policy risks") {
		t.Fatalf("NewDelegateAction incompatible disposition error = %v", err)
	}
}

func TestActionAccessorsReturnDefensiveCopies(t *testing.T) {
	record := testMCPRecord(t, "context7")
	actions, err := BuildDelegateActions(DelegateInput{
		Locked:          testLockedFile(t, record),
		SelectedTargets: testSelectedTargets(t, target.TargetClaudeCode),
		Context:         reconciliation.ContextApply,
		Readiness: []DelegateReadinessFact{
			{Subject: record.SubjectID(), Runner: delegatepolicy.RunnerAvailable},
		},
	})
	if err != nil {
		t.Fatalf("BuildDelegateActions returned error: %v", err)
	}

	action := requireOneAction(t, actions)
	plan := action.Plan()
	args := plan.Command().Args()
	args[0] = "mutated"
	env := plan.Env().Bindings()
	env[0] = delegate.EnvBinding{}
	risks := action.Risks()
	risks[0] = reconciliation.DelegateRisk{Code: reconciliation.DelegateRiskMissingRunner}
	dependencies := action.Dependencies()
	dependencies[0].Subject = topology.SubjectID{}

	if action.Plan().Command().Args()[0] == "mutated" ||
		action.Plan().Env().Bindings()[0] == (delegate.EnvBinding{}) ||
		action.Risks()[0].Code == reconciliation.DelegateRiskMissingRunner ||
		action.Dependencies()[0].Subject.IsZero() {
		t.Fatalf("action accessors leaked mutable state")
	}
}

func testLockedFile(t *testing.T, records ...lock.LockedSubjectContract) lock.File {
	t.Helper()
	return snapshottest.File(t, records...)
}

func testMCPRecord(t *testing.T, serverID string) lock.LockedSubjectContract {
	t.Helper()
	env := map[string]desiredmcp.EnvReference{
		"API_TOKEN": desiredtest.MCPEnvReference(t, "CONTEXT7_API_TOKEN"),
	}
	transport := desiredtest.MCPStdio(
		t,
		desiredtest.MCPCommand(t, "npx"),
		[]string{"-y", "@upstash/context7-mcp"},
		env,
	)
	binding := desiredtest.MCPBinding(
		t,
		target.TargetClaudeCode,
		target.ScopeProject,
		transport,
		desiredmcp.OnAbsentRemoveBinding,
	)
	server := desiredtest.MCPServer(t, desiredmcp.Spec{
		Name:     serverID,
		Bindings: []desiredmcp.Binding{binding},
	})
	graph, err := topologymcp.Servers([]desiredmcp.Server{server})
	if err != nil {
		t.Fatalf("MCPServer returned error: %v", err)
	}
	delegatePlan, err := mcpdelegate.MCPBindingDelegatePlan(server, binding)
	if err != nil {
		t.Fatalf("MCPBindingDelegatePlan returned error: %v", err)
	}
	canonical, err := mcpcodec.CanonicalClaudeProjectMCPServerEntry(
		mcpcodec.ClaudeProjectMCPServerProjection{
			ServerID:        serverID,
			Command:         "npx",
			Args:            []string{"-y", "@upstash/context7-mcp"},
			Env:             map[string]string{"API_TOKEN": "${CONTEXT7_API_TOKEN}"},
			AdapterContract: aggregate.ClaudeProjectMCPStdioAdapterV1,
		},
	)
	if err != nil {
		t.Fatalf("CanonicalClaudeProjectMCPServerEntry returned error: %v", err)
	}
	record, err := lock.NewMCPProjectionSubjectContract(lock.MCPProjectionSubjectInput{
		Graph:                graph,
		EntityID:             server.ID(),
		PlacementID:          aggregate.MCPPlacementClaudeProject,
		ServerID:             serverID,
		RequestedOnAbsent:    desiredmcp.OnAbsentRemoveBinding,
		LauncherCommand:      "npx",
		LauncherArgs:         []string{"-y", "@upstash/context7-mcp"},
		CanonicalProjection:  string(canonical),
		DelegatePlan:         &delegatePlan,
		CredentialReferences: []string{"CONTEXT7_API_TOKEN"},
	})
	if err != nil {
		t.Fatalf("NewMCPProjectionSubjectContract returned error: %v", err)
	}
	return record
}

func testDelegatePlan(
	t *testing.T,
	runnerKind delegate.RunnerKind,
	commandName string,
	args []string,
	envRefs []string,
	packageRef *delegate.PackageRef,
	pinPolicy delegate.PinPolicy,
) delegate.DelegatePlan {
	t.Helper()
	runner, err := delegate.NewRunner(runnerKind)
	if err != nil {
		t.Fatalf("NewRunner returned error: %v", err)
	}
	command, err := delegate.NewCommandSpec(commandName, args)
	if err != nil {
		t.Fatalf("NewCommandSpec returned error: %v", err)
	}
	bindings := make([]delegate.EnvBinding, 0, len(envRefs))
	for _, name := range envRefs {
		binding, err := delegate.NewEnvBinding(name, name)
		if err != nil {
			t.Fatalf("NewEnvBinding returned error: %v", err)
		}
		bindings = append(bindings, binding)
	}
	env, err := delegate.NewEnvBindingSet(bindings)
	if err != nil {
		t.Fatalf("NewEnvBindingSet returned error: %v", err)
	}
	plan, err := delegate.NewDelegatePlan(delegate.DelegatePlanSpec{
		Runner:     runner,
		Command:    command,
		Env:        env,
		PackageRef: packageRef,
		PinPolicy:  pinPolicy,
	})
	if err != nil {
		t.Fatalf("NewDelegatePlan returned error: %v", err)
	}
	return plan
}

func testPolicyDecision(
	t *testing.T,
	plan delegate.DelegatePlan,
	mode delegatepolicy.Mode,
	runner delegatepolicy.RunnerReadiness,
	missingEnvRefs []string,
) delegatepolicy.Decision {
	t.Helper()
	decision, err := delegatepolicy.Evaluate(delegatepolicy.Input{
		Plan:           plan,
		Mode:           mode,
		Runner:         runner,
		MissingEnvRefs: missingEnvRefs,
	})
	if err != nil {
		t.Fatalf("Evaluate returned error: %v", err)
	}
	return decision
}

func testSelectedTargets(t *testing.T, selected target.Target) reconciliation.SelectedTargets {
	t.Helper()
	selection, err := reconciliation.NewSelectedTargets([]target.Target{selected})
	if err != nil {
		t.Fatalf("NewSelectedTargets returned error: %v", err)
	}
	return selection
}

func requireOneAction(t *testing.T, actions []reconciliation.DelegateAction) reconciliation.DelegateAction {
	t.Helper()
	if len(actions) != 1 {
		t.Fatalf("actions = %#v, want one", actions)
	}
	return actions[0]
}

func assertDependency(t *testing.T, action reconciliation.DelegateAction, kind reconciliation.DelegateDependencyKind, subject topology.SubjectID) {
	t.Helper()
	for _, dependency := range action.Dependencies() {
		if dependency.Kind == kind && dependency.Subject == subject {
			return
		}
	}
	t.Fatalf("dependencies = %#v, want %s/%#v", action.Dependencies(), kind, subject)
}

func assertRisk(t *testing.T, action reconciliation.DelegateAction, code reconciliation.DelegateRiskCode) {
	t.Helper()
	for _, risk := range action.Risks() {
		if risk.Code == code {
			return
		}
	}
	t.Fatalf("risks = %#v, want %s", action.Risks(), code)
}
