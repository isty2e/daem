package statefile

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	aggregatecodec "github.com/isty2e/daem/internal/realization/aggregate/codec"
	"github.com/isty2e/daem/internal/target"
)

func TestSelectedAndDefaultStatefileReadersPreserveNativeState(t *testing.T) {
	codec, err := NewNativeCodec(aggregatecodec.Catalog().Contributions())
	if err != nil {
		t.Fatal(err)
	}
	state, err := durable.NewSnapshot(durable.SnapshotInput{ManagedPaths: []durable.ManagedPathState{nativeDocumentCodecBaseline(t, target.TargetPi, target.ScopeGlobal, 0o600)}})
	if err != nil {
		t.Fatal(err)
	}
	content, err := codec.Encode(state)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadOptionalWithOptions(t.Context(), path, ReadOptions{Codec: codec, PhysicalPathBudget: recovery.NewPhysicalPathBudget()})
	if err != nil || !loaded.Equal(state) {
		t.Fatalf("selected filesystem reader lost complete native state: %v", err)
	}
	if snapshot, err := LoadOptional(t.Context(), path); err != nil || !snapshot.Equal(state) {
		t.Fatalf("default reader lost native whole-file state: %v", err)
	}
	actual, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(actual, content) {
		t.Fatalf("state observation rewrote selected state: %v", err)
	}
}

func TestSelectedStatefileReaderKeepsMissingRetiredAndFailureSemantics(t *testing.T) {
	codec, err := NewNativeCodec(aggregatecodec.Catalog().Contributions())
	if err != nil {
		t.Fatal(err)
	}
	options := ReadOptions{Codec: codec, PhysicalPathBudget: recovery.NewPhysicalPathBudget()}
	path := filepath.Join(t.TempDir(), "state.json")
	if snapshot, err := LoadOptionalWithOptions(t.Context(), path, options); err != nil || !snapshot.Equal(durable.EmptySnapshot()) {
		t.Fatalf("missing selected state: %v", err)
	}
	retired := []byte(`{"version":7,"managed_paths":[],"managed_aggregate_contributions":[],"pending_carrier_installs":[],"pending_carrier_removals":[],"managed_carrier_claims":[],"delegate_attempts":[],"host_route_attempts":[]}`)
	if err := os.WriteFile(path, retired, 0o600); err != nil {
		t.Fatal(err)
	}
	if snapshot, err := LoadOptionalWithOptions(t.Context(), path, options); err != nil || !snapshot.Equal(durable.EmptySnapshot()) {
		t.Fatalf("exact empty retirement: %v", err)
	}
	for _, content := range [][]byte{[]byte("malformed"), []byte(`{"version":7,"managed_paths":null}`), []byte(`{"version":9999}`)} {
		if err := os.WriteFile(path, content, 0o600); err != nil {
			t.Fatal(err)
		}
		if snapshot, err := LoadOptionalWithOptions(t.Context(), path, options); err == nil || !snapshot.Equal(durable.EmptySnapshot()) {
			t.Fatal("malformed or unsupported selected state became absence/partial state")
		}
	}
	sentinel := errors.New("path budget exhausted")
	options.PhysicalPathBudget = selectedStateBudget{sentinel}
	if _, err := LoadOptionalWithOptions(t.Context(), path, options); !errors.Is(err, sentinel) {
		t.Fatalf("budget error lost its cause: %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := LoadOptionalWithOptions(ctx, path, options); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation did not precede path observation: %v", err)
	}
	if _, err := LoadOptionalWithOptions(t.Context(), path, ReadOptions{}); err == nil {
		t.Fatal("selected reader accepted missing dependencies")
	}
}

type selectedStateBudget struct{ err error }

func (budget selectedStateBudget) AdmitPathComponents(int) error { return budget.err }
