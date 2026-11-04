package record

import (
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/assurance/pathauthority"
	"github.com/isty2e/daem/internal/assurance/pathauthority/pathtest"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	ownershipmutation "github.com/isty2e/daem/internal/effect/mutation/ownership"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/output/ownership"
)

type ordinaryPromotionFixture struct {
	record      Ordinary
	content     []byte
	fingerprint string
	directory   string
	intent      ownership.ProvisionalAcquireIntent
	transition  ownershipmutation.ClaimTransition
}

func ordinaryPromotionRecordFixture(t *testing.T) ordinaryPromotionFixture {
	t.Helper()
	root := t.TempDir()
	namespace := filepath.Join(root, "global")
	candidate := filepath.Join(namespace, "AGÉNTS.md")
	provisional, err := pathauthority.NewProvisional(candidate, pathtest.DarwinCaseSensitive(candidate).Witness(), namespace, pathtest.DarwinCaseSensitive(namespace).Witness())
	if err != nil {
		t.Fatal(err)
	}
	owner, err := stateauthority.New(pathtest.Exact(filepath.Join(root, "state.json")), filepath.Join(root, "daem.toml"))
	if err != nil {
		t.Fatal(err)
	}
	destination, err := output.Parse("~/.codex/AGENTS.md")
	if err != nil {
		t.Fatal(err)
	}
	intent, err := ownership.NewProvisionalAcquireIntentFromLegacy(destination, "", provisional, owner, testOperationID)
	if err != nil {
		t.Fatal(err)
	}
	address, err := ownership.NewManagedAddressFromLegacy(pathtest.DarwinCaseSensitive(candidate), "")
	if err != nil {
		t.Fatal(err)
	}
	transition, err := ownershipmutation.NewAcquireTransitionFromIntent(intent, address)
	if err != nil {
		t.Fatal(err)
	}
	journal := recoveryJournalFor(globalAcquireRecoveryEntry(t))
	journal.ProvisionalAcquires, err = recoveryProvisionalAcquireIntents([]ownership.ProvisionalAcquireIntent{intent})
	if err != nil {
		t.Fatal(err)
	}
	content, err := marshalRecoveryJournalWithStateContentForPromotion(journal, testStateCodec())
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := DecodeOrdinarySnapshot(t.Context(), privateJournalSnapshotForTest(t, content), FileName, testStateCodec())
	if err != nil {
		t.Fatal(err)
	}
	record, present := loaded.Ordinary()
	if !present {
		t.Fatal("ordinary decoder returned another family")
	}
	return ordinaryPromotionFixture{record: record, content: content, fingerprint: Fingerprint(content), directory: testOperationDir, intent: intent, transition: transition}
}
