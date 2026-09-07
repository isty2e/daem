package cli

import (
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/realization"
	"github.com/isty2e/daem/internal/realization/lock/snapshottest"
	"github.com/isty2e/daem/internal/reconcile"
	reconcileprojection "github.com/isty2e/daem/internal/reconcile/build/projection"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/target"
	topologyprojection "github.com/isty2e/daem/internal/topology/projection"
	statusworkflow "github.com/isty2e/daem/internal/workflow/status"
	"github.com/isty2e/daem/test/outputtest"
)

func TestStatusExitContractSeparatesReportOnlyFromCheck(t *testing.T) {
	clean := statusworkflow.CommandResult{}
	pending := statusworkflow.CommandResult{Reconciliation: statusPendingManagedPathPlan(t)}
	missingLock := statusworkflow.CommandResult{LockfileMissing: true}

	tests := []struct {
		name   string
		result statusworkflow.CommandResult
		check  bool
		want   int
	}{
		{name: "report-only clean", result: clean, want: 0},
		{name: "report-only pending", result: pending, want: 0},
		{name: "report-only missing lock", result: missingLock, want: 0},
		{name: "check clean", result: clean, check: true, want: 0},
		{name: "check pending", result: pending, check: true, want: 1},
		{name: "check missing lock", result: missingLock, check: true, want: 1},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := statusCheckExitCode(test.result, test.check); got != test.want {
				t.Fatalf("statusCheckExitCode = %d, want %d", got, test.want)
			}
		})
	}
}

func statusPendingManagedPathPlan(t *testing.T) reconcile.Result {
	t.Helper()
	entityID, err := entity.New(entity.KindInstructions, "project")
	if err != nil {
		t.Fatal(err)
	}
	subject, err := topologyprojection.Subject(entityID, "instructions.project.agents")
	if err != nil {
		t.Fatal(err)
	}
	state, err := durable.NewManagedPathState(
		subject,
		[]target.Target{target.TargetCodex},
		target.ScopeProject,
		outputtest.Parse(t, "AGENTS.md"),
		artifact.ContentHash("sha256:1111111111111111111111111111111111111111111111111111111111111111"),
		realization.PathProjectionFile,
		realization.PathPermissionsExact,
		0o600,
	)
	if err != nil {
		t.Fatal(err)
	}
	selectedTargets, err := reconcile.NewSelectedTargets([]target.Target{target.TargetCodex})
	if err != nil {
		t.Fatal(err)
	}
	decisions, err := reconcileprojection.BuildManagedPathDecisions(reconcileprojection.ManagedPathInput{
		Locked:          snapshottest.Section(t),
		SelectedTargets: selectedTargets,
		States:          []durable.ManagedPathState{state},
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := reconcile.NewResult(reconcile.ResultInput{Context: reconcile.ContextInspect, ManagedPaths: decisions})
	if err != nil {
		t.Fatal(err)
	}
	return result
}
