package publication

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/isty2e/daem/internal/assurance/durable"
	journalinventory "github.com/isty2e/daem/internal/effect/journal/inventory"
	journalqualification "github.com/isty2e/daem/internal/effect/journal/qualification"
	journalrecord "github.com/isty2e/daem/internal/effect/journal/record"
	"github.com/isty2e/daem/internal/effect/journal/retirement"
	"github.com/isty2e/daem/internal/effect/mutation"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	storagecommit "github.com/isty2e/daem/internal/effect/storage/commit"
	"github.com/isty2e/daem/internal/effect/workmeasure"
)

type NativeReleasePublicationOptions struct {
	OperationAuthority *rootedpath.EntryAuthority
	Filesystem         mutationfs.Store
	StateCodec         durable.SnapshotCodec
	Budget             *physicalwork.Budget
}

// CapturedNativeRelease contains only reloaded metadata-release publication
// evidence. It neither proves a local FileSet commit nor permits host effects.
type CapturedNativeRelease struct {
	source      journalinventory.ActiveJournal
	capture     CaptureResult
	release     journalrecord.NativeRelease
	budget      *physicalwork.Budget
	initialized bool
}

func (captured CapturedNativeRelease) Validate() error {
	if !captured.initialized {
		return fmt.Errorf("published native metadata release is required")
	}
	return errors.Join(captured.release.Validate(), captured.source.PhysicalAuthority().Validate())
}

func (captured CapturedNativeRelease) Release() journalrecord.NativeRelease { return captured.release }

func (captured CapturedNativeRelease) Source() journalinventory.ActiveJournal { return captured.source }

func (captured CapturedNativeRelease) CaptureResult() CaptureResult { return captured.capture }

func (captured CapturedNativeRelease) RequireAllocation(budget *physicalwork.Budget) error {
	if err := captured.Validate(); err != nil {
		return err
	}
	if budget == nil || budget != captured.budget {
		return fmt.Errorf("native release belongs to another work allocation")
	}
	return nil
}

// CaptureNativeRelease publishes one journal-only private tree and reloads the
// selected purpose. The containing operation serializes the recovery root and
// supplies an authority bound with this allocation; control files are untouched.
func CaptureNativeRelease(ctx context.Context, paths Paths, release journalrecord.NativeRelease, options NativeReleasePublicationOptions) (result CapturedNativeRelease, resultErr error) {
	if ctx == nil || options.OperationAuthority == nil || options.Filesystem == nil || options.StateCodec == nil || options.Budget == nil {
		return CapturedNativeRelease{}, fmt.Errorf("native release publication requires context, selected authority, filesystem, codec and budget")
	}
	if err := release.Validate(); err != nil {
		return CapturedNativeRelease{}, err
	}
	var outcome mutationfs.CommitOutcome
	attempted := false
	var directory string
	defer func() {
		resultErr = errors.Join(resultErr, ctx.Err())
		if resultErr != nil {
			result = CapturedNativeRelease{}
			if attempted {
				resultErr = &NativeJournalPublicationError{directory: directory, outcome: outcome, cause: resultErr}
			}
		}
	}()
	if err := options.Budget.RequireCapturePending(); err != nil {
		return CapturedNativeRelease{}, err
	}
	content, err := release.Encode(ctx, options.StateCodec)
	if err != nil {
		return CapturedNativeRelease{}, err
	}
	if int64(len(content)) > options.Budget.RemainingBytes() || options.Budget.RemainingEntries() < 1 {
		return CapturedNativeRelease{}, fmt.Errorf("native release journal exceeds the shared staging budget")
	}
	inventoryOptions := journalinventory.Options{
		Filesystem: options.Filesystem, StateCodec: options.StateCodec,
		PhysicalPathBudget: options.Budget, Decoder: decodeNativeCaptureInventoryJournal,
	}
	if err := journalinventory.RequireClear(ctx, paths.RecoveryDir, inventoryOptions); err != nil {
		return CapturedNativeRelease{}, err
	}
	directory, err = mutation.CanonicalDirectoryEntryPathBounded(filepath.Join(paths.RecoveryDir, release.OperationID()), physicalwork.MaximumPhysicalPathDepth, options.Budget)
	if err != nil {
		return CapturedNativeRelease{}, err
	}
	capability, err := options.OperationAuthority.Acquire()
	if err != nil {
		return CapturedNativeRelease{}, err
	}
	capabilityOpen := true
	defer func() {
		if capabilityOpen {
			resultErr = errors.Join(resultErr, capability.Close())
		}
	}()
	if err := journalqualification.RequireOperationDestination(capability, directory); err != nil {
		return CapturedNativeRelease{}, err
	}
	prepared, err := options.Filesystem.PrepareRootedTree(ctx, capability, func(writer mutationfs.RootedTreeWriter) error {
		if err := writer.SetRootMode(retirement.DirectoryMode); err != nil {
			return err
		}
		name, err := mutationfs.NewTreeRelativePath(journalrecord.FileName)
		if err != nil {
			return err
		}
		work, err := workmeasure.NewArtifactWork(1, int64(len(content)))
		if err != nil {
			return err
		}
		if err := options.Budget.AdmitTree(work); err != nil {
			return err
		}
		return writer.WriteFile(name, journalrecord.Mode, bytes.NewReader(content))
	})
	capabilityOpen = false
	if err != nil {
		return CapturedNativeRelease{}, err
	}
	defer func() { resultErr = errors.Join(resultErr, prepared.Abort(context.WithoutCancel(ctx))) }()
	attempted = true
	outcome, err = prepared.CommitWithOutcome(ctx)
	if err != nil {
		return CapturedNativeRelease{}, err
	}
	if outcome.State() != mutationfs.CommitOutcomeComplete {
		return CapturedNativeRelease{}, fmt.Errorf("native release publication has no complete visibility conclusion")
	}
	active, err := journalinventory.LoadNativeRoot(ctx, paths.RecoveryDir, inventoryOptions)
	if err != nil {
		return CapturedNativeRelease{}, err
	}
	source := active.Active()
	if source == nil || source.OperationDir() != directory || source.Identity().OperationID() != release.OperationID() {
		return CapturedNativeRelease{}, fmt.Errorf("published native release did not reload as the selected active operation")
	}
	decoded, present := source.Record().NativeRelease()
	if !present {
		return CapturedNativeRelease{}, fmt.Errorf("published journal changed its release-only purpose")
	}
	fingerprint, err := source.Record().AuthorityFingerprint(ctx, options.StateCodec)
	if err != nil {
		return CapturedNativeRelease{}, err
	}
	if fingerprint != journalrecord.Fingerprint(content) {
		return CapturedNativeRelease{}, fmt.Errorf("published native release changed its exact record")
	}
	if err := validateNativeReleaseDirectory(ctx, options.OperationAuthority, source.PhysicalAuthority()); err != nil {
		return CapturedNativeRelease{}, err
	}
	return CapturedNativeRelease{
		source: *source, release: decoded, budget: options.Budget, initialized: true,
		capture: CaptureResult{OperationID: release.OperationID(), Directory: directory, JournalPath: filepath.Join(directory, journalrecord.FileName), RecordFingerprint: fingerprint},
	}, nil
}

func validateNativeReleaseDirectory(ctx context.Context, authority *rootedpath.EntryAuthority, expected journalinventory.ActiveJournalAuthority) (resultErr error) {
	capability, err := authority.Acquire()
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, capability.Close()) }()
	snapshot, err := storagecommit.SnapshotRootedDirectoryEntries(ctx, capability, 1)
	if err != nil {
		return err
	}
	if !expected.Matches(snapshot.RootIdentity()) || !snapshot.RootOwnedByInvoker() || snapshot.RootMode() != retirement.DirectoryMode {
		return fmt.Errorf("native release directory identity or private ownership changed")
	}
	entries := snapshot.Entries()
	if len(entries) != 1 || entries[0].Name() != journalrecord.FileName || entries[0].Mode() != journalrecord.Mode || entries[0].Kind() != mutationfs.EntryKindFile || !entries[0].OwnedByInvoker() {
		return fmt.Errorf("native release directory contains unowned artifacts")
	}
	return ctx.Err()
}
