package journal

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"

	journalinventory "github.com/isty2e/daem/internal/effect/journal/inventory"

	"github.com/isty2e/daem/internal/assurance/durable"
	journalqualification "github.com/isty2e/daem/internal/effect/journal/qualification"

	journalrecord "github.com/isty2e/daem/internal/effect/journal/record"
	"github.com/isty2e/daem/internal/effect/journal/retirement"

	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/encoding/jsonstrict"
)

func reloadNativePublication(ctx context.Context, directory, operationID, fingerprint string, batch nativePublicationContext, after durable.Snapshot, options CaptureOptions) (result CapturedNativeJournal, resultErr error) {
	inventory, err := journalinventory.LoadRoot(ctx, filepath.Dir(directory), journalinventory.Options{
		Filesystem: options.Filesystem, StateCodec: options.StateCodec, PhysicalPathBudget: batch.budget, Decoder: decodeNativeCaptureInventoryJournal,
	})
	if err != nil {
		return CapturedNativeJournal{}, err
	}
	if inventory.Decision().State() != retirement.StateActive || inventory.Active() == nil {
		return CapturedNativeJournal{}, fmt.Errorf("published native journal is not the sole active operation")
	}
	active := inventory.Active()
	journal, admitted := active.Record().Native()
	if !admitted || active.Record().OperationID() != operationID || active.OperationDir() != directory || active.Identity().JournalAuthorityFingerprint() != fingerprint {
		return CapturedNativeJournal{}, fmt.Errorf("published native operation or fingerprint differs from its staged record")
	}
	if !journal.MatchesContext(batch.owner, batch.before, after) {
		return CapturedNativeJournal{}, fmt.Errorf("published native journal differs from its complete operation context")
	}
	root, err := rootedpath.CaptureCanonicalRootNoFollowBounded(directory, physicalwork.MaximumPhysicalPathDepth, batch.budget)
	if err != nil {
		return CapturedNativeJournal{}, err
	}
	defer func() {
		resultErr = errors.Join(resultErr, root.Close(), ctx.Err())
		if resultErr != nil {
			result = CapturedNativeJournal{}
		}
	}()
	if err := journalinventory.ValidateActiveJournalRoot(ctx, options.Filesystem, root, batch.budget, active.PhysicalAuthority()); err != nil {
		return CapturedNativeJournal{}, err
	}
	snapshot, err := journalqualification.ReadJournalSnapshot(ctx, root, options.Filesystem, batch.budget)
	if err != nil {
		return CapturedNativeJournal{}, err
	}
	if journalrecord.Fingerprint(snapshot.Content()) != fingerprint {
		return CapturedNativeJournal{}, fmt.Errorf("published native journal bytes differ from the staged fingerprint")
	}
	if _, err := journalrecord.DecodeNativeSnapshot(ctx, snapshot, filepath.Join(directory, journalrecord.FileName), options.StateCodec); err != nil {
		return CapturedNativeJournal{}, err
	}
	qualified, err := journalqualification.QualifyDocuments(journal, ctx, journalqualification.ReadOptions{
		Artifacts: root, Filesystem: options.Filesystem, Budget: batch.budget, Limits: batch.limits,
		Codecs: options.Codecs, OrdinaryCapabilities: batch.ordinaryCapabilities,
	}, batch.roots)
	if err != nil {
		return CapturedNativeJournal{}, err
	}
	if err := journalinventory.ValidateActiveJournalRoot(ctx, options.Filesystem, root, batch.budget, active.PhysicalAuthority()); err != nil {
		return CapturedNativeJournal{}, err
	}
	if err := journalinventory.ValidateActiveJournalAuthority(ctx, options.Filesystem, options.OperationAuthority, active.PhysicalAuthority()); err != nil {
		return CapturedNativeJournal{}, err
	}
	if _, _, err := journalqualification.LoadOrdinaryBackups(ctx, journal, directory, active.PhysicalAuthority(), options.Filesystem, batch.budget); err != nil {
		return CapturedNativeJournal{}, err
	}
	capture := CaptureResult{OperationID: operationID, Directory: directory, JournalPath: filepath.Join(directory, journalrecord.FileName), RecordFingerprint: fingerprint}
	if err := journalqualification.ValidateNativeRevision(ctx, capture.Directory, capture.RecordFingerprint, active.PhysicalAuthority(), options.Filesystem, options.OperationAuthority, batch.budget); err != nil {
		return CapturedNativeJournal{}, err
	}
	return CapturedNativeJournal{
		capture: capture,
		journal: journal, qualified: qualified, active: active.PhysicalAuthority(), budget: batch.budget, publication: batch, initialized: true,
	}, nil
}

func decodeNativeCaptureInventoryJournal(ctx context.Context, snapshot mutationfs.RegularFileSnapshot, path string, codec durable.SnapshotCodec) (journalrecord.Loaded, error) {
	if err := ctx.Err(); err != nil {
		return journalrecord.Loaded{}, err
	}
	version, err := jsonstrict.ValidateVersionedObject(snapshot.Content(), "capture recovery inventory journal", journalrecord.MaximumJSONDepth)
	if err != nil {
		return journalrecord.Loaded{}, err
	}
	switch version {
	case journalrecord.OrdinaryVersion:
		return journalrecord.DecodeOrdinarySnapshot(ctx, snapshot, path, codec)
	case journalrecord.NativeVersion:
		return journalrecord.DecodeNativeSnapshot(ctx, snapshot, path, codec)
	default:
		return journalrecord.Loaded{}, journalrecord.UnsupportedVersion(version)
	}
}

// ValidateActive checks the selected directory identity and exact published
// record revision. It does not refresh sources, host state, Registry or leases.
func (captured CapturedNativeJournal) ValidateActive(ctx context.Context, filesystem mutationfs.RootedReader, entry *rootedpath.EntryAuthority, budget *physicalwork.Budget) error {
	if err := captured.Validate(); err != nil {
		return err
	}
	if budget != captured.budget {
		return fmt.Errorf("native active validation requires the original allocation")
	}
	return journalqualification.ValidateNativeRevision(ctx, captured.capture.Directory, captured.capture.RecordFingerprint, captured.active, filesystem, entry, budget)
}
