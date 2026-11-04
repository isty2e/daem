package physicalwork

import (
	"testing"

	"github.com/isty2e/daem/internal/effect/workmeasure"
)

func TestJointGeneralExecutionKeepsControlAndHostAllocationsDisjoint(t *testing.T) {
	budget := jointExecutionReadyBudget(t)
	consume, err := workmeasure.NewArtifactWork(budget.RemainingEntries()-10, budget.RemainingBytes()-20)
	if err != nil {
		t.Fatal(err)
	}
	if err := budget.AdmitTree(consume); err != nil {
		t.Fatal(err)
	}
	hostWork, err := workmeasure.NewArtifactWork(3, 5)
	if err != nil {
		t.Fatal(err)
	}
	if err := budget.ReserveGeneralDirectoryObservation(hostWork); err != nil {
		t.Fatal(err)
	}
	if err := budget.ReserveGeneralPathComponents(7); err != nil {
		t.Fatal(err)
	}
	entries, bytes := budget.RemainingEntries(), budget.RemainingBytes()
	host, err := budget.BeginGeneralExecutionRetainingControl()
	if err != nil {
		t.Fatal(err)
	}
	if budget.RemainingEntries() != entries || budget.RemainingBytes() != bytes {
		t.Fatal("joint transfer exhausted or replenished control capacity")
	}
	controlWork, err := workmeasure.NewArtifactWork(entries, bytes)
	if err != nil {
		t.Fatal(err)
	}
	if err := budget.AdmitTree(controlWork); err != nil {
		t.Fatal("control observation lost its remaining allocation", err)
	}
	if err := budget.AdmitTree(hostWork); err == nil {
		t.Fatal("control observation borrowed transferred host capacity")
	}
	reservedHost, err := workmeasure.NewArtifactWork(4, 5)
	if err != nil {
		t.Fatal(err)
	}
	if err := host.AdmitTree(reservedHost); err != nil {
		t.Fatal("control exhaustion consumed the disjoint host reservation", err)
	}
	if err := host.AdmitTree(hostWork); err == nil {
		t.Fatal("host execution borrowed control capacity")
	}
	if err := host.AdmitPathComponents(7); err != nil {
		t.Fatal(err)
	}
	if err := host.AdmitPathComponents(1); err == nil {
		t.Fatal("host execution admitted unreserved path work")
	}
	if _, err := budget.BeginGeneralExecutionRetainingControl(); err == nil {
		t.Fatal("joint host reservation transferred twice")
	}
	if _, _, err := budget.BeginGeneralExecution(); err == nil {
		t.Fatal("legacy transfer reused a joint host reservation")
	}
	if err := budget.ReserveGeneralPathComponents(1); err == nil {
		t.Fatal("general reservation remained open after transfer")
	}
}

func TestJointGeneralExecutionPreservesLifecycleAdmission(t *testing.T) {
	pending := NewCaptureBudget(nil)
	if _, err := pending.BeginGeneralExecutionRetainingControl(); err == nil {
		t.Fatal("pending removal census was accepted as admitted empty")
	}
	budget, err := NewBudget(0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := budget.BeginGeneralExecutionRetainingControl(); err == nil {
		t.Fatal("joint execution started before removal cleanup transfer")
	}
	if err := budget.ConcludeCleanupLifecycle(); err != nil {
		t.Fatal(err)
	}
	if _, err := budget.BeginGeneralExecutionRetainingControl(); err == nil {
		t.Fatal("joint execution started with pending scratch cleanup")
	}
	if err := budget.ConcludeScratchCleanupNotApplicable(); err != nil {
		t.Fatal(err)
	}
	if _, err := budget.BeginGeneralExecutionRetainingControl(); err == nil {
		t.Fatal("joint execution started with pending retirement")
	}
	if err := budget.ConcludeRetirementNotApplicable(); err != nil {
		t.Fatal(err)
	}
	if _, err := budget.BeginGeneralExecutionRetainingControl(); err != nil {
		t.Fatal(err)
	}
	legacy := jointExecutionReadyBudget(t)
	if _, _, err := legacy.BeginGeneralExecution(); err != nil {
		t.Fatal(err)
	}
	if _, err := legacy.BeginGeneralExecutionRetainingControl(); err == nil {
		t.Fatal("joint transfer reused a legacy host reservation")
	}
}

func jointExecutionReadyBudget(t *testing.T) *Budget {
	t.Helper()
	budget, err := NewBudget(0)
	if err != nil {
		t.Fatal(err)
	}
	if err := budget.ConcludeCleanupLifecycle(); err != nil {
		t.Fatal(err)
	}
	if err := budget.ConcludeScratchCleanupNotApplicable(); err != nil {
		t.Fatal(err)
	}
	if err := budget.ConcludeRetirementNotApplicable(); err != nil {
		t.Fatal(err)
	}
	return budget
}
