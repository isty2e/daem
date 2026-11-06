package publication

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

type StageOptions struct {
	StateCodec durable.SnapshotCodec
	Codecs     aggregate.CodecCatalog
	Limits     configdocument.Limits
	Budget     *physicalwork.Budget
}

// StageDocuments writes only an unpublished document-only tree. The caller
// must propagate errors from PrepareRootedTree and separately qualify publication.
func StageDocuments(journal journalrecord.Native, ctx context.Context, comparisons []journaldocument.Comparison, writer mutationfs.RootedTreeWriter, options StageOptions) (string, error) {
	if ctx == nil || writer == nil || options.Budget == nil || options.StateCodec == nil {
		return "", fmt.Errorf("native document staging requires context, private writer, state codec and shared budget")
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if err := journal.RequireDocumentOnly(); err != nil {
		return "", err
	}
	prepared, err := PrepareJournal(journal, ctx, comparisons, options, workmeasure.ArtifactWork{})
	if err != nil {
		return "", err
	}
	return prepared.Stage(ctx, writer)
}

type PreparedJournal struct {
	documents journalrecord.PreparedDocuments
	content   []byte
	budget    *physicalwork.Budget
}

func PrepareJournal(journal journalrecord.Native, ctx context.Context, comparisons []journaldocument.Comparison, options StageOptions, ordinaryWork workmeasure.ArtifactWork) (PreparedJournal, error) {
	if ctx == nil || options.Budget == nil || options.StateCodec == nil {
		return PreparedJournal{}, fmt.Errorf("native document staging requires context, private writer, state codec and shared budget")
	}
	entries, imageBytes, err := preflightDocumentJournalImages(ctx, comparisons, options.Budget)
	if err != nil {
		return PreparedJournal{}, err
	}
	prepared, err := journal.PrepareDocuments(ctx, comparisons, options.Limits, options.Codecs)
	if err != nil {
		return PreparedJournal{}, err
	}
	content, err := prepared.Encode(ctx, options.StateCodec)
	if err != nil {
		return PreparedJournal{}, err
	}
	if entries > options.Budget.RemainingEntries()-ordinaryWork.Entries() || int64(len(content)) > options.Budget.RemainingBytes()-ordinaryWork.Bytes()-imageBytes {
		return PreparedJournal{}, fmt.Errorf("native journal, ordinary backups and images exceed the shared staging budget")
	}
	return PreparedJournal{documents: prepared, content: content, budget: options.Budget}, nil
}

func (prepared PreparedJournal) Stage(ctx context.Context, writer mutationfs.RootedTreeWriter) (string, error) {
	comparisons, err := prepared.documents.Comparisons()
	if err != nil {
		return "", err
	}
	if ctx == nil || writer == nil || prepared.budget == nil {
		return "", fmt.Errorf("native document staging requires context, private writer, state codec and shared budget")
	}
	for index, comparison := range comparisons {
		if _, _, err := StageImages(ctx, comparison, index, writer, prepared.budget); err != nil {
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
	writeErr := writer.WriteFile(path, journalrecord.Mode, bytes.NewReader(prepared.content))
	work, workErr := workmeasure.NewArtifactWork(1, int64(len(prepared.content)))
	if workErr == nil {
		workErr = prepared.budget.AdmitTree(work)
	}
	if err := errors.Join(writeErr, workErr, ctx.Err()); err != nil {
		return "", err
	}
	fingerprint := journalrecord.Fingerprint(prepared.content)
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
