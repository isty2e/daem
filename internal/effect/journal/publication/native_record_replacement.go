package publication

import (
	"context"
	"errors"
	"fmt"

	journalinventory "github.com/isty2e/daem/internal/effect/journal/inventory"

	journalrecord "github.com/isty2e/daem/internal/effect/journal/record"

	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/workmeasure"
)

func (captured CapturedNativeJournal) replaceRecord(ctx context.Context, candidate journalrecord.Native, options NativePromotionOptions) (CapturedNativeJournal, error) {
	filesystem, admitted := options.Filesystem.(mutationfs.Store)
	if ctx == nil || !admitted || options.RecordAuthority == nil || options.StateCodec == nil {
		return CapturedNativeJournal{}, fmt.Errorf("native record replacement requires selected authority, codecs and inventory observation ports")
	}
	content, err := candidate.Encode(ctx, options.StateCodec)
	if err != nil {
		return CapturedNativeJournal{}, err
	}
	work, err := workmeasure.NewArtifactWork(0, int64(len(content)))
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
	limit := min(journalrecord.MaximumBytes, captured.budget.RemainingBytes())
	if limit <= 0 {
		return CapturedNativeJournal{}, errors.Join(fmt.Errorf("native promotion exhausted its record byte allocation"), capability.Close())
	}
	bytes, mode, identity, err := options.Filesystem.ReadRootedRegularFileUpTo(ctx, capability, limit)
	charged := int64(len(bytes))
	if err != nil {
		charged = limit
	}
	work, workErr := workmeasure.NewArtifactWork(0, charged)
	if workErr == nil {
		workErr = captured.budget.AdmitTree(work)
	}
	err = errors.Join(err, workErr)
	if err != nil || mode.Perm() != journalrecord.Mode || journalrecord.Fingerprint(bytes) != captured.capture.RecordFingerprint {
		return CapturedNativeJournal{}, errors.Join(fmt.Errorf("native promotion journal revision changed"), err, capability.Close())
	}
	outcome, parent, err := options.Filesystem.ReplaceRootedFileAndRefreshParent(ctx, capability, content, journalrecord.Mode, identity, captured.active.Identity())
	if err != nil {
		return CapturedNativeJournal{}, &NativeJournalPublicationError{directory: captured.capture.Directory, outcome: outcome, cause: err}
	}
	if outcome.State() != mutationfs.CommitOutcomeComplete || parent == nil {
		return CapturedNativeJournal{}, fmt.Errorf("native promotion has no confirmed refreshed record authority")
	}
	active, err := journalinventory.NewActiveAuthority(parent)
	if err != nil {
		return CapturedNativeJournal{}, err
	}
	if err := journalinventory.ValidateActiveJournalAuthority(ctx, options.Filesystem, options.OperationAuthority, active); err != nil {
		return CapturedNativeJournal{}, err
	}
	return reloadNativePublication(ctx, captured.capture.Directory, captured.capture.OperationID, journalrecord.Fingerprint(content), captured.publication, captured.journal.After(), CaptureOptions{
		Filesystem: filesystem, StateCodec: options.StateCodec, Codecs: options.Codecs, OperationAuthority: options.OperationAuthority,
	})
}
