package journal

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/effect/journal/recovery"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/output"
)

func TestCaptureRemovalIntentsRejectsPathDepthBeforePublication(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("canonicalize test root: %v", err)
	}
	intent := removalPlanTestIntent(t, root, nil)
	demands, err := recovery.NewRemovalDemandSet([]recovery.RemovalDemand{intent.Demand()})
	if err != nil {
		t.Fatalf("construct removal demand set: %v", err)
	}
	components := make([]string, recovery.MaximumPhysicalPathDepth+1)
	for index := range components {
		components[index] = "d"
	}
	alias := filepath.Join(filepath.Dir(root), "removal-depth-alias")
	if err := os.Symlink(root, alias); err != nil {
		t.Fatalf("create alias: %v", err)
	}
	deepPath := filepath.Join(append([]string{alias}, components...)...)

	_, err = captureRemovalIntents(
		context.Background(),
		demands,
		func(output.Destination) (string, error) { return deepPath, nil },
	)
	if err == nil || !strings.Contains(err.Error(), "physical path depth") {
		t.Fatalf("captureRemovalIntents error = %v, want path-depth rejection", err)
	}
}

func TestCaptureRemovalIntentsRejectsDeepPhysicalAliasBeforePublication(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("canonicalize test root: %v", err)
	}
	intent := removalPlanTestIntent(t, root, nil)
	demands, err := recovery.NewRemovalDemandSet([]recovery.RemovalDemand{intent.Demand()})
	if err != nil {
		t.Fatalf("construct removal demand set: %v", err)
	}
	components := make([]string, recovery.MaximumPhysicalPathDepth+1)
	for index := range components {
		components[index] = "d"
	}
	deepParent := filepath.Join(append([]string{root}, components...)...)
	if err := os.MkdirAll(deepParent, 0o700); err != nil {
		t.Fatalf("create deep physical parent: %v", err)
	}
	alias := filepath.Join(filepath.Dir(root), "short-removal-alias")
	if err := os.Symlink(deepParent, alias); err != nil {
		t.Fatalf("create short removal alias: %v", err)
	}

	_, err = captureRemovalIntents(
		context.Background(),
		demands,
		func(output.Destination) (string, error) {
			return filepath.Join(alias, "entry"), nil
		},
	)
	if err == nil || !strings.Contains(err.Error(), "physical path depth") {
		t.Fatalf("captureRemovalIntents error = %v, want physical-depth rejection", err)
	}
}

func TestAllocateLogicalRemovalNamesUsesOneBoundObservationPerPair(t *testing.T) {
	var observed []string
	calls := 0
	names, err := allocateLogicalRemovalNames(
		t.Context(),
		make(map[string]struct{}),
		func(_ context.Context, candidate mutationfs.LogicalRemovalNames) (bool, error) {
			calls++
			observed = []string{candidate.Residue(), candidate.Cleanup()}
			return false, nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("slot observation calls = %d, want 1", calls)
	}
	want := []string{names.Residue(), names.Cleanup()}
	if len(observed) != len(want) || observed[0] != want[0] || observed[1] != want[1] {
		t.Fatalf("observed slot pair = %q, want %q", observed, want)
	}
}

func TestAllocateLogicalRemovalNamesRetriesOccupiedPairAsOneObservation(t *testing.T) {
	calls := 0
	names, err := allocateLogicalRemovalNames(
		t.Context(),
		make(map[string]struct{}),
		func(_ context.Context, _ mutationfs.LogicalRemovalNames) (bool, error) {
			calls++
			return calls == 1, nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("slot observation calls = %d, want 2", calls)
	}
	if names.Residue() == "" || names.Cleanup() == "" {
		t.Fatal("allocated names are empty")
	}
}
