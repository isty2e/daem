package qualification

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"

	journalinventory "github.com/isty2e/daem/internal/effect/journal/inventory"
	journalrecord "github.com/isty2e/daem/internal/effect/journal/record"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/effect/workmeasure"
)

func RequireOperationDestination(capability rootedpath.CommitCapability, directory string) error {
	if capability == nil {
		return fmt.Errorf("recovery journal publication capability is required")
	}
	capabilityPath, err := capability.Destination().LexicalPath()
	if err != nil {
		return fmt.Errorf("read recovery journal publication destination: %w", err)
	}
	if filepath.Clean(capabilityPath) != filepath.Clean(directory) {
		return fmt.Errorf(
			"recovery journal publication destination %q does not match operation directory %q",
			capabilityPath,
			directory,
		)
	}
	return nil
}

func ReadJournalSnapshot(ctx context.Context, root *rootedpath.CapturedRoot, filesystem mutationfs.RootedReader, budget *physicalwork.Budget) (mutationfs.RegularFileSnapshot, error) {
	maximum := min(journalrecord.MaximumBytes, budget.RemainingBytes())
	if maximum <= 0 {
		return mutationfs.RegularFileSnapshot{}, fmt.Errorf("native journal reload exceeds the remaining byte allocation")
	}
	authority, err := root.AuthorityBounded(budget)
	if err != nil {
		return mutationfs.RegularFileSnapshot{}, err
	}
	relative, err := rootedpath.NewRelativeDestination(journalrecord.FileName)
	if err != nil {
		return mutationfs.RegularFileSnapshot{}, err
	}
	bound, err := authority.Bind(relative)
	if err != nil {
		return mutationfs.RegularFileSnapshot{}, err
	}
	capability, err := root.AcquireBounded(bound, physicalwork.MaximumPhysicalPathDepth, budget)
	if err != nil {
		return mutationfs.RegularFileSnapshot{}, err
	}
	content, mode, identity, readErr := filesystem.ReadRootedRegularFileUpTo(ctx, capability, maximum)
	charged := int64(len(content))
	if readErr != nil {
		charged = maximum
	}
	work, workErr := workmeasure.NewArtifactWork(0, charged)
	if workErr == nil {
		workErr = budget.AdmitTree(work)
	}
	if err := errors.Join(readErr, capability.Close(), workErr, ctx.Err()); err != nil {
		return mutationfs.RegularFileSnapshot{}, err
	}
	return mutationfs.NewRegularFileSnapshot(content, mode, identity)
}

// ValidateNativeRevision checks one selected physical directory and byte revision.
// It does not refresh host/state/Registry observations or confer execution rights.
func ValidateNativeRevision(ctx context.Context, directory, fingerprint string, active journalinventory.ActiveJournalAuthority, filesystem mutationfs.RootedReader, entry *rootedpath.EntryAuthority, budget *physicalwork.Budget) (resultErr error) {
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
	pathErr := RequireOperationDestination(capability, directory)
	identity, identityErr := filesystem.CaptureRootedEntryIdentity(ctx, capability)
	if err := errors.Join(pathErr, identityErr, capability.Close()); err != nil {
		return err
	}
	if !active.Matches(identity) {
		return fmt.Errorf("published native journal directory identity changed")
	}
	root, err := rootedpath.CaptureCanonicalRootNoFollowBounded(directory, physicalwork.MaximumPhysicalPathDepth, budget)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, root.Close(), ctx.Err()) }()
	if err := journalinventory.ValidateActiveJournalRoot(ctx, filesystem, root, budget, active); err != nil {
		return err
	}
	snapshot, err := ReadJournalSnapshot(ctx, root, filesystem, budget)
	if err != nil {
		return err
	}
	if snapshot.Mode().Perm() != journalrecord.Mode || journalrecord.Fingerprint(snapshot.Content()) != fingerprint {
		return fmt.Errorf("published native journal record revision changed")
	}
	return journalinventory.ValidateActiveJournalRoot(ctx, filesystem, root, budget, active)
}
