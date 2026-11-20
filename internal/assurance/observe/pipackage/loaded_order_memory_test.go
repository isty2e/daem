package pipackage_test

import (
	"bytes"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/assurance/observe/relation/piorder"

	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/target"
)

func TestLoadedPiOrderRejectsOversizedInputBeforeCopying(t *testing.T) {
	root := t.TempDir()
	document := aggregate.ExistingDocument(bytes.Repeat([]byte(" "), 2*piorder.MaximumSettingsBytes))
	input, _ := loadedOrderFixture(t, target.ScopeProject, filepath.Join(root, ".pi"), root, root,
		[]orderSpec{{id: "a", source: "npm:a@1"}, {id: "b", source: "npm:b@1"}}, document)
	if observation, err := piorder.ReconstructOrder(input); err == nil || observation.Validate() == nil {
		t.Fatal("oversized input did not return a refusal without a valid observation")
	}
	result := testing.Benchmark(func(b *testing.B) {
		for range b.N {
			if _, err := piorder.ReconstructOrder(input); err == nil {
				b.Fatal("oversized loaded document was accepted")
			}
		}
	})
	allocated := result.AllocedBytesPerOp()
	t.Logf("oversized rejection: %d B/op", allocated)
	if result.N == 0 || allocated >= int64(piorder.MaximumSettingsBytes) {
		t.Fatalf("oversized rejection allocated %d bytes per call; content was copied before the existing byte limit", allocated)
	}
}
