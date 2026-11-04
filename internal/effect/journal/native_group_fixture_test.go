package journal

import (
	"context"

	journaldocument "github.com/isty2e/daem/internal/effect/journal/document"
	journalnamespace "github.com/isty2e/daem/internal/effect/journal/namespace"
)

func prepareWholeDocumentGroup(ctx context.Context, namespace journalnamespace.DocumentNamespace, input wholeDocumentCaptureInput) (recoveryDocumentGroup, journaldocument.Comparison, error) {
	candidate, err := prepareWholeDocumentCandidate(ctx, namespace, input)
	if err != nil {
		return recoveryDocumentGroup{}, journaldocument.Comparison{}, err
	}
	group, comparison, err := candidate.captureRemoval(ctx, input.Budget())
	return group.record, comparison, err
}

func prepareSelectedDocumentGroup(ctx context.Context, namespace journalnamespace.DocumentNamespace, input selectedDocumentCaptureInput, subjects documentCaptureSubjects) (recoveryDocumentGroup, journaldocument.Comparison, error) {
	candidate, err := prepareSelectedDocumentCandidate(ctx, namespace, input, subjects)
	if err != nil {
		return recoveryDocumentGroup{}, journaldocument.Comparison{}, err
	}
	group, comparison, err := candidate.captureRemoval(ctx, input.Budget())
	return group.record, comparison, err
}
