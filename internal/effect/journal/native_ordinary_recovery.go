package journal

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"

	journalrecord "github.com/isty2e/daem/internal/effect/journal/record"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/output/ownership"
	"github.com/isty2e/daem/internal/target"
)

type nativeOrdinaryAssessment struct {
	paths       []recoveryPathObservation
	backups     []recoveryBackupObservation
	entries     recovery.EntryAssessment
	checkpoints []recovery.RemovalCleanupObligation
}

func ordinaryBackups(ctx context.Context, journal journalrecord.Native, directory string, active ActiveJournalAuthority, filesystem mutationfs.RootedReader, budget *physicalwork.Budget) ([]recovery.Entry, []recoveryBackupObservation, error) {
	entries, err := journal.OrdinaryEntries(ctx)
	if err != nil {
		return nil, nil, err
	}
	views, err := journal.RecordedEntries()
	if err != nil {
		return nil, nil, err
	}
	backups, err := recoveryBackupObservations(ctx, directory, active, views, filesystem, budget)
	if err != nil {
		return nil, nil, err
	}
	if err := recovery.ValidateBackups(entries, canonicalRecoveryBackupEvidence(backups)); err != nil {
		return nil, nil, err
	}
	return entries, backups, nil
}

func assessOrdinaryRecovery(journal journalrecord.Native, ctx context.Context, options nativeDocumentRecoveryReadOptions, registry ownership.Registry) (nativeOrdinaryAssessment, error) {
	entries, backups, err := ordinaryBackups(ctx, journal, options.directory, options.active, options.documents.Filesystem, options.documents.Budget)
	if err != nil {
		return nativeOrdinaryAssessment{}, err
	}
	if len(entries) == 0 {
		assessed, err := recovery.AssessEntries(nil, nil, nil)
		return nativeOrdinaryAssessment{entries: assessed}, err
	}
	filesystem, supported := options.documents.Filesystem.(mutationfs.Reader)
	if !supported {
		return nativeOrdinaryAssessment{}, fmt.Errorf("ordinary recovery requires its selected observation ports")
	}
	ordinaryViews, err := journal.RecordedEntries()
	if err != nil {
		return nativeOrdinaryAssessment{}, err
	}
	if err := validateRecoveryGlobalPathBindings(ctx, ordinaryViews, options.resolver, options.documents.OrdinaryCapabilities, options.documents.Budget); err != nil {
		return nativeOrdinaryAssessment{}, err
	}
	work := &ordinaryCaptureWork{budget: options.documents.Budget}
	manifest, err := work.manifest(Paths{ManifestRoot: options.roots.Manifest.SelectedPath}, options.roots.Manifest.Root)
	if err != nil {
		return nativeOrdinaryAssessment{}, err
	}
	paths, err := recoveryPathObservations(ctx, ordinaryViews, filesystem, manifest, options.resolver, options.documents.OrdinaryCapabilities, options.documents.Codecs, options.documents.Budget)
	if err != nil {
		return nativeOrdinaryAssessment{}, err
	}
	intents, err := journal.ProvisionalAcquires()
	if err != nil {
		return nativeOrdinaryAssessment{}, err
	}
	paths, err = applyRecoveryIntentOwnershipEvidence(ctx, paths, intents, registry, options.resolver, options.documents.Budget)
	if err != nil {
		return nativeOrdinaryAssessment{}, err
	}
	checkpoints, err := ordinaryRetirementCheckpoints(journal, ctx, paths, options)
	if err != nil {
		return nativeOrdinaryAssessment{}, err
	}
	assessed, err := recovery.AssessEntriesWithRemovalCleanup(entries, canonicalRecoveryPathEvidence(paths), canonicalRecoveryBackupEvidence(backups), checkpoints)
	if err != nil {
		return nativeOrdinaryAssessment{}, err
	}
	if !assessed.Recoverable() {
		return nativeOrdinaryAssessment{}, &nativeSelectedPhaseError{cause: fmt.Errorf("ordinary host paths differ from the recorded recovery envelope")}
	}
	return nativeOrdinaryAssessment{entries: assessed, paths: paths, backups: backups, checkpoints: checkpoints}, nil
}

func ordinaryRetirementCheckpoints(journal journalrecord.Native, ctx context.Context, paths []recoveryPathObservation, options nativeDocumentRecoveryReadOptions) ([]recovery.RemovalCleanupObligation, error) {
	absent := make(map[string]bool)
	for _, path := range paths {
		if path.ContentPath == "" && !path.Exists && !path.PathExisted && path.Error == "" && path.BlockedReason == "" {
			absent[path.Path] = true
		}
	}
	needed := make(map[string]bool)
	entries, err := journal.RecordedEntries()
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if absent[entry.Path()] && entry.ContentPath() == "" && entry.Backup().Existed() && entry.ExpectedExists() {
			needed[entry.Path()] = true
		}
	}
	if len(needed) == 0 {
		return nil, nil
	}
	intents, err := journal.OrdinaryRemovalIntents()
	if err != nil {
		return nil, err
	}
	filesystem, supported := options.documents.Filesystem.(mutationfs.Reader)
	if !supported {
		return nil, fmt.Errorf("ordinary removal observation requires selected filesystem ports")
	}
	var checkpoints []recovery.RemovalCleanupObligation
	for _, intent := range intents {
		if !needed[intent.Destination().String()] {
			continue
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if err := options.documents.Budget.AdmitObservation(); err != nil {
			return nil, err
		}
		checkpoint, err := observeRemovalCleanupIntent(ctx, intent, PlanLoadOptions{Filesystem: filesystem, Resolver: options.resolver}, options.documents.Budget, false)
		if err != nil {
			return nil, err
		}
		checkpoints = append(checkpoints, checkpoint)
	}
	return checkpoints, nil
}

type nativeOrdinaryRootBinding struct {
	root        *rootedpath.CapturedRoot
	destination rootedpath.Destination
}

// The independent resolver selects exact logical destinations. Recorded roots
// are reopened only after matching those paths and their capture-time ancestry.
func captureNativeOrdinaryRecoveryRoots(ctx context.Context, entries []journalrecord.PathEntry, resolver func(output.Destination) (string, error), supplied RootedCapabilityResolver, budget *physicalwork.Budget) (selected RootedCapabilityResolver, retained []*rootedpath.CapturedRoot, resultErr error) {
	if supplied != nil {
		return supplied, nil, nil
	}
	defer func() {
		if resultErr != nil {
			for _, root := range retained {
				resultErr = errors.Join(resultErr, root.Close())
			}
			selected, retained = nil, nil
		}
	}()
	bindings := make(map[output.Destination]nativeOrdinaryRootBinding)
	currentRoots := make(map[string]rootedpath.Authority)
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, retained, err
		}
		if err := entry.Validate(); err != nil {
			return nil, retained, err
		}
		if entry.Scope() != target.ScopeGlobal {
			continue
		}
		destination, err := output.Parse(entry.Path())
		if err != nil {
			return nil, retained, err
		}
		if _, present := bindings[destination]; present {
			continue
		}
		if resolver == nil || !entry.HasGlobalBinding() {
			return nil, retained, fmt.Errorf("ordinary global recovery requires independent selection and recorded root evidence")
		}
		observed, err := observeRecoveryGlobalPathBindingForRecovery(destination, resolver, nil, budget)
		if err != nil {
			return nil, retained, err
		}
		if err := matchRecordedGlobalBinding(entry, observed, currentRoots, budget); err != nil {
			return nil, retained, err
		}
		_, expected, err := entry.GlobalBinding()
		if err != nil {
			return nil, retained, err
		}
		root, err := rootedpath.CaptureCanonicalRootNoFollowBounded(expected.PhysicalRoot(), physicalwork.MaximumPhysicalPathDepth, budget)
		if err != nil {
			return nil, retained, err
		}
		retained = append(retained, root)
		authority, err := root.AuthorityBounded(budget)
		if err != nil {
			return nil, retained, err
		}
		if err := expected.Match(authority); err != nil {
			return nil, retained, err
		}
		path, err := filepath.Rel(expected.PhysicalRoot(), observed.resolvedPath)
		if err != nil {
			return nil, retained, err
		}
		relative, err := rootedpath.NewRelativeDestination(filepath.ToSlash(path))
		if err != nil {
			return nil, retained, err
		}
		bound, err := authority.Bind(relative)
		if err != nil {
			return nil, retained, err
		}
		bindings[destination] = nativeOrdinaryRootBinding{root: root, destination: bound}
	}
	return func(destination output.Destination, allocation rootedpath.PhysicalTraversalBudget) (rootedpath.CommitCapability, bool, error) {
		binding, present := bindings[destination]
		if !present {
			return nil, false, nil
		}
		if allocation == nil {
			allocation = budget
		}
		capability, err := binding.root.AcquireBounded(binding.destination, physicalwork.MaximumPhysicalPathDepth, allocation)
		return capability, true, err
	}, retained, nil
}
