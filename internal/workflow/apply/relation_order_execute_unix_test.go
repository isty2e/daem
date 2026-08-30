//go:build unix

package apply

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	relationhost "github.com/isty2e/daem/internal/assurance/observe/relation/host"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	"github.com/isty2e/daem/internal/effect/execute"
	"github.com/isty2e/daem/internal/effect/execute/delegate"
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	hostsurfacecatalog "github.com/isty2e/daem/internal/hostsurface/catalog"
	daempaths "github.com/isty2e/daem/internal/paths"
	aggregatecodec "github.com/isty2e/daem/internal/realization/aggregate/codec"
	lock "github.com/isty2e/daem/internal/realization/lock"
	lockrefine "github.com/isty2e/daem/internal/realization/lock/refine"
	hostrelation "github.com/isty2e/daem/internal/realization/relation"
	"github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/subprocess"
	"github.com/isty2e/daem/internal/target"
	targetselection "github.com/isty2e/daem/internal/target/selection"
	"github.com/isty2e/daem/internal/topology"
)

func TestApplyAuthorityCoversEveryOpenCodeOrderSelectorCandidate(t *testing.T) {
	t.Parallel()

	planned := applyAuthorityTestPlan(t)
	root := planned.context.Paths.ManifestRoot
	locked := relationOrderTestLock(
		t,
		root,
		desiredextension.CarrierOpenCodePlugin,
		target.TargetOpenCode,
		[]string{"alpha@1", "beta@1"},
	)
	selection, err := targetselection.ForDiagnostics([]string{string(target.TargetOpenCode)})
	if err != nil {
		t.Fatal(err)
	}
	selected, err := reconcile.NewSelectedTargets([]target.Target{target.TargetOpenCode})
	if err != nil {
		t.Fatal(err)
	}
	planned.context.Lockfile = locked
	planned.context.Selection = selection
	planned.assessment.SelectedTargets = selected

	evidence, err := buildApplyAuthorityEvidence(t.Context(), planned)
	if err != nil {
		t.Fatal(err)
	}
	want := map[mutation.RevisionRequest]bool{}
	for _, name := range []string{
		"opencode.json",
		"opencode.jsonc",
		"tui.json",
		"tui.jsonc",
	} {
		path := filepath.Join(root, ".opencode", name)
		want[mutation.NewBoundedContentRevisionRequest(
			path,
			mutation.PathEffectDirectoryEntry,
		)] = false
		want[mutation.NewBoundedContentRevisionRequest(
			path,
			mutation.PathEffectReferent,
		)] = false
	}
	for _, request := range evidence.firstEffectRevisions {
		if _, expected := want[request]; expected {
			want[request] = true
		}
	}
	for request, found := range want {
		if !found {
			t.Fatalf("missing order selector authority %#v", request)
		}
	}
}

func TestRelationOrderConvergenceRequiresRenewedRiskAuthorization(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	paths := daempaths.Paths{ManifestRoot: root}
	locked := relationOrderTestLock(
		t,
		root,
		desiredextension.CarrierPiPackage,
		target.TargetPi,
		[]string{"npm:a@1", "npm:b@1"},
	)
	settingsPath := filepath.Join(root, ".pi", "settings.json")
	writeRelationOrderTestFile(t, settingsPath, `{"packages":["npm:b@1","npm:foreign@1"]}`)
	constraint := locked.Locked.OrderConstraints()[0]
	initial := relationOrderTestReconciliation(
		t,
		paths,
		locked,
		[]string{constraint.Members()[0].Subject().String()},
	)

	postRoute := `{"packages":["npm:b@1","npm:foreign@1","npm:a@1"]}`
	writeRelationOrderTestFile(t, settingsPath, postRoute)
	projectRoot := captureRelationOrderTestRoot(t, root)
	defer projectRoot.Close()
	options := runOptions{
		projectRoot:       projectRoot,
		executionGuard:    testApplyExecutionGuard(t, paths),
		orderRiskBaseline: newRelationOrderRiskBaseline(initial.RelationOrders()),
		validateBeforeEffects: func(context.Context, mutation.PhysicalAuthoritySet) error {
			return nil
		},
	}

	result, err := runRelationOrderConvergence(
		t.Context(),
		paths,
		locked,
		initial,
		options,
	)
	if !errors.Is(err, ErrRelationOrderRiskExpansion) {
		t.Fatalf("runRelationOrderConvergence error = %v, want risk expansion", err)
	}
	if result.actionCount != 0 ||
		result.results[0].Outcome() != RelationOrderNotAttempted {
		t.Fatalf("unapproved result = %#v", result)
	}
	if got := string(readRelationOrderTestFile(t, settingsPath)); got != postRoute {
		t.Fatalf("unapproved risk mutated settings: %s", got)
	}

	declined := 0
	options.RelationOrderRiskAuthorizer = func(
		_ context.Context,
		expansion RelationOrderRiskExpansion,
	) (bool, error) {
		declined++
		if expansion.AddedRiskCount() != 2 {
			t.Fatalf("expanded risk count = %d, want 2", expansion.AddedRiskCount())
		}
		return false, nil
	}
	result, err = runRelationOrderConvergence(
		t.Context(),
		paths,
		locked,
		initial,
		options,
	)
	if !errors.Is(err, ErrRelationOrderNotAuthorized) ||
		declined != 1 ||
		result.results[0].Outcome() != RelationOrderNotAttempted {
		t.Fatalf(
			"declined result = %#v declined=%d error=%v",
			result,
			declined,
			err,
		)
	}
	if got := string(readRelationOrderTestFile(t, settingsPath)); got != postRoute {
		t.Fatalf("declined risk mutated settings: %s", got)
	}

	authorized := 0
	options.RelationOrderRiskAuthorizer = func(
		_ context.Context,
		expansion RelationOrderRiskExpansion,
	) (bool, error) {
		authorized++
		if expansion.AddedRiskCount() != 2 ||
			len(expansion.Deltas()) != 1 {
			t.Fatalf("risk expansion = %#v", expansion)
		}
		return true, nil
	}
	result, err = runRelationOrderConvergence(
		t.Context(),
		paths,
		locked,
		initial,
		options,
	)
	if err != nil {
		t.Fatalf("authorized convergence: %v", err)
	}
	if authorized != 1 || result.actionCount != 1 ||
		result.results[0].Outcome() != RelationOrderConverged ||
		!result.results[0].Changed() {
		t.Fatalf("authorized result = %#v authorized=%d", result, authorized)
	}
	got := string(readRelationOrderTestFile(t, settingsPath))
	if strings.Index(got, "npm:a@1") > strings.Index(got, "npm:b@1") {
		t.Fatalf("Pi package order did not converge: %s", got)
	}

	exact := relationOrderTestReconciliation(t, paths, locked, nil)
	var eventCount int
	options.ExecuteEvents = func(execute.Event) { eventCount++ }
	result, err = runRelationOrderConvergence(
		t.Context(),
		paths,
		locked,
		exact,
		options,
	)
	if err != nil || result.actionCount != 0 ||
		result.results[0].Outcome() != RelationOrderExact ||
		eventCount != 0 {
		t.Fatalf("idempotent result = %#v events=%d err=%v", result, eventCount, err)
	}
}

func TestRelationOrderConvergenceReportsPartialOpenCodeFailure(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	paths := daempaths.Paths{ManifestRoot: root}
	locked := relationOrderTestLock(
		t,
		root,
		desiredextension.CarrierOpenCodePlugin,
		target.TargetOpenCode,
		[]string{"alpha@1", "beta@1"},
	)
	serverPath := filepath.Join(root, ".opencode", "opencode.json")
	tuiPath := filepath.Join(root, ".opencode", "tui.json")
	initialDocument := `{"plugin":["beta@1","foreign@1","alpha@1"]}`
	writeRelationOrderTestFile(t, serverPath, initialDocument)
	writeRelationOrderTestFile(t, tuiPath, initialDocument)
	initial := relationOrderTestReconciliation(t, paths, locked, nil)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	projectRoot := captureRelationOrderTestRoot(t, root)
	defer projectRoot.Close()
	var events []execute.Event
	result, err := runRelationOrderConvergence(
		ctx,
		paths,
		locked,
		initial,
		runOptions{
			projectRoot:       projectRoot,
			executionGuard:    testApplyExecutionGuard(t, paths),
			orderRiskBaseline: newRelationOrderRiskBaseline(initial.RelationOrders()),
			validateBeforeEffects: func(context.Context, mutation.PhysicalAuthoritySet) error {
				return nil
			},
			ExecuteEvents: func(event execute.Event) {
				events = append(events, event)
				if event.Kind == execute.EventRelationOrderDone {
					cancel()
				}
			},
		},
	)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("runRelationOrderConvergence error = %v, want cancellation", err)
	}
	if result.actionCount != 1 || len(result.results) != 2 {
		t.Fatalf("partial result = %#v", result)
	}
	outcomes := []RelationOrderOutcome{
		result.results[0].Outcome(),
		result.results[1].Outcome(),
	}
	if !slices.Equal(outcomes, []RelationOrderOutcome{
		RelationOrderConverged,
		RelationOrderFailed,
	}) {
		t.Fatalf("partial outcomes = %v", outcomes)
	}
	if !strings.Contains(result.results[1].Detail(), "context canceled") ||
		result.results[1].PublicDetail() != "extension order update failed" {
		t.Fatalf(
			"failed result details = raw %q public %q",
			result.results[1].Detail(),
			result.results[1].PublicDetail(),
		)
	}
	if got := string(readRelationOrderTestFile(t, serverPath)); strings.Index(got, "alpha@1") > strings.Index(got, "beta@1") {
		t.Fatalf("server did not converge before cancellation: %s", got)
	}
	if got := string(readRelationOrderTestFile(t, tuiPath)); got != initialDocument {
		t.Fatalf("TUI changed after cancellation: %s", got)
	}
	if len(events) != 4 ||
		events[0].Kind != execute.EventRelationOrderStarted ||
		events[1].Kind != execute.EventRelationOrderDone ||
		events[2].Kind != execute.EventRelationOrderStarted ||
		events[3].Kind != execute.EventRelationOrderFailed {
		t.Fatalf("partial events = %#v", events)
	}
}

func TestRelationOrderObservationFailureSuppressesLaterDelegate(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	paths, err := daempaths.Resolve(filepath.Join(root, "daem.toml"))
	if err != nil {
		t.Fatal(err)
	}
	paths, err = paths.WithDataDir(filepath.Join(root, ".daem-test-data"))
	if err != nil {
		t.Fatal(err)
	}
	orderLocked := relationOrderTestLock(
		t,
		root,
		desiredextension.CarrierPiPackage,
		target.TargetPi,
		[]string{"npm:a@1", "npm:b@1"},
	)
	settingsPath := filepath.Join(root, ".pi", "settings.json")
	writeRelationOrderTestFile(t, settingsPath, `{"packages":["npm:b@1","npm:a@1"]}`)
	initial := relationOrderTestReconciliation(t, paths, orderLocked, nil)

	mcpLocked, _ := applyMCPLockfile(
		t,
		"context7",
		"must-not-run-daem-test",
		[]string{"--serve"},
	)
	subjects := append(orderLocked.Locked.Subjects(), mcpLocked.Locked.Subjects()...)
	section, err := lock.NewLockedSection(
		subjects,
		orderLocked.Locked.OrderConstraints(),
	)
	if err != nil {
		t.Fatal(err)
	}
	combined := lock.File{Version: lock.CurrentVersion, Locked: section}
	delegatePlan, present := mcpLocked.Locked.Subjects()[0].DelegatePlan()
	if !present {
		t.Fatal("MCP lock subject has no delegate plan")
	}
	delegateAction, err := reconcile.NewDelegateAction(reconcile.DelegateActionInput{
		Subject:     mcpLocked.Locked.Subjects()[0].SubjectID(),
		Target:      target.TargetClaudeCode,
		Scope:       target.ScopeProject,
		Plan:        delegatePlan,
		Disposition: reconcile.DelegateScheduled,
	})
	if err != nil {
		t.Fatal(err)
	}
	reconciliation, err := reconcile.NewResult(reconcile.ResultInput{
		Context:        reconcile.ContextApply,
		RelationOrders: initial.RelationOrders(),
		Delegates:      []reconcile.DelegateAction{delegateAction},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(settingsPath, []byte(`{"packages":[`), 0o600); err != nil {
		t.Fatal(err)
	}
	owner, err := stateauthority.New(mustObservedPathAuthority(t, paths.StatefilePath), paths.ManifestPath)
	if err != nil {
		t.Fatal(err)
	}
	runnerCalled := false
	_, err = runHostRoutesOrderDelegatesAndPersistAttemptRecords(
		t.Context(),
		paths,
		combined,
		applyMCPSelection(t),
		paths.StatefilePath,
		durable.EmptySnapshot(),
		owner,
		durablecarrier.EmptyGlobalCarrierClaims(),
		0,
		reconciliation,
		runOptions{
			orderRiskBaseline: newRelationOrderRiskBaseline(reconciliation.RelationOrders()),
			DelegateExecutor: delegate.NewExecutor(delegate.Options{
				Runner: func(context.Context, subprocess.CommandRequest) subprocess.CommandResult {
					runnerCalled = true
					return subprocess.CommandResult{Started: true, HasExitCode: true}
				},
			}),
		},
	)
	if !errors.Is(err, ErrRelationOrderBlock) {
		t.Fatalf("execution error = %v, want relation order block", err)
	}
	if runnerCalled {
		t.Fatal("delegate runner was called after relation-order observation failure")
	}
}

func relationOrderTestLock(
	t testing.TB,
	root string,
	carrier desiredextension.Carrier,
	selectedTarget target.Target,
	sourceValues []string,
) lock.File {
	return relationOrderTestLockAtScope(
		t,
		root,
		carrier,
		selectedTarget,
		target.ScopeProject,
		sourceValues,
	)
}

func relationOrderTestLockAtScope(
	t testing.TB,
	root string,
	carrier desiredextension.Carrier,
	selectedTarget target.Target,
	scope target.Scope,
	sourceValues []string,
) lock.File {
	t.Helper()
	extensions := make([]desiredextension.Extension, 0, len(sourceValues))
	for index, value := range sourceValues {
		source, err := desiredextension.NewSourceRef(
			desiredextension.SourceKindHostSource,
			value,
		)
		if err != nil {
			t.Fatal(err)
		}
		extension, err := desiredextension.New(desiredextension.Spec{
			Name:    string(rune('a' + index)),
			Carrier: carrier,
			Target:  selectedTarget,
			Scope:   scope,
			Source:  source,
		})
		if err != nil {
			t.Fatal(err)
		}
		extensions = append(extensions, extension)
	}
	subjects, err := lockrefine.Extensions(extensions)
	if err != nil {
		t.Fatal(err)
	}
	constraints, err := lockrefine.ExtensionOrderConstraints(
		extensions,
		aggregatecodec.ExtensionOrderIdentityResolver(
			daempaths.Paths{ManifestRoot: root},
		),
	)
	if err != nil {
		t.Fatal(err)
	}
	section, err := lock.NewLockedSection(subjects, constraints)
	if err != nil {
		t.Fatal(err)
	}
	return lock.File{Version: lock.CurrentVersion, Locked: section}
}

func relationOrderTestReconciliation(
	t testing.TB,
	paths daempaths.Paths,
	locked lock.File,
	pendingInstallSubjects []string,
) reconcile.Result {
	t.Helper()
	constraint := locked.Locked.OrderConstraints()[0]
	selectedTarget, capability, admitted := hostsurfacecatalog.Product().ExtensionOrderCapabilityForClass(
		constraint.ClassID(),
	)
	if !admitted {
		t.Fatal("order class is not admitted")
	}
	observation, err := relationhost.ObserveOrder(relationhost.OrderInput{
		Paths: paths, Lockfile: locked, Constraint: constraint,
	})
	if err != nil {
		t.Fatal(err)
	}
	pending := make([]hostrelation.RelationOrderMember, 0, len(pendingInstallSubjects))
	for _, value := range pendingInstallSubjects {
		for _, member := range constraint.Members() {
			if member.Subject().String() == value {
				pending = append(pending, member)
			}
		}
	}
	pendingIDs := make([]topology.SubjectID, 0, len(pending))
	for _, member := range pending {
		pendingIDs = append(pendingIDs, member.Subject())
	}
	decisions := make([]reconcile.RelationOrderDecision, 0, len(observation.Physical()))
	for _, physical := range observation.Physical() {
		decision, err := reconcile.NewRelationOrderDecision(
			reconcile.RelationOrderDecisionInput{
				Target:          selectedTarget,
				Scope:           capability.Scope(),
				Constraint:      constraint,
				Sequence:        physical.Sequence(),
				PendingInstalls: pendingIDs,
			},
		)
		if err != nil {
			t.Fatal(err)
		}
		decisions = append(decisions, decision)
	}
	result, err := reconcile.NewResult(reconcile.ResultInput{
		Context:        reconcile.ContextApply,
		RelationOrders: decisions,
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func captureRelationOrderTestRoot(t testing.TB, root string) *rootedpath.CapturedRoot {
	t.Helper()
	captured, err := rootedpath.CaptureRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	return captured
}

func writeRelationOrderTestFile(t testing.TB, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func readRelationOrderTestFile(t testing.TB, path string) []byte {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return content
}
