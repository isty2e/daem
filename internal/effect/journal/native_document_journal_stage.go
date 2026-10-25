package journal

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/realization/aggregate"
)

type nativeDocumentStageOptions struct {
	stateCodec durable.SnapshotCodec
	codecs     aggregate.CodecCatalog
	limits     configdocument.Limits
	budget     *recovery.PhysicalWorkBudget
}

// stageDocumentJournal writes only an unpublished document-only tree. The caller
// must propagate errors from PrepareRootedTree and separately qualify publication.
func (journal nativeJournalSnapshots) stageDocumentJournal(ctx context.Context, comparisons []DocumentComparison, writer mutationfs.RootedTreeWriter, options nativeDocumentStageOptions) (string, error) {
	if ctx == nil || writer == nil || options.budget == nil || options.stateCodec == nil {
		return "", fmt.Errorf("native document staging requires context, private writer, state codec and shared budget")
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if len(journal.record.DocumentGroups) == 0 || len(journal.record.Entries) != 0 || len(journal.record.ClaimTransitions) != 0 || len(journal.record.ProvisionalAcquires) != 0 || len(journal.record.RemovalIntents) != 0 {
		return "", fmt.Errorf("document-only staging requires document groups without ordinary entry artifacts")
	}
	entries, imageBytes, err := preflightDocumentJournalImages(ctx, comparisons, options.budget)
	if err != nil {
		return "", err
	}
	prepared, err := journal.prepareDocuments(ctx, comparisons, options.limits, options.codecs)
	if err != nil {
		return "", err
	}
	record := journal.record
	record.DocumentGroups = slices.Clone(record.DocumentGroups)
	for index, group := range prepared.groups {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		_, original, expected, err := documentImageReferences(group.comparison, index)
		if err != nil {
			return "", err
		}
		record.DocumentGroups[index].Original, record.DocumentGroups[index].Expected = original, expected
	}
	content, err := marshalNativeJournalRecord(ctx, record, journal.before, journal.after, options.stateCodec)
	if err != nil {
		return "", err
	}
	if entries > options.budget.RemainingEntries() || int64(len(content)) > options.budget.RemainingBytes()-imageBytes {
		return "", fmt.Errorf("native journal and images exceed the shared staging budget")
	}
	for index, group := range prepared.groups {
		if _, _, err := stageDocumentImages(ctx, group.comparison, index, writer, options.budget); err != nil {
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
	work, workErr := recovery.NewArtifactWork(1, int64(len(content)))
	if workErr == nil {
		workErr = options.budget.AdmitTree(work)
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

func preflightDocumentJournalImages(ctx context.Context, comparisons []DocumentComparison, budget *recovery.PhysicalWorkBudget) (int, int64, error) {
	entries, contentBytes := 1, int64(0)
	for _, comparison := range comparisons {
		if err := ctx.Err(); err != nil {
			return 0, 0, err
		}
		files := 0
		for _, image := range []aggregate.Document{comparison.before, comparison.expected} {
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
