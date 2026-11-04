package journal

import (
	"context"

	journaldocument "github.com/isty2e/daem/internal/effect/journal/document"
	journalnamespace "github.com/isty2e/daem/internal/effect/journal/namespace"
	journalrecord "github.com/isty2e/daem/internal/effect/journal/record"
)

func prepareWholeDocumentGroup(ctx context.Context, namespace journalnamespace.DocumentNamespace, input wholeDocumentCaptureInput) (journalrecord.SealedDocumentGroup, journaldocument.Comparison, error) {
	candidate, err := prepareWholeDocumentCandidate(ctx, namespace, input)
	if err != nil {
		return journalrecord.SealedDocumentGroup{}, journaldocument.Comparison{}, err
	}
	return candidate.captureRemoval(ctx, input.Budget())
}

func prepareSelectedDocumentGroup(ctx context.Context, namespace journalnamespace.DocumentNamespace, input selectedDocumentCaptureInput, subjects documentCaptureSubjects) (journalrecord.SealedDocumentGroup, journaldocument.Comparison, error) {
	candidate, err := prepareSelectedDocumentCandidate(ctx, namespace, input, subjects)
	if err != nil {
		return journalrecord.SealedDocumentGroup{}, journaldocument.Comparison{}, err
	}
	return candidate.captureRemoval(ctx, input.Budget())
}
