//go:build darwin || linux

package protection

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	journalrecovery "github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/subprocess"
)

func TestInvocationDemandPreparesCompleteScheduleBeforeTransfer(t *testing.T) {
	first, firstInput, firstBind := invocationFixture(t)
	second, secondInput, secondBind := invocationFixture(t)
	_, unreserved, _ := invocationFixture(t)
	budget := journalrecovery.NewPhysicalCaptureBudget(nil)
	firstInput, err := PrepareInvocation(t.Context(), firstInput, budget)
	if err == nil {
		firstInput, err = firstInput.ReserveWork(budget, journalrecovery.InvocationWork{})
	}
	if err != nil {
		t.Fatal(err)
	}
	secondInput, err = PrepareInvocation(t.Context(), secondInput, budget)
	if err == nil {
		secondInput, err = secondInput.ReserveWork(budget, journalrecovery.InvocationWork{})
	}
	if err != nil {
		t.Fatal(err)
	}
	unreserved, err = PrepareInvocation(t.Context(), unreserved, budget)
	if err != nil {
		t.Fatal(err)
	}
	request := subprocess.CommandAttemptRequest{Command: "/bin/true"}
	result, err := ExecuteInvocation(t.Context(), firstInput, subprocess.CommandExecutor{}, request, firstBind, nil)
	if err != nil {
		t.Fatal("first compiled invocation failed", err)
	}
	assertInvocationSettled(t, first, result)
	if _, err := secondInput.ReserveWork(budget, journalrecovery.InvocationWork{}); err == nil {
		t.Fatal("a copied plan reserved a second allocation")
	}
	if _, err := unreserved.ReserveWork(budget, journalrecovery.InvocationWork{}); err == nil {
		t.Fatal("another demand was admitted after transfer")
	}
	result, err = ExecuteInvocation(t.Context(), secondInput, subprocess.CommandExecutor{}, request, secondBind, nil)
	if err != nil {
		t.Fatal("already reserved second invocation lost its credit", err)
	}
	assertInvocationSettled(t, second, result)
}

func TestInvocationDemandProjectionDoesNotReobserveChangedLayout(t *testing.T) {
	fixture, input, bind := invocationFixture(t)
	budget := journalrecovery.NewPhysicalCaptureBudget(nil)
	input, err := PrepareInvocation(t.Context(), input, budget)
	if err != nil {
		t.Fatal(err)
	}
	parent := filepath.Dir(fixture.Documents[0])
	if err := os.Rename(parent, parent+".moved"); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Base(parent), parent); err != nil {
		t.Fatal(err)
	}
	input, err = input.ReserveWork(budget, journalrecovery.InvocationWork{})
	if err != nil {
		t.Fatal("pure demand projection consulted the changed layout", err)
	}
	result, err := ExecuteInvocation(t.Context(), input, subprocess.CommandExecutor{}, subprocess.CommandAttemptRequest{Command: "/bin/true"}, bind, nil)
	if err == nil {
		t.Fatal("compiled demand bypassed fresh namespace qualification")
	}
	if _, attempted := result.Process(); attempted {
		t.Fatal("unqualified layout reached its command")
	}
	if rows, err := fixture.Store.LoadPendingProtections(t.Context()); err != nil || len(rows) != 0 {
		t.Fatal("pre-Arm layout refusal changed the registry", err)
	}
}

func TestInvocationDemandRefusalCannotPublishOrExecute(t *testing.T) {
	fixture, input, bind := invocationFixture(t)
	budget := journalrecovery.NewPhysicalCaptureBudget(nil)
	input, err := PrepareInvocation(t.Context(), input, budget)
	if err != nil {
		t.Fatal(err)
	}
	if err := budget.AdmitPhysicalWork(0, 0, budget.RemainingBytes()); err != nil {
		t.Fatal(err)
	}
	if _, err := input.ReserveWork(budget, journalrecovery.InvocationWork{}); err == nil {
		t.Fatal("insufficient shared capacity admitted the invocation")
	}
	result, err := ExecuteInvocation(t.Context(), input, subprocess.CommandExecutor{}, subprocess.CommandAttemptRequest{Command: "/bin/true"}, bind, nil)
	if err == nil {
		t.Fatal("unreserved invocation executed")
	}
	if _, attempted := result.Process(); attempted {
		t.Fatal("failed reservation reached command execution")
	}
	if rows, err := fixture.Store.LoadPendingProtections(t.Context()); err != nil || len(rows) != 0 {
		t.Fatal("failed reservation published an obligation", err)
	}
}

func TestInvocationDemandCoversReplacementAndAbsenceWithoutRecreation(t *testing.T) {
	for _, remove := range []bool{false, true} {
		name := "replacement"
		if remove {
			name = "absence"
		}
		t.Run(name, func(t *testing.T) {
			fixture, input, bind := invocationFixture(t)
			script := `printf 'rewritten\n' > "$1.new"; /bin/chmod 600 "$1.new"; /bin/mv "$1.new" "$1"`
			if remove {
				script = `/bin/rm "$1"`
			}
			result, err := invokeFixture(t.Context(), input, subprocess.CommandExecutor{}, subprocess.CommandAttemptRequest{
				Command: "/bin/sh", Args: []string{"-c", script, "protected", fixture.Documents[0], fixture.Documents[1]},
			}, bind, nil)
			if err != nil {
				t.Fatal("compiled lifecycle could not settle ordinary host changes", err)
			}
			if record, present := result.Protection(); !present || record.Settlement() != SettlementCleared {
				t.Fatal("ordinary change did not settle its protection")
			}
			if process, present := result.Process(); !present || !process.Attempt().Succeeded() {
				t.Fatal("ordinary change did not complete mechanically")
			}
			if info, err := os.Stat(fixture.Documents[1]); err != nil || info.Mode().Perm() != 0o600 {
				t.Fatal("safety changed the unrelated document mode", err)
			}
			if remove {
				if _, err := os.Stat(fixture.Documents[0]); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("safety recreated a removed document", err)
				}
			} else if content, err := os.ReadFile(fixture.Documents[0]); err != nil || string(content) != "rewritten\n" {
				t.Fatal("safety rejected or restored ordinary replacement content", err)
			}
			if rows, err := fixture.Store.LoadPendingProtections(t.Context()); err != nil || len(rows) != 0 {
				t.Fatal("settled lifecycle retained an obligation", err)
			}
		})
	}
}

func TestInvocationChangedOwnerBitsRemainPendingWithoutRepair(t *testing.T) {
	fixture, input, bind := invocationFixture(t)
	path := fixture.Documents[0]
	t.Cleanup(func() { _ = os.Chmod(path, 0o600) })
	result, err := invokeFixture(t.Context(), input, subprocess.CommandExecutor{}, subprocess.CommandAttemptRequest{Command: "/bin/sh", Args: []string{"-c", `/bin/chmod 000 "$1"; /bin/chmod 644 "$2"`, "protected", path, fixture.Documents[1]}}, bind, nil)
	if err == nil {
		t.Fatal("changed owner permission bits were treated as qualified")
	}
	if process, present := result.Process(); !present || !process.Attempt().Succeeded() {
		t.Fatal("metadata failure discarded mechanical command completion")
	}
	if record, present := result.Protection(); !present || record.Settlement() == SettlementCleared {
		t.Fatal("unqualified owner metadata discharged the fence")
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0 {
		t.Fatal("safety repaired owner bits outside its authority", err)
	}
	if info, err := os.Stat(fixture.Documents[1]); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatal("unqualified peer prevented an independently authorized restriction", err)
	}
	if rows, err := fixture.Store.LoadPendingProtections(t.Context()); err != nil || len(rows) != 1 {
		t.Fatal("unqualified metadata lost its durable obligation", err)
	}
}
