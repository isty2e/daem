package journal

import (
	"context"

	journalnamespace "github.com/isty2e/daem/internal/effect/journal/namespace"
	journalqualification "github.com/isty2e/daem/internal/effect/journal/qualification"
	journalrecord "github.com/isty2e/daem/internal/effect/journal/record"
)

// This combined read stage is still not an executable operation authority:
// active-journal, state/control, registry, phase and execution-lease joins remain.
type nativeQualifiedDocumentRecords struct {
	records    journalrecord.LoadedDocuments
	namespaces []journalnamespace.DocumentNamespace
}

func loadQualifiedDocuments(journal journalrecord.Native, ctx context.Context, options journalqualification.ReadOptions, roots journalnamespace.Roots) (nativeQualifiedDocumentRecords, error) {
	namespaces, err := journalqualification.LoadNamespaces(journal, ctx, options, roots)
	if err != nil {
		return nativeQualifiedDocumentRecords{}, err
	}
	records, err := journalqualification.LoadDocuments(journal, ctx, options)
	if err != nil {
		return nativeQualifiedDocumentRecords{}, err
	}
	return nativeQualifiedDocumentRecords{records: records, namespaces: namespaces}, nil
}
