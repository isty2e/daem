package qualification

import (
	"context"
	"fmt"

	journalinventory "github.com/isty2e/daem/internal/effect/journal/inventory"

	journalnamespace "github.com/isty2e/daem/internal/effect/journal/namespace"

	journalrecord "github.com/isty2e/daem/internal/effect/journal/record"
	"github.com/isty2e/daem/internal/effect/journal/recovery"

	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/output/ownership"
)

type nativeOrdinaryAssessment struct {
	paths       []recovery.PathEvidence
	backups     []recovery.BackupEvidence
	entries     recovery.EntryAssessment
	checkpoints []recovery.RemovalCleanupObligation
}

func LoadOrdinaryBackups(ctx context.Context, journal journalrecord.Native, directory string, active journalinventory.ActiveJournalAuthority, filesystem mutationfs.RootedReader, budget *physicalwork.Budget) ([]recovery.Entry, []recovery.BackupEvidence, error) {
	entries, err := journal.OrdinaryEntries(ctx)
	if err != nil {
		return nil, nil, err
	}
	views, err := journal.RecordedEntries()
	if err != nil {
		return nil, nil, err
	}
	backups, err := ObserveBackups(ctx, directory, active, views, filesystem, budget)
	if err != nil {
		return nil, nil, err
	}
	if err := recovery.ValidateBackups(entries, recovery.CloneBackupEvidence(backups)); err != nil {
		return nil, nil, err
	}
	return entries, backups, nil
}

func assessOrdinaryRecovery(journal journalrecord.Native, ctx context.Context, options NativeReadOptions, registry ownership.Registry) (nativeOrdinaryAssessment, error) {
	entries, backups, err := LoadOrdinaryBackups(ctx, journal, options.Directory, options.Active, options.Documents.Filesystem, options.Documents.Budget)
	if err != nil {
		return nativeOrdinaryAssessment{}, err
	}
	if len(entries) == 0 {
		assessed, err := recovery.AssessEntries(nil, nil, nil)
		return nativeOrdinaryAssessment{entries: assessed}, err
	}
	filesystem, supported := options.Documents.Filesystem.(mutationfs.Reader)
	if !supported {
		return nativeOrdinaryAssessment{}, fmt.Errorf("ordinary recovery requires its selected observation ports")
	}
	ordinaryViews, err := journal.RecordedEntries()
	if err != nil {
		return nativeOrdinaryAssessment{}, err
	}
	if err := ValidateGlobalPathBindings(ctx, ordinaryViews, options.Resolver, options.Documents.OrdinaryCapabilities, options.Documents.Budget); err != nil {
		return nativeOrdinaryAssessment{}, err
	}
	manifest, err := journalnamespace.CaptureManifestAuthority(options.Roots.Manifest.SelectedPath, options.Roots.Manifest.Root, options.Documents.Budget)
	if err != nil {
		return nativeOrdinaryAssessment{}, err
	}
	paths, err := ObservePaths(ctx, ordinaryViews, filesystem, manifest, options.Resolver, options.Documents.OrdinaryCapabilities, options.Documents.Codecs, options.Documents.Budget)
	if err != nil {
		return nativeOrdinaryAssessment{}, err
	}
	intents, err := journal.ProvisionalAcquires()
	if err != nil {
		return nativeOrdinaryAssessment{}, err
	}
	paths, err = ApplyIntentOwnershipEvidence(ctx, paths, intents, registry, options.Resolver, options.Documents.Budget)
	if err != nil {
		return nativeOrdinaryAssessment{}, err
	}
	checkpoints, err := ordinaryRetirementCheckpoints(journal, ctx, paths, options)
	if err != nil {
		return nativeOrdinaryAssessment{}, err
	}
	assessed, err := recovery.AssessEntriesWithRemovalCleanup(entries, recovery.ClonePathEvidence(paths), recovery.CloneBackupEvidence(backups), checkpoints)
	if err != nil {
		return nativeOrdinaryAssessment{}, err
	}
	if !assessed.Recoverable() {
		return nativeOrdinaryAssessment{}, &SelectedPhaseError{cause: fmt.Errorf("ordinary host paths differ from the recorded recovery envelope")}
	}
	return nativeOrdinaryAssessment{entries: assessed, paths: paths, backups: backups, checkpoints: checkpoints}, nil
}

func ordinaryRetirementCheckpoints(journal journalrecord.Native, ctx context.Context, paths []recovery.PathEvidence, options NativeReadOptions) ([]recovery.RemovalCleanupObligation, error) {
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
	filesystem, supported := options.Documents.Filesystem.(mutationfs.Reader)
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
		if err := options.Documents.Budget.AdmitObservation(); err != nil {
			return nil, err
		}
		checkpoint, err := ObserveRemovalCleanupIntent(ctx, intent, RemovalReadOptions{Filesystem: filesystem, Resolver: options.Resolver}, options.Documents.Budget)
		if err != nil {
			return nil, err
		}
		checkpoints = append(checkpoints, checkpoint)
	}
	return checkpoints, nil
}
