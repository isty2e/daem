package publication

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/pathauthority/pathtest"
	journalrecord "github.com/isty2e/daem/internal/effect/journal/record"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	ownershipmutation "github.com/isty2e/daem/internal/effect/mutation/ownership"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/output/ownership"
	"github.com/isty2e/daem/internal/realization"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/test/outputtest"
)

func claimAuthorityDraftForTest(t *testing.T) *journalrecord.OrdinaryDraft {
	t.Helper()
	mutation := normalizedPathCapture(t, journalrecord.CaptureCreate, target.ScopeGlobal, outputtest.Parse(t, "~/.agents/skills/capture"), "", realization.PathProjectionDirectory)
	root := t.TempDir()
	provenance, err := rootedpath.NewAuthorityProvenance(root, testContentHashString("claim root"), testContentHashString("claim mount"))
	if err != nil {
		t.Fatal(err)
	}
	demands, err := recovery.NewRemovalDemandSet(nil)
	if err != nil {
		t.Fatal(err)
	}
	draft, err := journalrecord.NewOrdinaryDraft("claim-coverage", time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC), durable.EmptySnapshot(), durable.EmptySnapshot(), provenance, []journalrecord.CaptureMutation{mutation}, demands)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := draft.BeginEntry(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := draft.SupplyBefore(recovery.BeforePathState{}); err != nil {
		t.Fatal(err)
	}
	physical := filepath.Join(root, "capture")
	if err := draft.SupplyGlobalBinding(journalrecord.NewGlobalBinding(physical, provenance)); err != nil {
		t.Fatal(err)
	}
	if err := draft.CloseEntries(); err != nil {
		t.Fatal(err)
	}
	if err := draft.SupplyRemovalIntents(nil); err != nil {
		t.Fatal(err)
	}
	address, err := ownership.NewManagedAddressFromLegacy(pathtest.Exact(physical), "")
	if err != nil {
		t.Fatal(err)
	}
	transition, err := ownershipmutation.NewAcquireTransition(address, documentClaimsForTest(t).owner, "claim-coverage")
	if err != nil {
		t.Fatal(err)
	}
	if err := draft.BeginClaimCoverage([]ownershipmutation.ClaimTransition{transition}, nil); err != nil {
		t.Fatal(err)
	}
	return draft
}
