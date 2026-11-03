package journal

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/isty2e/daem/internal/effect/journal/recovery"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
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

func (journal nativeJournalSnapshots) ordinaryBackups(ctx context.Context, directory string, active ActiveJournalAuthority, filesystem mutationfs.RootedReader, budget *recovery.PhysicalWorkBudget) ([]recovery.Entry, []recoveryBackupObservation, error) {
	entries := make([]recovery.Entry, 0, len(journal.record.Entries))
	for _, record := range journal.record.Entries {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		entry, err := canonicalRecoveryEntry(record)
		if err != nil {
			return nil, nil, err
		}
		entries = append(entries, entry)
	}
	backups, err := recoveryBackupObservations(ctx, directory, active, journal.record.Entries, filesystem, budget)
	if err != nil {
		return nil, nil, err
	}
	if err := recovery.ValidateBackups(entries, canonicalRecoveryBackupEvidence(backups)); err != nil {
		return nil, nil, err
	}
	return entries, backups, nil
}

func (journal nativeJournalSnapshots) assessOrdinaryRecovery(ctx context.Context, options nativeDocumentRecoveryReadOptions, registry ownership.Registry) (nativeOrdinaryAssessment, error) {
	entries, backups, err := journal.ordinaryBackups(ctx, options.directory, options.active, options.documents.filesystem, options.documents.budget)
	if err != nil {
		return nativeOrdinaryAssessment{}, err
	}
	if len(entries) == 0 {
		assessed, err := recovery.AssessEntries(nil, nil, nil)
		return nativeOrdinaryAssessment{entries: assessed}, err
	}
	filesystem, supported := options.documents.filesystem.(mutationfs.Reader)
	if !supported {
		return nativeOrdinaryAssessment{}, fmt.Errorf("ordinary recovery requires its selected observation ports")
	}
	if err := validateRecoveryGlobalPathBindings(ctx, journal.record.Entries, options.resolver, options.documents.ordinaryCapabilities, options.documents.budget); err != nil {
		return nativeOrdinaryAssessment{}, err
	}
	work := &ordinaryCaptureWork{budget: options.documents.budget}
	manifest, err := work.manifest(Paths{ManifestRoot: options.roots.Manifest.SelectedPath}, options.roots.Manifest.Root)
	if err != nil {
		return nativeOrdinaryAssessment{}, err
	}
	paths, err := recoveryPathObservations(ctx, journal.record.Entries, filesystem, manifest, options.resolver, options.documents.ordinaryCapabilities, options.documents.codecs, options.documents.budget)
	if err != nil {
		return nativeOrdinaryAssessment{}, err
	}
	intents, err := canonicalProvisionalAcquireIntents(journal.record.ProvisionalAcquires)
	if err != nil {
		return nativeOrdinaryAssessment{}, err
	}
	paths, err = applyRecoveryIntentOwnershipEvidence(ctx, paths, intents, registry, options.resolver, options.documents.budget)
	if err != nil {
		return nativeOrdinaryAssessment{}, err
	}
	checkpoints, err := journal.ordinaryRetirementCheckpoints(ctx, paths, options)
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

func (journal nativeJournalSnapshots) ordinaryRetirementCheckpoints(ctx context.Context, paths []recoveryPathObservation, options nativeDocumentRecoveryReadOptions) ([]recovery.RemovalCleanupObligation, error) {
	absent := make(map[string]bool)
	for _, path := range paths {
		if path.ContentPath == "" && !path.Exists && !path.PathExisted && path.Error == "" && path.BlockedReason == "" {
			absent[path.Path] = true
		}
	}
	needed := make(map[string]bool)
	for _, entry := range journal.record.Entries {
		if absent[entry.Path] && entry.ContentPath == "" && entry.Before.Existed && entry.ExpectedAfter.Existed {
			needed[entry.Path] = true
		}
	}
	if len(needed) == 0 {
		return nil, nil
	}
	intents, err := canonicalRecoveryRemovalIntents(journal.record.RemovalIntents)
	if err != nil {
		return nil, err
	}
	filesystem, supported := options.documents.filesystem.(mutationfs.Reader)
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
		if err := options.documents.budget.AdmitObservation(); err != nil {
			return nil, err
		}
		checkpoint, err := observeRemovalCleanupIntent(ctx, intent, PlanLoadOptions{Filesystem: filesystem, Resolver: options.resolver}, options.documents.budget, false)
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
func captureNativeOrdinaryRecoveryRoots(ctx context.Context, entries []recoveryEntry, resolver func(output.Destination) (string, error), supplied RootedCapabilityResolver, budget *recovery.PhysicalWorkBudget) (selected RootedCapabilityResolver, retained []*rootedpath.CapturedRoot, resultErr error) {
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
		if entry.Scope != string(target.ScopeGlobal) {
			continue
		}
		destination, err := output.Parse(entry.Path)
		if err != nil {
			return nil, retained, err
		}
		if _, present := bindings[destination]; present {
			continue
		}
		if resolver == nil || entry.GlobalPathBinding == nil {
			return nil, retained, fmt.Errorf("ordinary global recovery requires independent selection and recorded root evidence")
		}
		observed, err := observeRecoveryGlobalPathBindingForRecovery(destination, resolver, nil, budget)
		if err != nil {
			return nil, retained, err
		}
		if err := entry.GlobalPathBinding.match(observed, currentRoots, budget); err != nil {
			return nil, retained, err
		}
		_, expected, err := entry.GlobalPathBinding.canonical()
		if err != nil {
			return nil, retained, err
		}
		root, err := rootedpath.CaptureCanonicalRootNoFollowBounded(expected.PhysicalRoot(), recovery.MaximumPhysicalPathDepth, budget)
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
		capability, err := binding.root.AcquireBounded(binding.destination, recovery.MaximumPhysicalPathDepth, allocation)
		return capability, true, err
	}, retained, nil
}
