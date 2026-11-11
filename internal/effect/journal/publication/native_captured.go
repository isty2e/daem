package publication

import (
	"context"
	"fmt"

	journalinventory "github.com/isty2e/daem/internal/effect/journal/inventory"
	journalqualification "github.com/isty2e/daem/internal/effect/journal/qualification"

	journalrecord "github.com/isty2e/daem/internal/effect/journal/record"

	journalnamespace "github.com/isty2e/daem/internal/effect/journal/namespace"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	journaldocument "github.com/isty2e/daem/internal/effect/journal/document"
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
	source      journalinventory.ActiveJournal
	capture     CaptureResult
	journal     journalrecord.Native
	qualified   journalqualification.Documents
	active      journalinventory.ActiveJournalAuthority
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

func (captured CapturedNativeJournal) CaptureResult() CaptureResult { return captured.capture }

// BeforeState returns the complete Before snapshot, or its zero value for zero capture.
func (captured CapturedNativeJournal) BeforeState() durable.Snapshot {
	return captured.journal.Before()
}

// AfterState returns the complete After snapshot, or its zero value for zero capture.
func (captured CapturedNativeJournal) AfterState() durable.Snapshot { return captured.journal.After() }

// Owner returns the operation owner, or its zero value for zero capture.
func (captured CapturedNativeJournal) Owner() stateauthority.Authority {
	return captured.journal.Owner()
}

func (captured CapturedNativeJournal) ActiveJournalAuthority() journalinventory.ActiveJournalAuthority {
	return captured.active
}

// CapturedNativeDocument is a read-only projection of one published group. Its
// namespace borrows the original host roots; it never borrows the closed reload root.
type CapturedNativeDocument struct {
	captured *CapturedNativeJournal
	index    int
}

func (document CapturedNativeDocument) valid() bool {
	return document.captured != nil && document.captured.initialized && document.index >= 0 && document.index < document.captured.qualified.Len()
}

func (document CapturedNativeDocument) loadedEvidence() (journalrecord.LoadedDocument, bool) {
	if !document.valid() {
		return journalrecord.LoadedDocument{}, false
	}
	return document.captured.qualified.DocumentAt(document.index)
}

func (captured CapturedNativeJournal) Documents() []CapturedNativeDocument {
	if !captured.initialized {
		return nil
	}
	result := make([]CapturedNativeDocument, captured.qualified.Len())
	for index := range result {
		result[index] = CapturedNativeDocument{captured: &captured, index: index}
	}
	return result
}

func (document CapturedNativeDocument) Namespace() journalnamespace.DocumentNamespace {
	if !document.valid() {
		return journalnamespace.DocumentNamespace{}
	}
	namespace, present := document.captured.qualified.NamespaceAt(document.index)
	if !present {
		return journalnamespace.DocumentNamespace{}
	}
	return namespace
}

func (document CapturedNativeDocument) Comparison() journaldocument.Comparison {
	loaded, present := document.loadedEvidence()
	if !present {
		return journaldocument.Comparison{}
	}
	return loaded.Group().Comparison()
}

func (document CapturedNativeDocument) Original() (aggregate.Document, *recovery.PermissionMode) {
	loaded, present := document.loadedEvidence()
	if !present {
		return aggregate.Document{}, nil
	}
	image := loaded.Images().Original()
	return image.Document(), image.Mode()
}

func (document CapturedNativeDocument) Expected() (aggregate.Document, *recovery.PermissionMode) {
	loaded, present := document.loadedEvidence()
	if !present {
		return aggregate.Document{}, nil
	}
	image := loaded.Images().Expected()
	return image.Document(), image.Mode()
}

func (document CapturedNativeDocument) SelectedSubjects(ctx context.Context) ([]topology.SubjectID, error) {
	if !document.valid() || ctx == nil {
		return nil, fmt.Errorf("captured native document and context are required")
	}
	recordIndex, present := document.captured.qualified.RecordIndex(document.index)
	if !present {
		return nil, fmt.Errorf("captured native document has no record index")
	}
	recorded, err := document.captured.journal.DocumentAt(recordIndex)
	if err != nil {
		return nil, err
	}
	return recorded.SelectedSubjects(ctx)
}

func (document CapturedNativeDocument) Claims() (ownershipmutation.DocumentClaimTransition, bool) {
	loaded, present := document.loadedEvidence()
	if !present {
		return ownershipmutation.DocumentClaimTransition{}, false
	}
	recordIndex, present := document.captured.qualified.RecordIndex(document.index)
	if !present {
		return ownershipmutation.DocumentClaimTransition{}, false
	}
	recorded, err := document.captured.journal.DocumentAt(recordIndex)
	if err != nil {
		return ownershipmutation.DocumentClaimTransition{}, false
	}
	return loaded.Claims(), recorded.HasExactClaims()
}

func (document CapturedNativeDocument) BindingIntents() []ownership.DocumentBindingIntent {
	loaded, present := document.loadedEvidence()
	if !present {
		return nil
	}
	return loaded.Group().BindingIntents()
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
	loaded, present := document.loadedEvidence()
	if !present {
		return nil
	}
	values := loaded.Provisional()
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
	recordIndex, present := document.captured.qualified.RecordIndex(document.index)
	if !present {
		return recovery.DocumentRemoval{}, false, fmt.Errorf("captured native document has no record index")
	}
	record, err := document.captured.journal.DocumentAt(recordIndex)
	if err != nil {
		return recovery.DocumentRemoval{}, false, err
	}
	return record.Removal()
}
