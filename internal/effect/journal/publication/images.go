package publication

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"

	journaldocument "github.com/isty2e/daem/internal/effect/journal/document"
	journalrecord "github.com/isty2e/daem/internal/effect/journal/record"

	"github.com/isty2e/daem/internal/effect/journal/recovery"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/effect/workmeasure"
	"github.com/isty2e/daem/internal/realization/aggregate"
)

// StageImages writes only into the containing unpublished tree. Its
// caller must propagate failures so that the tree owner aborts partial artifacts.
func StageImages(ctx context.Context, comparison journaldocument.Comparison, groupIndex int, writer mutationfs.RootedTreeWriter, budget *physicalwork.Budget) (journalrecord.DocumentImage, journalrecord.DocumentImage, error) {
	if ctx == nil {
		return journalrecord.DocumentImage{}, journalrecord.DocumentImage{}, fmt.Errorf("document image staging context is required")
	}
	if err := ctx.Err(); err != nil {
		return journalrecord.DocumentImage{}, journalrecord.DocumentImage{}, err
	}
	if err := comparison.Validate(); err != nil {
		return journalrecord.DocumentImage{}, journalrecord.DocumentImage{}, err
	}
	if groupIndex < 0 || writer == nil || budget == nil {
		return journalrecord.DocumentImage{}, journalrecord.DocumentImage{}, fmt.Errorf("document image staging requires an ordinal, private writer and shared budget")
	}
	entries, contentBytes := 0, int64(0)
	for _, document := range []aggregate.Document{comparison.BeforeDocument(), comparison.ExpectedDocument()} {
		if err := journaldocument.ValidateImage(ctx, document, comparison.Limits()); err != nil {
			return journalrecord.DocumentImage{}, journalrecord.DocumentImage{}, err
		}
		if document.ContentSize() > recovery.MaximumRecoveryBackupFileBytes {
			return journalrecord.DocumentImage{}, journalrecord.DocumentImage{}, fmt.Errorf("document image exceeds the backup byte envelope")
		}
		if document.Exists() {
			entries++
			contentBytes += document.ContentSize()
		}
	}
	if entries != 0 {
		entries++
	}
	if entries > budget.RemainingEntries() || contentBytes > budget.RemainingBytes() {
		return journalrecord.DocumentImage{}, journalrecord.DocumentImage{}, fmt.Errorf("document images exceed the shared staging budget")
	}
	name, original, expected, err := journalrecord.StagedDocumentImageReferences(comparison, groupIndex)
	if err != nil {
		return journalrecord.DocumentImage{}, journalrecord.DocumentImage{}, err
	}
	directory, err := mutationfs.NewTreeRelativePath(name)
	if err != nil {
		return journalrecord.DocumentImage{}, journalrecord.DocumentImage{}, err
	}
	if err := ctx.Err(); err != nil {
		return journalrecord.DocumentImage{}, journalrecord.DocumentImage{}, err
	}
	if entries == 0 {
		return original, expected, nil
	}
	writeErr := writer.CreateDirectory(directory, 0o700)
	work, workErr := workmeasure.NewArtifactWork(1, 0)
	if workErr == nil {
		workErr = budget.AdmitTree(work)
	}
	if err := errors.Join(writeErr, workErr, ctx.Err()); err != nil {
		return journalrecord.DocumentImage{}, journalrecord.DocumentImage{}, err
	}
	if err := stageDocumentImage(ctx, comparison.BeforeDocument(), original, writer, budget); err != nil {
		return journalrecord.DocumentImage{}, journalrecord.DocumentImage{}, err
	}
	if err := stageDocumentImage(ctx, comparison.ExpectedDocument(), expected, writer, budget); err != nil {
		return journalrecord.DocumentImage{}, journalrecord.DocumentImage{}, err
	}
	if err := ctx.Err(); err != nil {
		return journalrecord.DocumentImage{}, journalrecord.DocumentImage{}, err
	}
	return original, expected, nil
}

func stageDocumentImage(ctx context.Context, document aggregate.Document, record journalrecord.DocumentImage, writer mutationfs.RootedTreeWriter, budget *physicalwork.Budget) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !document.Exists() {
		return nil
	}
	reference, err := record.FileReference()
	if err != nil {
		return err
	}
	path, err := mutationfs.NewTreeRelativePath(strings.Split(reference, "/")...)
	if err != nil {
		return err
	}
	writeErr := writer.WriteFile(path, journalrecord.Mode, bytes.NewReader(document.Content()))
	work, workErr := workmeasure.NewArtifactWork(1, document.ContentSize())
	if workErr == nil {
		workErr = budget.AdmitTree(work)
	}
	return errors.Join(writeErr, workErr, ctx.Err())
}
