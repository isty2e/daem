package journal

import (
	"context"
	"fmt"
	"slices"

	journalnamespace "github.com/isty2e/daem/internal/effect/journal/namespace"

	journaldocument "github.com/isty2e/daem/internal/effect/journal/document"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	ownershipmutation "github.com/isty2e/daem/internal/effect/mutation/ownership"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/output/ownership"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/topology"
)

// CapturedNativeJournal can only be produced by publication followed by native16
// reload and qualification. Host namespaces remain borrowed from the containing
// operation. This value is capture evidence, not a lease or execution grant.
type CapturedNativeJournal struct {
	capture     CaptureResult
	journal     nativeJournalSnapshots
	qualified   nativeQualifiedDocumentRecords
	active      ActiveJournalAuthority
	budget      *physicalwork.Budget
	publication nativePublicationContext
	initialized bool
}

func (captured CapturedNativeJournal) Validate() error {
	if !captured.initialized {
		return fmt.Errorf("published native journal capture is required")
	}
	return captured.active.Validate()
}

func (captured CapturedNativeJournal) CaptureResult() CaptureResult  { return captured.capture }
func (captured CapturedNativeJournal) BeforeState() durable.Snapshot { return captured.journal.before }

func (captured CapturedNativeJournal) AfterState() durable.Snapshot { return captured.journal.after }

func (captured CapturedNativeJournal) Owner() stateauthority.Authority { return captured.journal.owner }

func (captured CapturedNativeJournal) ActiveJournalAuthority() ActiveJournalAuthority {
	return captured.active
}

// CapturedNativeDocument is a read-only projection of one published group. Its
// namespace borrows the original host roots; it never borrows the closed reload root.
type CapturedNativeDocument struct {
	captured *CapturedNativeJournal
	index    int
}

func (document CapturedNativeDocument) valid() bool {
	return document.captured != nil && document.captured.initialized && document.index >= 0 && document.index < len(document.captured.qualified.records.groups)
}

func (captured CapturedNativeJournal) Documents() []CapturedNativeDocument {
	if !captured.initialized {
		return nil
	}
	result := make([]CapturedNativeDocument, len(captured.qualified.records.groups))
	for index := range result {
		result[index] = CapturedNativeDocument{captured: &captured, index: index}
	}
	return result
}

func (document CapturedNativeDocument) Namespace() journalnamespace.DocumentNamespace {
	if !document.valid() {
		return journalnamespace.DocumentNamespace{}
	}
	return document.captured.qualified.namespaces[document.index]
}

func (document CapturedNativeDocument) Comparison() journaldocument.Comparison {
	if !document.valid() {
		return journaldocument.Comparison{}
	}
	return document.captured.qualified.records.groups[document.index].Comparison()
}

func (document CapturedNativeDocument) Original() (aggregate.Document, *recovery.PermissionMode) {
	if !document.valid() {
		return aggregate.Document{}, nil
	}
	image := document.captured.qualified.records.images[document.index].original
	return image.document, clonePermissionMode(image.mode)
}

func (document CapturedNativeDocument) Expected() (aggregate.Document, *recovery.PermissionMode) {
	if !document.valid() {
		return aggregate.Document{}, nil
	}
	image := document.captured.qualified.records.images[document.index].expected
	return image.document, clonePermissionMode(image.mode)
}

func (document CapturedNativeDocument) SelectedSubjects(ctx context.Context) ([]topology.SubjectID, error) {
	if !document.valid() || ctx == nil {
		return nil, fmt.Errorf("captured native document and context are required")
	}
	_, subjects, err := document.captured.journal.record.DocumentGroups[document.index].canonicalReferences(ctx)
	return subjects, err
}

func (document CapturedNativeDocument) Claims() (ownershipmutation.DocumentClaimTransition, bool) {
	if !document.valid() {
		return ownershipmutation.DocumentClaimTransition{}, false
	}
	known := document.captured.journal.record.DocumentGroups[document.index].ExclusiveClaims.Kind == "exact"
	return document.captured.qualified.records.claims[document.index], known
}

func (document CapturedNativeDocument) BindingIntents() []ownership.DocumentBindingIntent {
	if !document.valid() {
		return nil
	}
	return slices.Clone(document.captured.qualified.records.groups[document.index].BindingIntents())
}

type NativeDocumentProvisionalAcquire struct {
	acquire journaldocument.ProvisionalAcquire
}

func (acquire NativeDocumentProvisionalAcquire) Alias() aggregate.DocumentAddress {
	return acquire.acquire.Alias()
}

func (acquire NativeDocumentProvisionalAcquire) Intent() ownership.ProvisionalAcquireIntent {
	return acquire.acquire.Intent()
}

func (document CapturedNativeDocument) ProvisionalAcquires() []NativeDocumentProvisionalAcquire {
	if !document.valid() {
		return nil
	}
	values := document.captured.qualified.records.provisional[document.index]
	result := make([]NativeDocumentProvisionalAcquire, len(values))
	for index, value := range values {
		result[index] = NativeDocumentProvisionalAcquire{acquire: value}
	}
	return result
}

func (document CapturedNativeDocument) Removal() (recovery.DocumentRemoval, bool, error) {
	if !document.valid() {
		return recovery.DocumentRemoval{}, false, fmt.Errorf("captured native document is required")
	}
	record := document.captured.journal.record.DocumentGroups[document.index].Removal
	if record == nil {
		return recovery.DocumentRemoval{}, false, nil
	}
	value, err := record.canonical()
	return value, err == nil, err
}
