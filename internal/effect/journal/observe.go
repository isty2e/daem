package journal

import (
	"context"
	"errors"
	"fmt"

	journalinventory "github.com/isty2e/daem/internal/effect/journal/inventory"

	journalrecord "github.com/isty2e/daem/internal/effect/journal/record"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/effect/mutation"

	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/effect/workmeasure"
	"github.com/isty2e/daem/internal/output"

	outputownership "github.com/isty2e/daem/internal/output/ownership"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/target"
)

type recoveryPathObservation struct {
	Path          string
	ContentPath   string
	Exists        bool
	PathExisted   bool
	PathMode      *recovery.PermissionMode
	Kind          string
	ContentHash   string
	LinkTarget    string
	BlockedReason string
	BlockedDetail string
	Error         string
	Work          workmeasure.ArtifactWork
}

type recoveryBackupObservation struct {
	BackupPath  string
	Exists      bool
	Kind        string
	ContentHash string
	Error       string
	Work        workmeasure.ArtifactWork
}

func applyRecoveryIntentOwnershipEvidence(
	ctx context.Context,
	observations []recoveryPathObservation,
	intents []outputownership.ProvisionalAcquireIntent,
	registry outputownership.Registry,
	resolver func(output.Destination) (string, error),
	budget *physicalwork.Budget,
) ([]recoveryPathObservation, error) {
	observationIndexes := make(map[recoveryPathObservationKey]int, len(observations))
	for index, observation := range observations {
		key := recoveryPathObservationKey{path: observation.Path, contentPath: observation.ContentPath}
		if _, present := observationIndexes[key]; !present {
			observationIndexes[key] = index
		}
	}
	for _, intent := range intents {
		contentPath, err := intent.LegacyContentPath()
		if err != nil {
			return nil, err
		}
		index, found := observationIndexes[recoveryPathObservationKey{
			path:        intent.Destination().String(),
			contentPath: string(contentPath),
		}]
		if !found {
			continue
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if resolver == nil {
			observations[index].Error = "ownership intent destination resolver is required"
			continue
		}
		hostPath, err := resolver(intent.Destination())
		if err != nil {
			observations[index].Error = fmt.Sprintf("resolve ownership intent destination: %v", err)
			continue
		}
		authority, err := mutation.ObserveDirectoryEntryAuthorityBounded(
			hostPath,
			physicalwork.MaximumPhysicalPathDepth,
			budget,
		)
		if err != nil {
			return nil, fmt.Errorf("observe ownership intent destination: %w", err)
		}

		var conflict outputownership.Claim
		var present bool
		if exact, ok := authority.Exact(); ok {
			address, addressErr := outputownership.NewManagedAddress(exact, intent.Footprint())
			if addressErr != nil {
				observations[index].Error = addressErr.Error()
				continue
			}
			if admitErr := intent.AdmitAddress(address); admitErr != nil {
				observations[index].Error = fmt.Sprintf("ownership intent path authority changed: %v", admitErr)
				continue
			}
			conflict, present = registry.Conflict(address)
		} else if provisional, ok := authority.Provisional(); ok {
			if !provisional.Equal(intent.Path()) {
				observations[index].Error = "ownership intent provisional path authority changed"
				continue
			}
			conflict, present = registry.ProvisionalAncestorConflict(provisional)
		} else {
			observations[index].Error = "ownership intent path authority observation is empty"
			continue
		}
		if present {
			observations[index].BlockedReason = "ownership_conflict"
			observations[index].BlockedDetail = fmt.Sprintf(
				"recovery output overlaps a durable claim owned by manifest %q",
				conflict.Owner().ManifestPath(),
			)
		}
	}
	return observations, nil
}

func recoveryPathObservations(
	ctx context.Context,
	entries []journalrecord.PathEntry,
	filesystem mutationfs.Reader,
	manifestAuthority *manifestAuthoritySession,
	resolver func(destination output.Destination) (string, error),
	rootedCapability RootedCapabilityResolver,
	codecs aggregate.CodecCatalog,
	budget *physicalwork.Budget,
) ([]recoveryPathObservation, error) {
	observations := make([]recoveryPathObservation, 0, len(entries))
	seen := make(map[recoveryPathObservationKey]struct{}, len(entries))
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if err := entry.Validate(); err != nil {
			return nil, err
		}
		key := recoveryPathObservationKey{path: entry.Path(), contentPath: entry.ContentPath()}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		aggregateContract, err := entry.AggregateContract()
		if err != nil {
			observations = append(observations, recoveryPathObservation{
				Path: entry.Path(), ContentPath: entry.ContentPath(), Error: err.Error(),
			})
			continue
		}

		if entry.Scope() == target.ScopeProject {
			observation, observeErr := observeProjectRecoveryPath(
				ctx,
				entry.Path(),
				entry.ContentPath(),
				aggregateContract,
				filesystem,
				manifestAuthority,
				codecs,
				budget,
			)
			if observeErr != nil {
				return nil, fmt.Errorf("observe project recovery path %q: %w", entry.Path(), observeErr)
			}
			observations = append(observations, observation)
			continue
		}
		destination, err := output.Parse(entry.Path())
		if err != nil {
			observations = append(observations, recoveryPathObservation{
				Path: entry.Path(), ContentPath: entry.ContentPath(), Error: err.Error(),
			})
			continue
		}
		hostPath, err := resolver(destination)
		if err != nil {
			observations = append(observations, recoveryPathObservation{
				Path:        entry.Path(),
				ContentPath: entry.ContentPath(),
				Error:       err.Error(),
			})
			continue
		}
		observation, observeErr := observeGlobalRecoveryPath(
			ctx,
			entry.Path(),
			entry.ContentPath(),
			aggregateContract,
			hostPath,
			filesystem,
			rootedCapability,
			codecs,
			budget,
		)
		if observeErr != nil {
			return nil, fmt.Errorf("observe recovery path %q: %w", entry.Path(), observeErr)
		}
		observations = append(observations, observation)
	}

	return observations, nil
}

type recoveryPathObservationKey struct {
	path        string
	contentPath string
}

func recoveryBackupObservations(
	ctx context.Context,
	operationDir string,
	activeAuthority ActiveJournalAuthority,
	entries []journalrecord.PathEntry,
	filesystem mutationfs.RootedReader,
	budget *physicalwork.Budget,
) ([]recoveryBackupObservation, error) {
	backupPaths := make([]string, 0, len(entries))
	seen := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if err := entry.Validate(); err != nil {
			return nil, err
		}
		if !entry.Backup().Existed() ||
			(entry.Backup().Kind() != recovery.PathKindFile && entry.Backup().Kind() != recovery.PathKindDirectory) {
			continue
		}
		if _, exists := seen[entry.Backup().Path()]; exists {
			continue
		}
		seen[entry.Backup().Path()] = struct{}{}
		backupPaths = append(backupPaths, entry.Backup().Path())
	}
	if len(backupPaths) == 0 {
		return []recoveryBackupObservation{}, nil
	}
	if filesystem == nil {
		return nil, fmt.Errorf("recovery backup filesystem is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	root, err := rootedpath.CaptureCanonicalRootNoFollowBounded(
		operationDir,
		physicalwork.MaximumPhysicalPathDepth,
		budget,
	)
	if err != nil {
		return nil, fmt.Errorf("bind recovery operation directory: %w", err)
	}
	observations, observeErr := observeRecoveryBackupsRooted(
		ctx,
		root,
		activeAuthority,
		backupPaths,
		filesystem,
		budget,
	)
	return observations, errors.Join(observeErr, root.Close())
}

func observeRecoveryBackupsRooted(
	ctx context.Context,
	root *rootedpath.CapturedRoot,
	activeAuthority ActiveJournalAuthority,
	backupPaths []string,
	filesystem mutationfs.RootedReader,
	budget *physicalwork.Budget,
) ([]recoveryBackupObservation, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := journalinventory.ValidateActiveJournalRoot(
		ctx,
		filesystem,
		root,
		budget,
		activeAuthority,
	); err != nil {
		return nil, fmt.Errorf("revalidate recovery operation directory: %w", err)
	}

	rootAuthority, err := root.AuthorityBounded(budget)
	if err != nil {
		return nil, err
	}
	observations := make([]recoveryBackupObservation, 0, len(backupPaths))
	for _, backupPath := range backupPaths {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		backupRelative, err := rootedpath.NewRelativeDestination(backupPath)
		if err != nil {
			return nil, fmt.Errorf("recovery backup %q: %w", backupPath, err)
		}
		destination, err := rootAuthority.Bind(backupRelative)
		if err != nil {
			return nil, fmt.Errorf("bind recovery backup %q: %w", backupPath, err)
		}
		capability, err := root.AcquireBounded(
			destination,
			physicalwork.MaximumPhysicalPathDepth,
			budget,
		)
		if err != nil {
			return nil, fmt.Errorf("acquire recovery backup %q: %w", backupPath, err)
		}
		observation, observeErr := observeRecoveryBackup(
			ctx,
			backupPath,
			filesystem,
			capability,
			budget,
		)
		closeErr := capability.Close()
		if observeErr != nil {
			return nil, errors.Join(
				fmt.Errorf("observe recovery backup %q: %w", backupPath, observeErr),
				closeErr,
			)
		}
		if closeErr != nil {
			return nil, closeErr
		}
		observations = append(observations, observation)
	}

	return observations, nil
}

func recoveryRegularFileMaximumBytes(
	contentPath string,
	aggregateContract *aggregate.ProjectionContract,
	codecs aggregate.CodecCatalog,
) (int64, error) {
	if contentPath == "" {
		if aggregateContract != nil {
			return 0, fmt.Errorf("whole-path recovery observation carries an aggregate contract")
		}
		return recovery.MaximumRecoveryBackupFileBytes, nil
	}
	if aggregateContract == nil {
		return 0, fmt.Errorf("recovery content path %q has no aggregate contract", contentPath)
	}
	contract := aggregateContract.Clone()
	if err := contract.Validate(); err != nil {
		return 0, fmt.Errorf("recovery aggregate contract: %w", err)
	}
	codec, ok := codecs.Lookup(contract.CodecContractID())
	if !ok {
		return 0, fmt.Errorf("unsupported recovery aggregate codec %q", contract.CodecContractID())
	}
	return codec.MaximumDocumentBytes(), nil
}

func extractRecoveryObservationProjection(
	content []byte,
	destination output.Destination,
	contentPath output.ContentPath,
	aggregateContract *aggregate.ProjectionContract,
	codecs aggregate.CodecCatalog,
) ([]byte, bool, error) {
	if aggregateContract == nil {
		return nil, false, fmt.Errorf(
			"recovery content path %q has no aggregate contract",
			contentPath,
		)
	}
	address := aggregateContract.Address()
	contractPath, err := address.LegacyContentPath()
	if err != nil {
		return nil, false, err
	}
	if address.Document().AggregateRoot() != destination ||
		string(contractPath) != string(contentPath) {
		return nil, false, fmt.Errorf(
			"recovery aggregate contract address %q%q does not match observation %q%q",
			address.Document().AggregateRoot(),
			contractPath,
			destination,
			contentPath,
		)
	}
	selection, err := aggregate.NewSelection([]aggregate.ProjectionContract{aggregateContract.Clone()})
	if err != nil {
		return nil, false, err
	}
	codec, ok := codecs.Lookup(aggregateContract.CodecContractID())
	if !ok {
		return nil, false, fmt.Errorf("unsupported recovery aggregate codec %q", aggregateContract.CodecContractID())
	}
	snapshot, failure := codec.Read(aggregate.ExistingDocument(content), selection)
	if failure != nil {
		return nil, false, failure
	}
	states := snapshot.States()
	if len(states) != 1 {
		return nil, false, fmt.Errorf("recovery aggregate codec returned %d states", len(states))
	}
	return []byte(states[0].CanonicalProjection()), states[0].Present(), nil
}

func observeRecoveryBackup(
	ctx context.Context,
	journalPath string,
	filesystem mutationfs.RootedReader,
	capability rootedpath.CommitCapability,
	budget *physicalwork.Budget,
) (recoveryBackupObservation, error) {
	observed, err := observeRootedRecoveryCapability(
		ctx,
		journalPath,
		"",
		nil,
		filesystem,
		capability,
		aggregate.CodecCatalog{},
		budget,
	)
	if err != nil {
		return recoveryBackupObservation{}, err
	}
	result := recoveryBackupObservation{
		BackupPath:  journalPath,
		Exists:      observed.Exists,
		Kind:        observed.Kind,
		ContentHash: observed.ContentHash,
		Error:       observed.Error,
		Work:        observed.Work,
	}
	if result.Exists && result.Error == "" &&
		result.Kind != recovery.PathKindFile && result.Kind != recovery.PathKindDirectory {
		result.Error = fmt.Sprintf("unsupported backup kind %q", result.Kind)
	}
	return result, nil
}

func recoveryArtifactWorkLimits(
	directory bool,
	maximumFileBytes int64,
	budget *physicalwork.Budget,
) (workmeasure.ArtifactWork, workmeasure.ArtifactWork, error) {
	if budget == nil {
		return workmeasure.ArtifactWork{}, workmeasure.ArtifactWork{}, fmt.Errorf(
			"recovery artifact work budget is required",
		)
	}
	if maximumFileBytes <= 0 {
		return workmeasure.ArtifactWork{}, workmeasure.ArtifactWork{}, fmt.Errorf(
			"recovery artifact file-byte limit must be positive",
		)
	}
	remaining := budget.RemainingTreeWork()
	maximumEntries := 0
	readerEntries := 0
	maximumBytes := min(maximumFileBytes, remaining.Bytes())
	if directory {
		if remaining.Entries() <= 0 {
			return workmeasure.ArtifactWork{}, workmeasure.ArtifactWork{}, fmt.Errorf(
				"recovery directory observation exceeds remaining operation entry capacity",
			)
		}
		maximumEntries = min(recovery.MaximumArtifactTreeEntries, remaining.Entries()-1)
		readerEntries = maximumEntries + 1
		maximumBytes = min(recovery.MaximumArtifactTreeBytes, remaining.Bytes())
	}
	maximum, err := workmeasure.NewArtifactWork(maximumEntries, maximumBytes)
	if err != nil {
		return workmeasure.ArtifactWork{}, workmeasure.ArtifactWork{}, err
	}
	readerCapacity, err := workmeasure.NewArtifactWork(
		readerEntries,
		max(int64(1), maximumBytes),
	)
	return maximum, readerCapacity, err
}

func canonicalRecoveryPathEvidence(values []recoveryPathObservation) []recovery.PathEvidence {
	result := make([]recovery.PathEvidence, len(values))
	for index, value := range values {
		mode := value.PathMode
		if mode != nil {
			copied := *mode
			mode = &copied
		}
		result[index] = recovery.PathEvidence{
			Path:          value.Path,
			ContentPath:   value.ContentPath,
			Exists:        value.Exists,
			PathExisted:   value.PathExisted,
			PathMode:      mode,
			Kind:          value.Kind,
			ContentHash:   value.ContentHash,
			LinkTarget:    value.LinkTarget,
			BlockedReason: value.BlockedReason,
			BlockedDetail: value.BlockedDetail,
			Error:         value.Error,
		}
	}
	return result
}

func canonicalRecoveryBackupEvidence(values []recoveryBackupObservation) []recovery.BackupEvidence {
	result := make([]recovery.BackupEvidence, len(values))
	for index, value := range values {
		result[index] = recovery.BackupEvidence{
			BackupPath:  value.BackupPath,
			Exists:      value.Exists,
			Kind:        value.Kind,
			ContentHash: value.ContentHash,
			Error:       value.Error,
			Work:        value.Work,
		}
	}
	return result
}
