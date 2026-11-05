package journal

import (
	"context"
	"errors"
	"fmt"

	journalrecord "github.com/isty2e/daem/internal/effect/journal/record"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/effect/physicalwork"
)

func validateNativeActive(ctx context.Context, capture CaptureResult, active ActiveJournalAuthority, filesystem mutationfs.RootedReader, entry *rootedpath.EntryAuthority, budget *physicalwork.Budget) (resultErr error) {
	if ctx == nil || filesystem == nil || entry == nil || budget == nil {
		return fmt.Errorf("native active validation requires selected authority, filesystem and the original allocation")
	}
	if err := active.Validate(); err != nil {
		return err
	}
	capability, err := entry.Acquire()
	if err != nil {
		return err
	}
	pathErr := validateOperationCapabilityPath(capability, capture.Directory)
	identity, identityErr := filesystem.CaptureRootedEntryIdentity(ctx, capability)
	if err := errors.Join(pathErr, identityErr, capability.Close()); err != nil {
		return err
	}
	if !active.matches(identity) {
		return fmt.Errorf("published native journal directory identity changed")
	}
	root, err := rootedpath.CaptureCanonicalRootNoFollowBounded(capture.Directory, physicalwork.MaximumPhysicalPathDepth, budget)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, root.Close(), ctx.Err()) }()
	if err := ValidateActiveJournalRoot(ctx, filesystem, root, budget, active); err != nil {
		return err
	}
	snapshot, err := readNativeCaptureRecord(ctx, root, filesystem, budget)
	if err != nil {
		return err
	}
	if snapshot.Mode().Perm() != journalrecord.Mode || journalrecord.Fingerprint(snapshot.Content()) != capture.RecordFingerprint {
		return fmt.Errorf("published native journal record revision changed")
	}
	return ValidateActiveJournalRoot(ctx, filesystem, root, budget, active)
}
