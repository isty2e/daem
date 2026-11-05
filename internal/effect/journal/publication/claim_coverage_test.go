package publication

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	journalrecord "github.com/isty2e/daem/internal/effect/journal/record"
	"github.com/isty2e/daem/internal/output"
)

func TestClaimCoveragePreservesEntryLocalEffectErrorOrder(t *testing.T) {
	sentinel := errors.New("later resolver failure")
	for _, observation := range []string{"resolver failure", "provisional observation"} {
		t.Run(observation, func(t *testing.T) {
			draft := claimAuthorityDraftForTest(t)
			calls, requests := 0, 0
			resolver := func(output.Destination) (string, error) {
				calls++
				if observation == "resolver failure" || calls > 1 {
					return "", sentinel
				}
				return filepath.Join(t.TempDir(), "missing", "config"), nil
			}
			next := func(ctx context.Context) (journalrecord.ClaimAuthorityDemand, bool, error) {
				requests++
				if requests > 1 {
					return journalrecord.ClaimAuthorityDemand{}, false, sentinel
				}
				return draft.NextClaimAuthority(ctx)
			}
			err := captureClaimAuthorities(t.Context(), resolver, next, draft.BindClaimAuthority)
			if err == nil || calls != 1 || requests != 1 {
				t.Fatalf("coverage inspected later entries before the first observation result: calls=%d requests=%d err=%v", calls, requests, err)
			}
			if errors.Is(err, sentinel) != (observation == "resolver failure") {
				t.Fatalf("entry-local failure precedence changed: %v", err)
			}
			result, finishErr := draft.Finish()
			if entries, entryErr := result.RecordedEntries(); finishErr == nil || entryErr == nil || entries != nil {
				t.Fatal("failed capture supplied completed record evidence")
			}
		})
	}
}
