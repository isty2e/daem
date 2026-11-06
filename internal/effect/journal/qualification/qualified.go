package qualification

import (
	"context"
	"fmt"

	journalnamespace "github.com/isty2e/daem/internal/effect/journal/namespace"
	journalrecord "github.com/isty2e/daem/internal/effect/journal/record"
)

// Documents pairs completed record facts with their borrowed namespaces. Active
// journal, state/Registry, phase and execution authority require separate joins.
type Documents struct {
	records    journalrecord.LoadedDocuments
	namespaces []journalnamespace.DocumentNamespace
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
	if len(namespaces) != records.Len() {
		return Documents{}, fmt.Errorf("native recovery document facts are incomplete")
	}
	return Documents{records: records, namespaces: namespaces}, nil
}

func (documents Documents) Len() int { return documents.records.Len() }

func (documents Documents) Records() journalrecord.LoadedDocuments { return documents.records }

func (documents Documents) NamespaceAt(index int) (journalnamespace.DocumentNamespace, bool) {
	if index < 0 || index >= documents.records.Len() || index >= len(documents.namespaces) {
		return journalnamespace.DocumentNamespace{}, false
	}
	return documents.namespaces[index], true
}
