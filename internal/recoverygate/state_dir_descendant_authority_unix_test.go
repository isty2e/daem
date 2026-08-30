//go:build darwin || linux

package recoverygate

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	storagecommit "github.com/isty2e/daem/internal/effect/storage/commit"
)

func TestStateDirDescendantReservationChargesBeforeBindingAndConsumesExactly(t *testing.T) {
	root := t.TempDir()
	stateDir := filepath.Join(root, "state\ncontrol")
	statePath := filepath.Join(stateDir, "state.json")
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	budget := &stateDirRecordingBudget{limit: 1 << 20}
	authority, err := CaptureStateDirBounded(t.Context(), stateDir, 256, budget)
	if err != nil {
		t.Fatal(err)
	}
	beforeReservation := budget.used
	operation, err := authority.ReserveOperation(0, 0, false, statePath, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	reservation, err := operation.TakeDescendant()
	if err != nil {
		t.Fatal(err)
	}
	if budget.used <= beforeReservation {
		t.Fatalf("reservation did not charge planning budget: before=%d after=%d", beforeReservation, budget.used)
	}
	reservedAt := budget.used

	bound, err := reservation.Bind(t.Context())
	if err != nil {
		t.Fatalf("bind control-bearing StateDir descendant: %v", err)
	}
	defer bound.Close()
	if budget.used != reservedAt {
		t.Fatalf("binding charged planning budget after reservation: before=%d after=%d", reservedAt, budget.used)
	}
	if err := bound.Validate(t.Context()); err != nil {
		t.Fatalf("validate bound StateDir descendant: %v", err)
	}
	entry := bound.Entry()
	if entry == nil {
		t.Fatal("bound StateDir descendant has no entry authority")
	}
	capability, err := entry.Acquire()
	if err != nil {
		t.Fatal(err)
	}
	_, err = storagecommit.CaptureRootedEntryIdentity(t.Context(), capability)
	if !errors.Is(err, os.ErrNotExist) {
		_ = capability.Close()
		t.Fatalf("capture missing statefile identity: %v", err)
	}
	if err := (storagecommit.Adapter{}).CreateRootedFile(
		t.Context(),
		capability,
		[]byte("state\n"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	reserved, ok := bound.budget.(*stateDirReservedWorkBudget)
	if !ok {
		t.Fatalf("execution budget = %T", bound.budget)
	}
	reserved.mu.Lock()
	remaining := reserved.remaining
	reserved.mu.Unlock()
	if remaining != (stateDirPhysicalWork{}) {
		t.Fatalf("reserved physical work remaining = %#v, want exact consumption", remaining)
	}
	if budget.used != reservedAt {
		t.Fatalf("execution bypassed reservation and charged planning budget: before=%d after=%d", reservedAt, budget.used)
	}
}

func TestStateDirDescendantReservationRejectsInsufficientOperationBudgetBeforeBinding(t *testing.T) {
	root := t.TempDir()
	stateDir := filepath.Join(root, ".daem")
	statePath := filepath.Join(stateDir, "state.json")
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	budget := &stateDirRecordingBudget{limit: 1 << 20}
	authority, err := CaptureStateDirBounded(t.Context(), stateDir, 256, budget)
	if err != nil {
		t.Fatal(err)
	}
	budget.limit = budget.used
	if _, err := authority.ReserveOperation(0, 0, false, statePath, 1, 1); err == nil {
		t.Fatal("under-budget StateDir descendant reservation succeeded")
	}
	if _, statErr := os.Stat(statePath); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("reservation created statefile before binding: %v", statErr)
	}
}

var _ rootedpath.PhysicalTraversalBudget = (*stateDirRecordingBudget)(nil)
