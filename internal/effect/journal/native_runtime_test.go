package journal

import (
	"bytes"
	"context"
	"os"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/statefile"
	ownershipstore "github.com/isty2e/daem/internal/output/ownership/store"
	"github.com/isty2e/daem/internal/target"
)

func TestNativeCapturedRuntimeRequiresWholeCleanPhaseForRetirement(t *testing.T) {
	for _, phase := range []string{"before", "after", "partial", "third"} {
		t.Run(phase, func(t *testing.T) {
			input := nativeCaptureInput(t, false, target.ScopeProject)
			captured, err := input.capture(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			retiring, err := PrepareNativeJournalRetirement(t.Context(), captured, input.options.Filesystem, input.options.StateCodec)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = retiring.Close() })
			if phase == "after" || phase == "partial" {
				expected, mode := captured.Documents()[0].Expected()
				requireWholeGroupWrite(t, input.host, expected.Content(), mode.FileMode())
			}
			if phase == "after" {
				state, err := input.options.StateCodec.Encode(input.after)
				if err != nil {
					t.Fatal(err)
				}
				requireWholeGroupWrite(t, input.paths.StatefilePath, state, 0o600)
			}
			if phase == "third" {
				requireWholeGroupWrite(t, input.host, []byte("model='external'\nunmanaged=1\n"), 0o644)
			}
			reader, err := ownershipstore.NewRecoveryReader(input.registry)
			if err != nil {
				t.Fatal(err)
			}
			assessment, err := captured.AssessRecovery(t.Context(), NativeRecoveryOptions{
				Filesystem: input.options.Filesystem, OperationAuthority: input.options.OperationAuthority,
				State: func(ctx context.Context) (durable.Snapshot, error) {
					return statefile.LoadOptionalWithOptions(ctx, input.paths.StatefilePath, statefile.ReadOptions{Codec: input.options.StateCodec, PhysicalPathBudget: input.batch.budget})
				},
				Registry: reader, Codecs: input.options.Codecs,
			})
			if phase == "third" {
				if err == nil || assessment.valid {
					t.Fatal("third-value document produced usable recovery evidence", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if phase == "partial" {
				if !assessment.RequiresRollback() || retiring.ExecuteWithGate(t.Context(), assessment, nil) == nil {
					t.Fatal("unsettled host/state phase was allowed to retire")
				}
				if _, err := os.Stat(captured.capture.JournalPath); err != nil {
					t.Fatal("refused retirement lost its recovery evidence", err)
				}
				return
			}
			if phase == "before" && !assessment.CleanBefore() || phase == "after" && !assessment.CleanAfter() {
				t.Fatal("recovery phase does not reflect actual host, state and ownership")
			}
			beforeHost, err := os.ReadFile(input.host)
			if err != nil {
				t.Fatal(err)
			}
			if err := retiring.ExecuteWithGate(t.Context(), assessment, nil); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(captured.capture.Directory); !os.IsNotExist(err) {
				t.Fatal("native journal remained active after clean retirement", err)
			}
			afterHost, err := os.ReadFile(input.host)
			if err != nil || !bytes.Equal(beforeHost, afterHost) {
				t.Fatal("journal retirement changed host content", err)
			}
			if err := retiring.ExecuteWithGate(t.Context(), assessment, nil); err == nil {
				t.Fatal("native retirement continuation was consumed twice")
			}
		})
	}
}
