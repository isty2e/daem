package recoverygate

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/effect/physicalwork"
	daempaths "github.com/isty2e/daem/internal/paths"
)

type exhaustedBarrierBudget struct{ cause error }

func (budget exhaustedBarrierBudget) AdmitPathComponents(int) error { return budget.cause }

func TestBoundedBarrierPreservesPolicyAndCallerAllocation(t *testing.T) {
	paths, err := daempaths.Resolve(filepath.Join(t.TempDir(), "daem.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := Observe(t.Context(), paths); err != nil {
		t.Fatal(err)
	}
	if err := ObserveBounded(t.Context(), paths, physicalwork.NewCaptureBudget(nil)); err != nil {
		t.Fatal(err)
	}
	cause := errors.New("shared traversal allocation exhausted")
	if err := ObserveBounded(t.Context(), paths, exhaustedBarrierBudget{cause: cause}); !errors.Is(err, cause) {
		t.Fatal("barrier replaced its caller allocation", err)
	}
	if err := ObserveBounded(t.Context(), paths, nil); err == nil {
		t.Fatal("bounded barrier admitted absent allocation")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := ObserveBounded(ctx, paths, physicalwork.NewCaptureBudget(nil)); !errors.Is(err, context.Canceled) {
		t.Fatal("bounded observation hid cancellation", err)
	}
}
