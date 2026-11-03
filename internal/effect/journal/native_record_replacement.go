package journal

import (
	"context"
	"errors"
	"fmt"

	"github.com/isty2e/daem/internal/effect/journal/recovery"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
)

func (captured CapturedNativeJournal) replaceRecord(ctx context.Context, candidate nativeJournalRecord, options NativePromotionOptions) (CapturedNativeJournal, error) {
	filesystem, admitted := options.Filesystem.(mutationfs.Store)
	if ctx == nil || !admitted || options.RecordAuthority == nil || options.StateCodec == nil {
		return CapturedNativeJournal{}, fmt.Errorf("native record replacement requires selected authority, codecs and inventory observation ports")
	}
	content, err := marshalNativeJournalRecord(ctx, candidate, captured.journal.before, captured.journal.after, options.StateCodec)
	if err != nil {
		return CapturedNativeJournal{}, err
	}
	work, err := recovery.NewArtifactWork(0, int64(len(content)))
	if err != nil {
		return CapturedNativeJournal{}, err
	}
	if err := captured.budget.AdmitTree(work); err != nil {
		return CapturedNativeJournal{}, err
	}
	capability, err := options.RecordAuthority.Acquire()
	if err != nil {
		return CapturedNativeJournal{}, err
	}
	path, err := capability.Destination().LexicalPath()
	if err != nil || path != captured.capture.JournalPath {
		return CapturedNativeJournal{}, errors.Join(fmt.Errorf("native promotion record authority identifies another entry"), err, capability.Close())
	}
	limit := min(maximumRecoveryJournalBytes, captured.budget.RemainingBytes())
	if limit <= 0 {
		return CapturedNativeJournal{}, errors.Join(fmt.Errorf("native promotion exhausted its record byte allocation"), capability.Close())
	}
	bytes, mode, identity, err := options.Filesystem.ReadRootedRegularFileUpTo(ctx, capability, limit)
	charged := int64(len(bytes))
	if err != nil {
		charged = limit
	}
	work, workErr := recovery.NewArtifactWork(0, charged)
	if workErr == nil {
		workErr = captured.budget.AdmitTree(work)
	}
	err = errors.Join(err, workErr)
	if err != nil || mode.Perm() != recoveryJournalMode || recoveryJournalRecordFingerprint(bytes) != captured.capture.RecordFingerprint {
		return CapturedNativeJournal{}, errors.Join(fmt.Errorf("native promotion journal revision changed"), err, capability.Close())
	}
	outcome, parent, err := options.Filesystem.ReplaceRootedFileAndRefreshParent(ctx, capability, content, recoveryJournalMode, identity, captured.active.identity)
	if err != nil {
		return CapturedNativeJournal{}, &NativeJournalPublicationError{directory: captured.capture.Directory, outcome: outcome, cause: err}
	}
	if outcome.State() != mutationfs.CommitOutcomeComplete || parent == nil {
		return CapturedNativeJournal{}, fmt.Errorf("native promotion has no confirmed refreshed record authority")
	}
	active, err := newActiveJournalAuthority(parent)
	if err != nil {
		return CapturedNativeJournal{}, err
	}
	if err := ValidateActiveJournalAuthority(ctx, options.Filesystem, options.OperationAuthority, active); err != nil {
		return CapturedNativeJournal{}, err
	}
	return reloadNativePublication(ctx, captured.capture.Directory, captured.capture.OperationID, recoveryJournalRecordFingerprint(content), captured.publication, captured.journal.after, CaptureOptions{
		Filesystem: filesystem, StateCodec: options.StateCodec, Codecs: options.Codecs, OperationAuthority: options.OperationAuthority,
	})
}
