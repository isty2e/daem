package recoveryoperation

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/statefile"
	journalqualification "github.com/isty2e/daem/internal/effect/journal/qualification"
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
			operation, err := OpenCapturedNativeRecovery(t.Context(), captured, input.options.StateCodec, input.options.ManifestRoot, input.budget, journalqualification.NativeRecoveryOptions{
				Filesystem: input.options.Filesystem, OperationAuthority: input.options.OperationAuthority,
				Registry: registry, Codecs: input.options.Codecs,
				State: func(ctx context.Context) (durable.Snapshot, error) {
					return statefile.LoadOptionalWithOptions(ctx, input.paths.StatefilePath, statefile.ReadOptions{Codec: input.options.StateCodec, PhysicalPathBudget: input.budget})
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
			plan := operation.Plan()
			aliases, err := plan.DocumentWriteAliases()
			if err != nil {
				t.Fatal("live operation could not freeze document write facts", err)
			}
			if err := operation.Close(); err != nil {
				t.Fatal(err)
			}
			closedAliases, err := plan.DocumentWriteAliases()
			if err != nil || !reflect.DeepEqual(aliases, closedAliases) || plan.Classification() != recovery.ClassificationCleanBefore {
				t.Fatal("closing live recovery invalidated immutable plan facts", err)
			}
			if len(operation.Documents()) != 0 || operation.InitialAssessment().CleanBefore() {
				t.Fatal("closed recovery exposed live documents or assessment")
			}
			input.requireUnchanged(t)
			if _, err := os.Stat(residue); err != nil {
				t.Fatal("observation changed inert legacy residue", err)
			}
		})
	}
}
