package journal

import (
	"testing"

	journalrecord "github.com/isty2e/daem/internal/effect/journal/record"
)

func TestNativeRecoveryDocumentRefusesZeroClosedAndInvalidIndexes(t *testing.T) {
	fixture := nativeJournalFixture(t)
	records, err := loadDocuments(fixture.journal, t.Context(), fixture.read)
	if err != nil {
		t.Fatal(err)
	}
	operation := &NativeRecoveryOperation{qualified: nativeQualifiedDocumentRecords{records: records}}
	closed := *operation
	closed.closed = true
	for name, document := range map[string]NativeRecoveryDocument{
		"zero": {}, "closed": {operation: &closed},
		"negative": {operation: operation, index: -1}, "past end": {operation: operation, index: 1},
	} {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if value := recover(); value != nil {
					t.Fatalf("invalid document panicked: %v", value)
				}
			}()
			if _, present, err := document.Removal(); err == nil || present {
				t.Fatal("invalid document supplied removal facts")
			}
			if document.Comparison().Validate() == nil {
				t.Fatal("invalid document supplied a comparison")
			}
			if document.Restoration().Validate() == nil {
				t.Fatal("invalid document supplied a restoration")
			}
			current, mode := document.Current()
			if current.Exists() || current.ContentSize() != 0 || mode != nil {
				t.Fatal("invalid document supplied current content")
			}
			if document.Namespace().Observation().Validate() == nil {
				t.Fatal("invalid document supplied namespace evidence")
			}
		})
	}
	if intents, err := (NativeRecoveryPlan{}).OrdinaryProvisionalAcquires(); err == nil || len(intents) != 0 {
		t.Fatal("zero recovery plan supplied valid empty acquires")
	}
}

func TestNativeRecoveryDocumentFailedRemovalIsNotPresent(t *testing.T) {
	fixture := nativeJournalFixture(t)
	records, err := loadDocuments(fixture.journal, t.Context(), fixture.read)
	if err != nil {
		t.Fatal(err)
	}
	operation := &NativeRecoveryOperation{
		plan:      NativeRecoveryPlan{journal: journalrecord.Native{}},
		qualified: nativeQualifiedDocumentRecords{records: records},
	}
	if _, present, err := (NativeRecoveryDocument{operation: operation}).Removal(); err == nil || present {
		t.Fatal("failed removal conversion supplied a present result")
	}
}

func TestNativePromotionRefusesUnusableCapturedFacts(t *testing.T) {
	if !(CapturedNativeJournal{}).NeedsPromotion() {
		t.Fatal("unavailable promotion facts allowed retirement preparation")
	}
}
