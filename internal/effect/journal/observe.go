package journal

import (
	"context"
	"errors"
	"fmt"

	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/effect/mutation"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
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
	Work          recovery.ArtifactWork
}

type recoveryBackupObservation struct {
	BackupPath  string
	Exists      bool
	Kind        string
	ContentHash string
	Error       string
	Work        recovery.ArtifactWork
}

func applyRecoveryIntentOwnershipEvidence(
	ctx context.Context,
	observations []recoveryPathObservation,
	intents []outputownership.ProvisionalAcquireIntent,
	registry outputownership.Registry,
	resolver func(output.Destination) (string, error),
	budget *recovery.PhysicalWorkBudget,
) ([]recoveryPathObservation, error) {
	observationIndexes := make(map[recoveryPathObservationKey]int, len(observations))
	for index, observation := range observations {
		key := recoveryPathObservationKey{path: observation.Path, contentPath: observation.ContentPath}
		if _, present := observationIndexes[key]; !present {
			observationIndexes[key] = index
		}
	}
	for _, intent := range intents {
		index, found := observationIndexes[recoveryPathObservationKey{
			path:        intent.Destination().String(),
			contentPath: string(intent.ContentPath()),
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
			recovery.MaximumPhysicalPathDepth,
			budget,
		)
		if err != nil {
			return nil, fmt.Errorf("observe ownership intent destination: %w", err)
		}

		var conflict outputownership.Claim
		var present bool
		if exact, ok := authority.Exact(); ok {
			address, addressErr := outputownership.NewManagedAddress(exact, string(intent.ContentPath()))
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
	entries []recoveryEntry,
	filesystem mutationfs.Reader,
	manifestAuthority *manifestAuthoritySession,
	resolver func(destination output.Destination) (string, error),
	rootedCapability RootedCapabilityResolver,
	codecs aggregate.CodecCatalog,
	budget *recovery.PhysicalWorkBudget,
) ([]recoveryPathObservation, error) {
	observations := make([]recoveryPathObservation, 0, len(entries))
	seen := make(map[recoveryPathObservationKey]struct{}, len(entries))
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		key := recoveryPathObservationKey{path: entry.Path, contentPath: entry.ContentPath}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		aggregateContract, err := canonicalRecoveryAggregateContract(entry)
		if err != nil {
			observations = append(observations, recoveryPathObservation{
				Path: entry.Path, ContentPath: entry.ContentPath, Error: err.Error(),
			})
			continue
		}

		if entry.Scope == string(target.ScopeProject) {
			observation, observeErr := observeProjectRecoveryPath(
				ctx,
				entry.Path,
				entry.ContentPath,
				aggregateContract,
				filesystem,
				manifestAuthority,
				codecs,
				budget,
			)
			if observeErr != nil {
				return nil, fmt.Errorf("observe project recovery path %q: %w", entry.Path, observeErr)
			}
			observations = append(observations, observation)
			continue
		}
		destination, err := output.Parse(entry.Path)
		if err != nil {
			observations = append(observations, recoveryPathObservation{
				Path: entry.Path, ContentPath: entry.ContentPath, Error: err.Error(),
			})
			continue
		}
		hostPath, err := resolver(destination)
		if err != nil {
			observations = append(observations, recoveryPathObservation{
				Path:        entry.Path,
				ContentPath: entry.ContentPath,
				Error:       err.Error(),
			})
			continue
		}
		observation, observeErr := observeGlobalRecoveryPath(
			ctx,
			entry.Path,
			entry.ContentPath,
			aggregateContract,
			hostPath,
			filesystem,
			rootedCapability,
			codecs,
			budget,
		)
		if observeErr != nil {
			return nil, fmt.Errorf("observe recovery path %q: %w", entry.Path, observeErr)
		}
		observations = append(observations, observation)
	}

	return observations, nil
}

func canonicalRecoveryAggregateContract(
	entry recoveryEntry,
) (*aggregate.ProjectionContract, error) {
	var contract aggregate.ProjectionContract
	if entry.Aggregate != nil {
		canonical, err := entry.Aggregate.canonical()
		if err != nil {
			return nil, fmt.Errorf("recovery aggregate contract: %w", err)
		}
		contract = canonical
	} else {
		if entry.ContentPath != "" {
			return nil, fmt.Errorf("recovery aggregate projection contract is required")
		}
		return nil, nil
	}

	subject, err := entry.Subject.canonical()
	if err != nil {
		return nil, fmt.Errorf("recovery aggregate projection subject: %w", err)
	}
	selectedTarget, err := target.ParseTarget(entry.Target)
	if err != nil {
		return nil, fmt.Errorf("recovery aggregate projection target: %w", err)
	}
	selectedScope, err := target.ParseScope(entry.Scope)
	if err != nil {
		return nil, fmt.Errorf("recovery aggregate projection scope: %w", err)
	}
	destination, err := output.Parse(entry.Path)
	if err != nil {
		return nil, fmt.Errorf("recovery aggregate projection destination: %w", err)
	}
	if err := validateAggregateProjectionCorrelation(
		subject,
		selectedTarget,
		selectedScope,
		destination,
		output.ContentPath(entry.ContentPath),
		contract,
	); err != nil {
		return nil, fmt.Errorf("recovery %w", err)
	}
	return pointerToAggregateContract(contract), nil
}

type recoveryPathObservationKey struct {
	path        string
	contentPath string
}

func recoveryBackupObservations(
	ctx context.Context,
	operationDir string,
	activeAuthority ActiveJournalAuthority,
	entries []recoveryEntry,
	filesystem mutationfs.RootedReader,
	budget *recovery.PhysicalWorkBudget,
) ([]recoveryBackupObservation, error) {
	backupPaths := make([]string, 0, len(entries))
	seen := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !entry.Before.Existed ||
			(entry.Before.Kind != recovery.PathKindFile && entry.Before.Kind != recovery.PathKindDirectory) {
			continue
		}
		if _, exists := seen[entry.Before.BackupPath]; exists {
			continue
		}
		seen[entry.Before.BackupPath] = struct{}{}
		backupPaths = append(backupPaths, entry.Before.BackupPath)
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

	root, err := rootedpath.CaptureRootNoFollowBounded(
		operationDir,
		recovery.MaximumPhysicalPathDepth,
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
	budget *recovery.PhysicalWorkBudget,
) ([]recoveryBackupObservation, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := ValidateActiveJournalRoot(
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
			recovery.MaximumPhysicalPathDepth,
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
	if address.Document().AggregateRoot() != destination ||
		string(address.ContentPath()) != string(contentPath) {
		return nil, false, fmt.Errorf(
			"recovery aggregate contract address %q%q does not match observation %q%q",
			address.Document().AggregateRoot(),
			address.ContentPath(),
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
	budget *recovery.PhysicalWorkBudget,
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
	budget *recovery.PhysicalWorkBudget,
) (recovery.ArtifactWork, recovery.ArtifactWork, error) {
	if budget == nil {
		return recovery.ArtifactWork{}, recovery.ArtifactWork{}, fmt.Errorf(
			"recovery artifact work budget is required",
		)
	}
	if maximumFileBytes <= 0 {
		return recovery.ArtifactWork{}, recovery.ArtifactWork{}, fmt.Errorf(
			"recovery artifact file-byte limit must be positive",
		)
	}
	remaining := budget.RemainingTreeWork()
	maximumEntries := 0
	readerEntries := 0
	maximumBytes := min(maximumFileBytes, remaining.Bytes())
	if directory {
		if remaining.Entries() <= 0 {
			return recovery.ArtifactWork{}, recovery.ArtifactWork{}, fmt.Errorf(
				"recovery directory observation exceeds remaining operation entry capacity",
			)
		}
		maximumEntries = min(recovery.MaximumArtifactTreeEntries, remaining.Entries()-1)
		readerEntries = maximumEntries + 1
		maximumBytes = min(recovery.MaximumArtifactTreeBytes, remaining.Bytes())
	}
	maximum, err := recovery.NewArtifactWork(maximumEntries, maximumBytes)
	if err != nil {
		return recovery.ArtifactWork{}, recovery.ArtifactWork{}, err
	}
	readerCapacity, err := recovery.NewArtifactWork(
		readerEntries,
		max(int64(1), maximumBytes),
	)
	return maximum, readerCapacity, err
}
