package qualification

import (
	"context"
	"fmt"

	journalnamespace "github.com/isty2e/daem/internal/effect/journal/namespace"
	journalrecord "github.com/isty2e/daem/internal/effect/journal/record"
)

// Documents preserves complete record facts while only its live subset borrows
// namespaces. Active journal, phase and execution authority need separate joins.
type Documents struct {
	records journalrecord.LoadedDocuments
	live    []qualifiedDocument
}

type qualifiedDocument struct {
	record    int
	namespace journalnamespace.DocumentNamespace
}

func QualifyDocuments(journal journalrecord.Native, ctx context.Context, options ReadOptions, roots journalnamespace.Roots) (Documents, error) {
	namespaces, err := LoadNamespaces(journal, ctx, options, roots)
	if err != nil {
		return Documents{}, err
	}
	records, err := LoadDocuments(journal, ctx, options)
	if err != nil {
		return Documents{}, err
	}
	return pairDemandedDocuments(records, namespaces, liveRootDemand(records))
}

func qualifyDemandedDocuments(journal journalrecord.Native, ctx context.Context, options ReadOptions, roots journalnamespace.Roots, records journalrecord.LoadedDocuments, demand RecoveryRootDemand) (Documents, error) {
	if !demand.valid || demand.count != records.Len() {
		return Documents{}, fmt.Errorf("native recovery document demand is incomplete")
	}
	namespaces, err := loadDemandedNamespaces(journal, ctx, options, roots, demand)
	if err != nil {
		return Documents{}, err
	}
	return pairDemandedDocuments(records, namespaces, demand)
}

func pairDemandedDocuments(records journalrecord.LoadedDocuments, namespaces []journalnamespace.DocumentNamespace, demand RecoveryRootDemand) (Documents, error) {
	if len(namespaces) != len(demand.live) {
		return Documents{}, fmt.Errorf("native recovery document facts are incomplete")
	}
	live := make([]qualifiedDocument, len(namespaces))
	for index, namespace := range namespaces {
		live[index] = qualifiedDocument{record: demand.live[index], namespace: namespace}
	}
	return Documents{records: records, live: live}, nil
}

func (documents Documents) Len() int { return len(documents.live) }

func (documents Documents) Records() journalrecord.LoadedDocuments { return documents.records }

func (documents Documents) RecordIndex(index int) (int, bool) {
	if index < 0 || index >= len(documents.live) {
		return 0, false
	}
	return documents.live[index].record, true
}

func (documents Documents) DocumentAt(index int) (journalrecord.LoadedDocument, bool) {
	record, present := documents.RecordIndex(index)
	if !present {
		return journalrecord.LoadedDocument{}, false
	}
	return documents.records.At(record)
}

func (documents Documents) NamespaceAt(index int) (journalnamespace.DocumentNamespace, bool) {
	if index < 0 || index >= len(documents.live) {
		return journalnamespace.DocumentNamespace{}, false
	}
	return documents.live[index].namespace, true
}
