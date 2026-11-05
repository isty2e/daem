package journal

import (
	"bytes"
	"context"
	"errors"
	"fmt"

	journaldocument "github.com/isty2e/daem/internal/effect/journal/document"
	journalrecord "github.com/isty2e/daem/internal/effect/journal/record"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/effect/workmeasure"
	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/realization/aggregate"
)

type nativeDocumentStageOptions struct {
	stateCodec durable.SnapshotCodec
	codecs     aggregate.CodecCatalog
	limits     configdocument.Limits
	budget     *physicalwork.Budget
}

// stageDocumentJournal writes only an unpublished document-only tree. The caller
// must propagate errors from PrepareRootedTree and separately qualify publication.
func stageDocumentJournal(journal journalrecord.Native, ctx context.Context, comparisons []journaldocument.Comparison, writer mutationfs.RootedTreeWriter, options nativeDocumentStageOptions) (string, error) {
	if ctx == nil || writer == nil || options.budget == nil || options.stateCodec == nil {
		return "", fmt.Errorf("native document staging requires context, private writer, state codec and shared budget")
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if err := journal.RequireDocumentOnly(); err != nil {
		return "", err
	}
	prepared, content, err := prepareDocumentJournal(journal, ctx, comparisons, options, workmeasure.ArtifactWork{})
	if err != nil {
		return "", err
	}
	return stagePreparedDocumentJournal(prepared, ctx, content, writer, options.budget)
}

func prepareDocumentJournal(journal journalrecord.Native, ctx context.Context, comparisons []journaldocument.Comparison, options nativeDocumentStageOptions, ordinaryWork workmeasure.ArtifactWork) (journalrecord.PreparedDocuments, []byte, error) {
	entries, imageBytes, err := preflightDocumentJournalImages(ctx, comparisons, options.budget)
	if err != nil {
		return journalrecord.PreparedDocuments{}, nil, err
	}
	prepared, err := journal.PrepareDocuments(ctx, comparisons, options.limits, options.codecs)
	if err != nil {
		return journalrecord.PreparedDocuments{}, nil, err
	}
	content, err := prepared.Encode(ctx, options.stateCodec)
	if err != nil {
		return journalrecord.PreparedDocuments{}, nil, err
	}
	if entries > options.budget.RemainingEntries()-ordinaryWork.Entries() || int64(len(content)) > options.budget.RemainingBytes()-ordinaryWork.Bytes()-imageBytes {
		return journalrecord.PreparedDocuments{}, nil, fmt.Errorf("native journal, ordinary backups and images exceed the shared staging budget")
	}
	return prepared, content, nil
}

func stagePreparedDocumentJournal(prepared journalrecord.PreparedDocuments, ctx context.Context, content []byte, writer mutationfs.RootedTreeWriter, budget *physicalwork.Budget) (string, error) {
	comparisons, err := prepared.Comparisons()
	if err != nil {
		return "", err
	}
	for index, comparison := range comparisons {
		if _, _, err := stageDocumentImages(ctx, comparison, index, writer, budget); err != nil {
			return "", err
		}
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	path, err := mutationfs.NewTreeRelativePath(journalrecord.FileName)
	if err != nil {
		return "", err
	}
	writeErr := writer.WriteFile(path, journalrecord.Mode, bytes.NewReader(content))
	work, workErr := workmeasure.NewArtifactWork(1, int64(len(content)))
	if workErr == nil {
		workErr = budget.AdmitTree(work)
	}
	if err := errors.Join(writeErr, workErr, ctx.Err()); err != nil {
		return "", err
	}
	fingerprint := journalrecord.Fingerprint(content)
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return fingerprint, nil
}

func preflightDocumentJournalImages(ctx context.Context, comparisons []journaldocument.Comparison, budget *physicalwork.Budget) (int, int64, error) {
	entries, contentBytes := 1, int64(0)
	for _, comparison := range comparisons {
		if err := ctx.Err(); err != nil {
			return 0, 0, err
		}
		files := 0
		for _, image := range []aggregate.Document{comparison.BeforeDocument(), comparison.ExpectedDocument()} {
			if image.ContentSize() > recovery.MaximumRecoveryBackupFileBytes || image.ContentSize() > budget.RemainingBytes()-contentBytes {
				return 0, 0, fmt.Errorf("native document images exceed the staging byte envelope")
			}
			if image.Exists() {
				files++
				contentBytes += image.ContentSize()
			}
		}
		if files != 0 {
			entries += files + 1
		}
		if entries > budget.RemainingEntries() {
			return 0, 0, fmt.Errorf("native document images and journal exceed the staging entry envelope")
		}
	}
	return entries, contentBytes, nil
}
