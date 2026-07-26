package delegatepolicy

import (
	"testing"

	"github.com/isty2e/daem/internal/realization/delegate"
	lock "github.com/isty2e/daem/internal/realization/lock"
	reconciliation "github.com/isty2e/daem/internal/reconcile"
)

func TestEvaluateApplySchedulesDelegateAndDisclosesRisks(t *testing.T) {
	plan := testDelegatePlanIdentity(t, delegate.RunnerNPX, "npx", []string{"-y", "@scope/server"}, []string{"API_TOKEN"}, testPackageRef(t, delegate.EcosystemNPM, "@scope/server", ""), delegate.PinFloating)

	decision, err := Evaluate(Input{
		Plan:   plan,
		Mode:   ModeApply,
		Runner: RunnerUnknown,
	})
	if err != nil {
		t.Fatalf("Evaluate returned error: %v", err)
	}
	if decision.Outcome() != OutcomeWarn {
		t.Fatalf("decision = %#v, want warned apply attempt", decision)
	}
	assertRisk(t, decision, reconciliation.DelegateRiskExternalStore, reconciliation.DelegateRiskWarn, string(delegate.RunnerNPX))
	assertRisk(t, decision, reconciliation.DelegateRiskFloatingPackage, reconciliation.DelegateRiskWarn, "@scope/server")
	disclosure := decision.Disclosure()
	if disclosure.Command != "npx" ||
		disclosure.Args[1] != "@scope/server" ||
		disclosure.Env[0].SourceName != "API_TOKEN" ||
		disclosure.Package == nil ||
		disclosure.Package.Name != "@scope/server" {
		t.Fatalf("disclosure = %#v", disclosure)
	}
}

func TestEvaluateApplyAllowsPlainDelegateWhenReady(t *testing.T) {
	plan := testDelegatePlanIdentity(t, delegate.RunnerPlain, "node", []string{"server.js"}, nil, nil, delegate.PinNotApplicable)

	decision, err := Evaluate(Input{
		Plan:   plan,
		Mode:   ModeApply,
		Runner: RunnerAvailable,
	})
	if err != nil {
		t.Fatalf("Evaluate returned error: %v", err)
	}
	if decision.Outcome() != OutcomeAllow {
		t.Fatalf("decision = %#v, want allowed plain delegate", decision)
	}
	if len(decision.Risks()) != 0 {
		t.Fatalf("risks = %#v, want none", decision.Risks())
	}
}

func TestEvaluateApplyWarnsForPackageBackedAndFloatingDelegates(t *testing.T) {
	pinned := testDelegatePlanIdentity(t, delegate.RunnerNPX, "npx", []string{"-y", "@scope/server@1.2.3"}, nil, testPackageRef(t, delegate.EcosystemNPM, "@scope/server", "1.2.3"), delegate.PinPinned)
	pinnedDecision, err := Evaluate(Input{
		Plan:   pinned,
		Mode:   ModeApply,
		Runner: RunnerAvailable,
	})
	if err != nil {
		t.Fatalf("Evaluate pinned returned error: %v", err)
	}
	if pinnedDecision.Outcome() != OutcomeWarn {
		t.Fatalf("pinned decision = %#v, want warned execution", pinnedDecision)
	}
	assertRisk(t, pinnedDecision, reconciliation.DelegateRiskExternalStore, reconciliation.DelegateRiskWarn, string(delegate.RunnerNPX))
	assertNoRisk(t, pinnedDecision, reconciliation.DelegateRiskFloatingPackage)

	floating := testDelegatePlanIdentity(t, delegate.RunnerNPX, "npx", []string{"-y", "@scope/server"}, nil, testPackageRef(t, delegate.EcosystemNPM, "@scope/server", ""), delegate.PinFloating)
	floatingDecision, err := Evaluate(Input{
		Plan:   floating,
		Mode:   ModeApply,
		Runner: RunnerAvailable,
	})
	if err != nil {
		t.Fatalf("Evaluate floating returned error: %v", err)
	}
	if floatingDecision.Outcome() != OutcomeWarn {
		t.Fatalf("floating decision = %#v, want warned execution", floatingDecision)
	}
	assertRisk(t, floatingDecision, reconciliation.DelegateRiskFloatingPackage, reconciliation.DelegateRiskWarn, "@scope/server")
}

func TestEvaluateApplyBlocksKnownMissingRunnerAndEnvRefs(t *testing.T) {
	plan := testDelegatePlanIdentity(t, delegate.RunnerPlain, "node", []string{"server.js"}, []string{"API_TOKEN", "OTHER_TOKEN"}, nil, delegate.PinNotApplicable)

	decision, err := Evaluate(Input{
		Plan:           plan,
		Mode:           ModeApply,
		Runner:         RunnerMissing,
		MissingEnvRefs: []string{"OTHER_TOKEN", "API_TOKEN", "API_TOKEN"},
	})
	if err != nil {
		t.Fatalf("Evaluate returned error: %v", err)
	}
	if decision.Outcome() != OutcomeBlock {
		t.Fatalf("decision = %#v, want blocked execution", decision)
	}
	assertRisk(t, decision, reconciliation.DelegateRiskMissingRunner, reconciliation.DelegateRiskBlock, "node")
	assertRisk(t, decision, reconciliation.DelegateRiskMissingEnvRef, reconciliation.DelegateRiskBlock, "API_TOKEN")
	assertRisk(t, decision, reconciliation.DelegateRiskMissingEnvRef, reconciliation.DelegateRiskBlock, "OTHER_TOKEN")
}

func TestEvaluateApplyBlocksOnPreconditionBlock(t *testing.T) {
	plan := testDelegatePlanIdentity(t, delegate.RunnerPlain, "node", []string{"server.js"}, nil, nil, delegate.PinNotApplicable)

	decision, err := Evaluate(Input{
		Plan:   plan,
		Mode:   ModeApply,
		Runner: RunnerAvailable,
		PreconditionBlocks: []PreconditionBlock{
			{Subject: "projection:projection/claude-project-mcp/context7"},
			{Subject: "projection:projection/claude-project-mcp/context7"},
		},
	})
	if err != nil {
		t.Fatalf("Evaluate returned error: %v", err)
	}
	if decision.Outcome() != OutcomeBlock {
		t.Fatalf("decision = %#v, want blocked precondition decision", decision)
	}
	if countRisk(decision, reconciliation.DelegateRiskPreconditionBlocked) != 1 {
		t.Fatalf("risks = %#v, want one precondition block", decision.Risks())
	}
	assertRisk(t, decision, reconciliation.DelegateRiskPreconditionBlocked, reconciliation.DelegateRiskBlock, "projection:projection/claude-project-mcp/context7")
}

func TestEvaluateDryRunSkipsAndKeepsDisclosureWithoutExecution(t *testing.T) {
	plan := testDelegatePlanIdentity(t, delegate.RunnerDocker, "docker", []string{"run", "ghcr.io/acme/server:latest"}, nil, testPackageRef(t, delegate.EcosystemContainer, "ghcr.io/acme/server", "latest"), delegate.PinFloating)

	decision, err := Evaluate(Input{
		Plan:   plan,
		Mode:   ModeDryRun,
		Runner: RunnerMissing,
	})
	if err != nil {
		t.Fatalf("Evaluate returned error: %v", err)
	}
	if decision.Outcome() != OutcomeSkip {
		t.Fatalf("decision = %#v, want dry-run skip without execution block", decision)
	}
	assertRisk(t, decision, reconciliation.DelegateRiskDryRunDisclosure, reconciliation.DelegateRiskInfo, "docker")
	assertRisk(t, decision, reconciliation.DelegateRiskMissingRunner, reconciliation.DelegateRiskBlock, "docker")
	disclosure := decision.Disclosure()
	if disclosure.Package == nil || disclosure.Package.Selector != "latest" {
		t.Fatalf("disclosure = %#v", disclosure)
	}
}

func TestEvaluateRejectsInconsistentMissingEnvRef(t *testing.T) {
	plan := testDelegatePlanIdentity(t, delegate.RunnerPlain, "node", []string{"server.js"}, []string{"API_TOKEN"}, nil, delegate.PinNotApplicable)

	_, err := Evaluate(Input{
		Plan:           plan,
		Mode:           ModeApply,
		Runner:         RunnerAvailable,
		MissingEnvRefs: []string{"OTHER_TOKEN"},
	})
	if err == nil {
		t.Fatal("Evaluate returned nil error for missing env ref outside plan")
	}
}

func TestEvaluateWarnsForHostSelectedDelegate(t *testing.T) {
	plan := testDelegatePlanIdentity(t, delegate.RunnerHostNative, "claude", []string{"plugin", "install", "marketplace/name"}, nil, nil, delegate.PinHostSelected)

	decision, err := Evaluate(Input{
		Plan:   plan,
		Mode:   ModeApply,
		Runner: RunnerAvailable,
	})
	if err != nil {
		t.Fatalf("Evaluate returned error: %v", err)
	}
	if decision.Outcome() != OutcomeWarn {
		t.Fatalf("decision = %#v, want warned host-selected execution", decision)
	}
	assertRisk(t, decision, reconciliation.DelegateRiskHostSelected, reconciliation.DelegateRiskWarn, string(delegate.RunnerHostNative))
}

func TestEvaluateRejectsTamperedDelegatePlanIdentity(t *testing.T) {
	plan := testDelegatePlanIdentity(t, delegate.RunnerPlain, "node", []string{"server.js"}, nil, nil, delegate.PinNotApplicable)
	plan.Args = []string{"other.js"}

	_, err := Evaluate(Input{
		Plan:   plan,
		Mode:   ModeApply,
		Runner: RunnerAvailable,
	})
	if err == nil {
		t.Fatal("Evaluate returned nil error for tampered delegate plan identity")
	}
}

func TestEvaluateRejectsUnsupportedModeAndRunnerReadiness(t *testing.T) {
	plan := testDelegatePlanIdentity(t, delegate.RunnerPlain, "node", []string{"server.js"}, nil, nil, delegate.PinNotApplicable)

	_, modeErr := Evaluate(Input{
		Plan:   plan,
		Mode:   Mode("background_apply"),
		Runner: RunnerAvailable,
	})
	if modeErr == nil {
		t.Fatal("Evaluate returned nil error for unsupported mode")
	}

	_, readinessErr := Evaluate(Input{
		Plan:   plan,
		Mode:   ModeApply,
		Runner: RunnerReadiness("maybe"),
	})
	if readinessErr == nil {
		t.Fatal("Evaluate returned nil error for unsupported runner readiness")
	}
}

func TestEvaluateDeduplicatesMissingEnvRisks(t *testing.T) {
	plan := testDelegatePlanIdentity(t, delegate.RunnerPlain, "node", []string{"server.js"}, []string{"API_TOKEN"}, nil, delegate.PinNotApplicable)

	decision, err := Evaluate(Input{
		Plan:           plan,
		Mode:           ModeApply,
		Runner:         RunnerAvailable,
		MissingEnvRefs: []string{"API_TOKEN", "API_TOKEN"},
	})
	if err != nil {
		t.Fatalf("Evaluate returned error: %v", err)
	}
	if countRisk(decision, reconciliation.DelegateRiskMissingEnvRef) != 1 {
		t.Fatalf("risks = %#v, want one missing env risk", decision.Risks())
	}
}

func TestDecisionRisksReturnsCopy(t *testing.T) {
	plan := testDelegatePlanIdentity(t, delegate.RunnerNPX, "npx", []string{"server"}, nil, testPackageRef(t, delegate.EcosystemNPM, "server", ""), delegate.PinFloating)
	decision, err := Evaluate(Input{
		Plan:   plan,
		Mode:   ModeApply,
		Runner: RunnerAvailable,
	})
	if err != nil {
		t.Fatalf("Evaluate returned error: %v", err)
	}

	risks := decision.Risks()
	risks[0] = reconciliation.DelegateRisk{Code: reconciliation.DelegateRiskMissingRunner, Severity: reconciliation.DelegateRiskBlock, Subject: "node"}

	assertRisk(t, decision, reconciliation.DelegateRiskExternalStore, reconciliation.DelegateRiskWarn, string(delegate.RunnerNPX))
	assertRisk(t, decision, reconciliation.DelegateRiskFloatingPackage, reconciliation.DelegateRiskWarn, "server")
	assertNoRisk(t, decision, reconciliation.DelegateRiskMissingRunner)
}

func TestEvaluateApplyAllowsUnknownRunnerForExecutorRevalidation(t *testing.T) {
	plan := testDelegatePlanIdentity(t, delegate.RunnerPlain, "node", []string{"server.js"}, nil, nil, delegate.PinNotApplicable)

	decision, err := Evaluate(Input{
		Plan:   plan,
		Mode:   ModeApply,
		Runner: RunnerUnknown,
	})
	if err != nil {
		t.Fatalf("Evaluate returned error: %v", err)
	}
	if decision.Outcome() != OutcomeAllow {
		t.Fatalf("decision = %#v, want executor revalidation to handle unknown runner", decision)
	}
	if len(decision.Risks()) != 0 {
		t.Fatalf("risks = %#v, want none for unknown runner fact", decision.Risks())
	}
}

func TestEvaluateApplyBlocksOnPassiveReadinessFacts(t *testing.T) {
	plan := testDelegatePlanIdentity(t, delegate.RunnerPlain, "node", []string{"server.js"}, []string{"API_TOKEN"}, nil, delegate.PinNotApplicable)

	decision, err := Evaluate(Input{
		Plan:           plan,
		Mode:           ModeApply,
		Runner:         RunnerMissing,
		MissingEnvRefs: []string{"API_TOKEN"},
	})
	if err != nil {
		t.Fatalf("Evaluate returned error: %v", err)
	}
	if decision.Outcome() != OutcomeBlock {
		t.Fatalf("decision = %#v, want blocked ordinary apply", decision)
	}
	assertRisk(t, decision, reconciliation.DelegateRiskMissingRunner, reconciliation.DelegateRiskBlock, "node")
	assertRisk(t, decision, reconciliation.DelegateRiskMissingEnvRef, reconciliation.DelegateRiskBlock, "API_TOKEN")
}

func TestEvaluateFloatingPackageRiskSubjectIncludesSelector(t *testing.T) {
	plan := testDelegatePlanIdentity(t, delegate.RunnerDocker, "docker", []string{"run", "ghcr.io/acme/server:latest"}, nil, testPackageRef(t, delegate.EcosystemContainer, "ghcr.io/acme/server", "latest"), delegate.PinFloating)

	decision, err := Evaluate(Input{
		Plan:   plan,
		Mode:   ModeApply,
		Runner: RunnerAvailable,
	})
	if err != nil {
		t.Fatalf("Evaluate returned error: %v", err)
	}
	assertRisk(t, decision, reconciliation.DelegateRiskFloatingPackage, reconciliation.DelegateRiskWarn, "ghcr.io/acme/server@latest")
}

func TestEvaluateDisclosureCopiesPlanFields(t *testing.T) {
	plan := testDelegatePlanIdentity(t, delegate.RunnerNPX, "npx", []string{"server"}, []string{"API_TOKEN"}, testPackageRef(t, delegate.EcosystemNPM, "server", ""), delegate.PinFloating)

	decision, err := Evaluate(Input{
		Plan:   plan,
		Mode:   ModeApply,
		Runner: RunnerUnknown,
	})
	if err != nil {
		t.Fatalf("Evaluate returned error: %v", err)
	}

	plan.Args[0] = "mutated"
	plan.Env[0] = lock.DelegateEnvBinding{Name: "MUTATED", SourceName: "MUTATED"}
	plan.Package.Name = "mutated"

	disclosure := decision.Disclosure()
	if disclosure.Args[0] != "server" ||
		disclosure.Env[0].SourceName != "API_TOKEN" ||
		disclosure.Package == nil ||
		disclosure.Package.Name != "server" {
		t.Fatalf("disclosure = %#v, want copied plan fields", disclosure)
	}
}

func TestDecisionDisclosureIsImmutableAndMatchesEveryPlanFact(t *testing.T) {
	plan := testDelegatePlanIdentity(
		t,
		delegate.RunnerNPX,
		"npx",
		[]string{"server"},
		[]string{"API_TOKEN"},
		testPackageRef(t, delegate.EcosystemNPM, "server", ""),
		delegate.PinFloating,
	)
	decision, err := Evaluate(Input{Plan: plan, Mode: ModeApply, Runner: RunnerAvailable})
	if err != nil {
		t.Fatalf("Evaluate returned error: %v", err)
	}
	disclosure := decision.Disclosure()
	if !disclosure.MatchesPlan(plan) {
		t.Fatal("canonical disclosure does not match its locked plan")
	}

	disclosure.Env[0].SourceName = "OTHER_TOKEN"
	disclosure.Args[0] = "other"
	disclosure.Package.Name = "other"
	if disclosure.MatchesPlan(plan) {
		t.Fatal("tampered disclosure matched locked plan")
	}
	fresh := decision.Disclosure()
	if fresh.Env[0].SourceName != "API_TOKEN" ||
		fresh.Args[0] != "server" ||
		fresh.Package.Name != "server" {
		t.Fatalf("decision disclosure leaked mutable state: %#v", fresh)
	}
}

func testDelegatePlanIdentity(
	t *testing.T,
	runnerKind delegate.RunnerKind,
	commandName string,
	args []string,
	envRefs []string,
	packageRef *delegate.PackageRef,
	pinPolicy delegate.PinPolicy,
) lock.DelegatePlanIdentity {
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
	return lock.DelegatePlanIdentityFromPlan(plan)
}

func testPackageRef(t *testing.T, ecosystem delegate.PackageEcosystem, name string, selector string) *delegate.PackageRef {
	t.Helper()
	packageRef, err := delegate.NewPackageRef(ecosystem, name, selector)
	if err != nil {
		t.Fatalf("NewPackageRef returned error: %v", err)
	}
	return &packageRef
}

func assertRisk(t *testing.T, decision Decision, code reconciliation.DelegateRiskCode, severity reconciliation.DelegateRiskSeverity, subject string) {
	t.Helper()
	for _, risk := range decision.Risks() {
		if risk.Code == code && risk.Severity == severity && risk.Subject == subject {
			return
		}
	}
	t.Fatalf("risks = %#v, want %s/%s/%s", decision.Risks(), code, severity, subject)
}

func assertNoRisk(t *testing.T, decision Decision, code reconciliation.DelegateRiskCode) {
	t.Helper()
	for _, risk := range decision.Risks() {
		if risk.Code == code {
			t.Fatalf("risks = %#v, did not want %s", decision.Risks(), code)
		}
	}
}

func countRisk(decision Decision, code reconciliation.DelegateRiskCode) int {
	count := 0
	for _, risk := range decision.Risks() {
		if risk.Code == code {
			count++
		}
	}
	return count
}
