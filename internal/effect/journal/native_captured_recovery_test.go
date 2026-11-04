package journal

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/statefile"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	ownershipstore "github.com/isty2e/daem/internal/output/ownership/store"
	"github.com/isty2e/daem/internal/target"
)

func TestCapturedNativeRecoveryRetainsInertLegacyInventory(t *testing.T) {
	for _, create := range []bool{true, false} {
		t.Run(map[bool]string{true: "new-document", false: "existing-document"}[create], func(t *testing.T) {
			input := nativeCaptureInput(t, create, target.ScopeProject)
			identity, legacy := captureInventoryJournal(t, input.paths.RecoveryDir, "previous-ordinary-operation")
			residue := filepath.Join(input.paths.RecoveryDir, identity.GCName())
			if err := os.Rename(legacy.Directory, residue); err != nil {
				t.Fatal(err)
			}
			captured, err := input.capture(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			registry, err := ownershipstore.NewRecoveryReader(input.registry)
			if err != nil {
				t.Fatal(err)
			}
			operation, err := OpenCapturedNativeRecovery(t.Context(), captured, input.options.StateCodec, NativeRecoveryOptions{
				Filesystem: input.options.Filesystem, OperationAuthority: input.options.OperationAuthority,
				Registry: registry, Codecs: input.options.Codecs,
				State: func(ctx context.Context) (durable.Snapshot, error) {
					return statefile.LoadOptionalWithOptions(ctx, input.paths.StatefilePath, statefile.ReadOptions{Codec: input.options.StateCodec, PhysicalPathBudget: input.batch.budget})
				},
			})
			if err != nil {
				t.Fatalf("admitted inert legacy residue prevented native settlement: %v", err)
			}
			defer func() {
				if err := operation.Close(); err != nil {
					t.Error(err)
				}
			}()
			if operation.Plan().Classification() != recovery.ClassificationCleanBefore {
				t.Fatal("unchanged actual Before was not clean-before")
			}
			if requests, err := operation.OrdinaryPhysicalAuthorityRequests(); err != nil || len(requests) != 0 {
				t.Fatal("document-only recovery required an ordinary resolver", err)
			}
			for _, document := range operation.Documents() {
				if err := document.Comparison().Validate(); err != nil {
					t.Fatal("live recovery lost its reconstructed comparison", err)
				}
				if err := document.Namespace().Observation().Validate(); err != nil {
					t.Fatal("live recovery lost its namespace evidence", err)
				}
			}
			input.requireUnchanged(t)
			if _, err := os.Stat(residue); err != nil {
				t.Fatal("observation changed inert legacy residue", err)
			}
		})
	}
}
