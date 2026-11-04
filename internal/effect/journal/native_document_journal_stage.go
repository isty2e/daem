package journal

import (
	"bytes"
	"context"
	"errors"
	"fmt"

	journaldocument "github.com/isty2e/daem/internal/effect/journal/document"

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
func stageDocumentJournal(journal nativeJournalSnapshots, ctx context.Context, comparisons []journaldocument.Comparison, writer mutationfs.RootedTreeWriter, options nativeDocumentStageOptions) (string, error) {
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
	return prepared.stageJournal(ctx, content, writer, options.budget)
}

func prepareDocumentJournal(journal nativeJournalSnapshots, ctx context.Context, comparisons []journaldocument.Comparison, options nativeDocumentStageOptions, ordinaryWork workmeasure.ArtifactWork) (nativeDocumentRecords, []byte, error) {
	entries, imageBytes, err := preflightDocumentJournalImages(ctx, comparisons, options.budget)
	if err != nil {
		return nativeDocumentRecords{}, nil, err
	}
	prepared, err := journal.prepareDocuments(ctx, comparisons, options.limits, options.codecs)
	if err != nil {
		return nativeDocumentRecords{}, nil, err
	}
	content, err := prepared.encode(ctx, options.stateCodec)
	if err != nil {
		return nativeDocumentRecords{}, nil, err
	}
	if entries > options.budget.RemainingEntries()-ordinaryWork.Entries() || int64(len(content)) > options.budget.RemainingBytes()-ordinaryWork.Bytes()-imageBytes {
		return nativeDocumentRecords{}, nil, fmt.Errorf("native journal, ordinary backups and images exceed the shared staging budget")
	}
	return prepared.documents, content, nil
}

func (prepared nativeDocumentRecords) stageJournal(ctx context.Context, content []byte, writer mutationfs.RootedTreeWriter, budget *physicalwork.Budget) (string, error) {
	for index, group := range prepared.groups {
		if _, _, err := stageDocumentImages(ctx, group.Comparison(), index, writer, budget); err != nil {
			return "", err
		}
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	path, err := mutationfs.NewTreeRelativePath(recoveryJournalFileName)
	if err != nil {
		return "", err
	}
	writeErr := writer.WriteFile(path, recoveryJournalMode, bytes.NewReader(content))
	work, workErr := workmeasure.NewArtifactWork(1, int64(len(content)))
	if workErr == nil {
		workErr = budget.AdmitTree(work)
	}
	if err := errors.Join(writeErr, workErr, ctx.Err()); err != nil {
		return "", err
	}
	fingerprint := recoveryJournalRecordFingerprint(content)
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
