package journal

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/isty2e/daem/internal/effect/journal/recovery"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/realization/aggregate"
)

// stageDocumentImages writes only into the containing unpublished tree. Its
// caller must propagate failures so that the tree owner aborts partial artifacts.
func stageDocumentImages(ctx context.Context, comparison DocumentComparison, groupIndex int, writer mutationfs.RootedTreeWriter, budget *recovery.PhysicalWorkBudget) (recoveryDocumentImage, recoveryDocumentImage, error) {
	if ctx == nil {
		return recoveryDocumentImage{}, recoveryDocumentImage{}, fmt.Errorf("document image staging context is required")
	}
	if err := ctx.Err(); err != nil {
		return recoveryDocumentImage{}, recoveryDocumentImage{}, err
	}
	if err := comparison.Validate(); err != nil {
		return recoveryDocumentImage{}, recoveryDocumentImage{}, err
	}
	if groupIndex < 0 || writer == nil || budget == nil {
		return recoveryDocumentImage{}, recoveryDocumentImage{}, fmt.Errorf("document image staging requires an ordinal, private writer and shared budget")
	}
	entries, contentBytes := 0, int64(0)
	for _, document := range []aggregate.Document{comparison.before, comparison.expected} {
		if err := validateComparisonDocument(ctx, document, comparison.limits); err != nil {
			return recoveryDocumentImage{}, recoveryDocumentImage{}, err
		}
		if document.ContentSize() > recovery.MaximumRecoveryBackupFileBytes {
			return recoveryDocumentImage{}, recoveryDocumentImage{}, fmt.Errorf("document image exceeds the backup byte envelope")
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
		return recoveryDocumentImage{}, recoveryDocumentImage{}, fmt.Errorf("document images exceed the shared staging budget")
	}
	directory, err := mutationfs.NewTreeRelativePath(fmt.Sprintf("document-%06d", groupIndex))
	if err != nil {
		return recoveryDocumentImage{}, recoveryDocumentImage{}, err
	}
	original, err := stagedDocumentImageRecord(comparison.before, comparison.beforeState, directory.Path()+"/original")
	if err != nil {
		return recoveryDocumentImage{}, recoveryDocumentImage{}, err
	}
	expected, err := stagedDocumentImageRecord(comparison.expected, comparison.expectedState, directory.Path()+"/expected")
	if err != nil {
		return recoveryDocumentImage{}, recoveryDocumentImage{}, err
	}
	if err := ctx.Err(); err != nil {
		return recoveryDocumentImage{}, recoveryDocumentImage{}, err
	}
	if entries == 0 {
		return original, expected, nil
	}
	writeErr := writer.CreateDirectory(directory, 0o700)
	work, workErr := recovery.NewArtifactWork(1, 0)
	if workErr == nil {
		workErr = budget.AdmitTree(work)
	}
	if err := errors.Join(writeErr, workErr, ctx.Err()); err != nil {
		return recoveryDocumentImage{}, recoveryDocumentImage{}, err
	}
	if err := stageDocumentImage(ctx, comparison.before, original, writer, budget); err != nil {
		return recoveryDocumentImage{}, recoveryDocumentImage{}, err
	}
	if err := stageDocumentImage(ctx, comparison.expected, expected, writer, budget); err != nil {
		return recoveryDocumentImage{}, recoveryDocumentImage{}, err
	}
	if err := ctx.Err(); err != nil {
		return recoveryDocumentImage{}, recoveryDocumentImage{}, err
	}
	return original, expected, nil
}

func stagedDocumentImageRecord(document aggregate.Document, state recovery.PartialDocumentState, path string) (recoveryDocumentImage, error) {
	var mode *recovery.PermissionMode
	if value, present := state.RegularFileMode(); present {
		mode = &value
	}
	if !document.Exists() {
		path = ""
	}
	return persistedDocumentImage(document, mode, path)
}

func stageDocumentImage(ctx context.Context, document aggregate.Document, record recoveryDocumentImage, writer mutationfs.RootedTreeWriter, budget *recovery.PhysicalWorkBudget) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !document.Exists() {
		return nil
	}
	path, err := mutationfs.NewTreeRelativePath(strings.Split(record.Artifact.Path, "/")...)
	if err != nil {
		return err
	}
	writeErr := writer.WriteFile(path, recoveryJournalMode, bytes.NewReader(document.Content()))
	work, workErr := recovery.NewArtifactWork(1, document.ContentSize())
	if workErr == nil {
		workErr = budget.AdmitTree(work)
	}
	return errors.Join(writeErr, workErr, ctx.Err())
}
