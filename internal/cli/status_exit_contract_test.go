package cli

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
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

func TestStatusExitContractIsDocumentedAsModeSpecific(t *testing.T) {
	_, sourcePath, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller did not return the test source path")
	}
	docPath := filepath.Join(filepath.Dir(sourcePath), "..", "..", "docs", "cli.md")
	content, err := os.ReadFile(docPath)
	if err != nil {
		t.Fatalf("ReadFile(%s) returned error: %v", docPath, err)
	}

	for _, row := range []string{
		"| `status` | any valid report | never because of reported state |",
		"| `status --check` | lockfile present, no pending output action, and no blocked carrier-relation, extension-order, or carrier-adoption action | lockfile missing, pending output action, blocked carrier relation/order action, or carrier-adoption claim conflict |",
		"| Report-only `status`, including pending or blocked rows | normal result | empty | `0` |",
		"| Clean `status --check` | normal result | empty | `0` |",
		"| Non-clean `status --check` | normal result | empty | `1` |",
	} {
		if !strings.Contains(string(content), row) {
			t.Fatalf("%s does not contain status contract row %q", docPath, row)
		}
	}
}
