package rootedpath

import (
	"strings"
	"testing"
)

type pathOnlyTestBudget struct {
	components int
}

func (budget *pathOnlyTestBudget) AdmitPathComponents(count int) error {
	budget.components += count
	return nil
}

func TestCommitCapabilityPathOnlyBudgetAdmitsPathOnlyWork(t *testing.T) {
	root := t.TempDir()
	captured, err := CaptureRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer captured.Close()
	authority, err := captured.Authority()
	if err != nil {
		t.Fatal(err)
	}
	relative, err := NewRelativeDestination("state/state.json")
	if err != nil {
		t.Fatal(err)
	}
	destination, err := authority.Bind(relative)
	if err != nil {
		t.Fatal(err)
	}
	budget := &pathOnlyTestBudget{}
	capability, err := captured.AcquireBounded(destination, 64, budget)
	if err != nil {
		t.Fatal(err)
	}
	defer capability.Close()

	before := budget.components
	if err := capability.AdmitPhysicalWork(3, 0, 0); err != nil {
		t.Fatalf("path-only physical work = %v, want admitted through path budget", err)
	}
	if budget.components != before+3 {
		t.Fatalf("path budget charged %d components, want %d", budget.components-before, 3)
	}
	if err := capability.AdmitPhysicalWork(0, 1, 0); err == nil ||
		!strings.Contains(err.Error(), "lacks physical-work capacity") {
		t.Fatalf("entry work against path-only budget = %v, want physical-work refusal", err)
	}
	if err := capability.AdmitPhysicalWork(0, 0, 8); err == nil ||
		!strings.Contains(err.Error(), "lacks physical-work capacity") {
		t.Fatalf("byte work against path-only budget = %v, want physical-work refusal", err)
	}
}
