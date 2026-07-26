package refresh

import (
	"context"
	"strings"
	"testing"

	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/test/testkit/metadatatx"
)

func TestRefreshPlanningFailsClosedOnInterruptedMetadataTransaction(t *testing.T) {
	manifestPath := writeNoObserverRefreshFixture(t)
	paths, err := daempaths.Resolve(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	metadatatx.WriteInterrupted(t, paths.StateDir)

	t.Run("dry-run", func(t *testing.T) {
		result, err := PlanDryRun(context.Background(), CommandInput{
			ManifestPath: manifestPath,
			ExtensionID:  "formatter",
		}, PlanOptions{CommandBuilder: syntheticRefreshCommandBuilder(t)})
		assertRefreshMetadataTransactionRefusal(t, result, err)
	})

	t.Run("write", func(t *testing.T) {
		prepared, err := PlanWrite(context.Background(), CommandInput{
			ManifestPath: manifestPath,
			ExtensionID:  "formatter",
		}, PlanOptions{CommandBuilder: syntheticRefreshCommandBuilder(t)})
		if prepared != nil {
			t.Cleanup(func() { _ = prepared.Close() })
			assertRefreshMetadataTransactionRefusal(t, prepared.Disclosure(), err)
			return
		}
		assertRefreshMetadataTransactionRefusal(t, CommandResult{}, err)
	})
}

func assertRefreshMetadataTransactionRefusal(
	t testing.TB,
	result CommandResult,
	err error,
) {
	t.Helper()
	if err == nil || !strings.Contains(err.Error(), "interrupted file-set transaction") {
		t.Fatalf("error = %v, want interrupted file-set transaction", err)
	}
	if result.ResultClass != "" && result.ResultClass != ResultRefused {
		t.Fatalf("result class = %q, want refused", result.ResultClass)
	}
	if result.ReasonCode != "" && result.ReasonCode != ReasonMutationAuthority {
		t.Fatalf("reason = %q, want %q", result.ReasonCode, ReasonMutationAuthority)
	}
}
