package journal

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/effect/mutation"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/supply/artifact/access"
	"github.com/isty2e/daem/internal/target"
)

type recoveryPathObservation struct {
	Path        string
	ContentPath string
	Exists      bool
	PathExisted bool
	PathMode    *recovery.PermissionMode
	Kind        string
	ContentHash string
	LinkTarget  string
	Error       string
}

type recoveryBackupObservation struct {
	BackupPath  string
	Exists      bool
	Kind        string
	ContentHash string
	Error       string
}

func recoveryPathObservations(
	ctx context.Context,
	entries []recoveryEntry,
	filesystem mutationfs.Reader,
	projectAuthority *projectAuthoritySession,
	resolver func(destination output.Destination) (string, error),
	rootedCapability RootedCapabilityResolver,
	codecs aggregate.CodecCatalog,
) []recoveryPathObservation {
	observations := make([]recoveryPathObservation, 0, len(entries))
	seen := make(map[recoveryPathObservationKey]struct{}, len(entries))
	for _, entry := range entries {
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
			observations = append(
				observations,
				observeProjectRecoveryPath(
					ctx,
					entry.Path,
					entry.ContentPath,
					aggregateContract,
					filesystem,
					projectAuthority,
					codecs,
				),
			)
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
		if rootedCapability != nil {
			observations = append(
				observations,
				observeGlobalRecoveryPath(
					ctx,
					entry.Path,
					entry.ContentPath,
					aggregateContract,
					hostPath,
					filesystem,
					rootedCapability,
					codecs,
				),
			)
			continue
		}

		observations = append(observations, observeRecoveryPath(
			ctx, filesystem, entry.Path, entry.ContentPath, hostPath, aggregateContract, codecs,
		))
	}

	return observations
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

func recoveryBackupObservations(ctx context.Context, operationDir string, entries []recoveryEntry) []recoveryBackupObservation {
	observations := make([]recoveryBackupObservation, 0, len(entries))
	seen := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		if !entry.Before.Existed ||
			(entry.Before.Kind != recovery.PathKindFile && entry.Before.Kind != recovery.PathKindDirectory) {
			continue
		}
		if _, exists := seen[entry.Before.BackupPath]; exists {
			continue
		}
		seen[entry.Before.BackupPath] = struct{}{}

		backupPath := filepath.Join(operationDir, filepath.FromSlash(entry.Before.BackupPath))
		observations = append(observations, observeRecoveryBackup(ctx, entry.Before.BackupPath, backupPath))
	}

	return observations
}

func observeRecoveryPath(
	ctx context.Context,
	filesystem mutationfs.PathReader,
	journalPath string,
	contentPath string,
	hostPath string,
	aggregateContract *aggregate.ProjectionContract,
	codecs aggregate.CodecCatalog,
) recoveryPathObservation {
	info, err := os.Lstat(hostPath)
	if err != nil {
		if os.IsNotExist(err) {
			return recoveryPathObservation{Path: journalPath, ContentPath: contentPath, Exists: false}
		}
		return recoveryPathObservation{Path: journalPath, ContentPath: contentPath, Error: fmt.Sprintf("stat destination: %v", err)}
	}
	if info.Mode()&os.ModeSymlink != 0 {
		linkTarget, err := os.Readlink(hostPath)
		if err != nil {
			return recoveryPathObservation{Path: journalPath, ContentPath: contentPath, Error: fmt.Sprintf("read symlink destination: %v", err)}
		}
		if contentPath != "" {
			return recoveryPathObservation{Path: journalPath, ContentPath: contentPath, Exists: true, Error: "content path requires a regular file"}
		}
		return recoveryPathObservation{
			Path:       journalPath,
			Exists:     true,
			Kind:       recovery.PathKindSymlink,
			LinkTarget: linkTarget,
		}
	}
	if !info.Mode().IsRegular() && !info.IsDir() {
		return recoveryPathObservation{Path: journalPath, ContentPath: contentPath, Exists: true, Error: fmt.Sprintf("unsupported file mode %s", info.Mode())}
	}
	if contentPath != "" {
		return observeRecoveryContentPath(
			ctx,
			filesystem,
			journalPath,
			contentPath,
			hostPath,
			aggregateContract,
			codecs,
		)
	}

	contentHash, artifactKind, err := access.HashPath(ctx, hostPath)
	if err != nil {
		return recoveryPathObservation{Path: journalPath, Exists: true, Error: fmt.Sprintf("hash destination: %v", err)}
	}
	if artifactKind != artifact.ArtifactKindFile && artifactKind != artifact.ArtifactKindDirectory {
		return recoveryPathObservation{Path: journalPath, Exists: true, Error: fmt.Sprintf("expected regular file or directory, found %s", artifactKind)}
	}

	return recoveryPathObservation{
		Path:        journalPath,
		Exists:      true,
		PathMode:    regularFilePermissionMode(info),
		Kind:        string(artifactKind),
		ContentHash: string(contentHash),
	}
}

func observeRecoveryContentPath(
	ctx context.Context,
	filesystem mutationfs.PathReader,
	journalPath string,
	contentPath string,
	hostPath string,
	aggregateContract *aggregate.ProjectionContract,
	codecs aggregate.CodecCatalog,
) recoveryPathObservation {
	commitPath, err := mutation.CanonicalDirectoryEntryPath(hostPath)
	if err != nil {
		return recoveryPathObservation{
			Path: journalPath, ContentPath: contentPath, Exists: true,
			Error: fmt.Sprintf("canonicalize content-path destination: %v", err),
		}
	}
	snapshot, err := filesystem.ReadRegularFileSnapshotUpTo(
		ctx,
		commitPath,
		MaximumRecoveryBackupFileBytes,
	)
	if err != nil {
		return recoveryPathObservation{Path: journalPath, ContentPath: contentPath, Exists: true, Error: fmt.Sprintf("read content-path destination: %v", err)}
	}
	content := snapshot.Content()
	destination, err := output.Parse(journalPath)
	if err != nil {
		return recoveryPathObservation{Path: journalPath, ContentPath: contentPath, Exists: true, Error: err.Error()}
	}
	projection, present, err := extractRecoveryObservationProjection(
		content,
		destination,
		output.ContentPath(contentPath),
		aggregateContract,
		codecs,
	)
	if err != nil {
		return recoveryPathObservation{Path: journalPath, ContentPath: contentPath, Exists: true, Error: err.Error()}
	}
	if !present {
		return recoveryPathObservation{
			Path:        journalPath,
			ContentPath: contentPath,
			Exists:      false,
			PathExisted: true,
			PathMode:    recovery.NewPermissionMode(snapshot.Mode()),
		}
	}
	return recoveryPathObservation{
		Path:        journalPath,
		ContentPath: contentPath,
		Exists:      true,
		PathExisted: true,
		PathMode:    recovery.NewPermissionMode(snapshot.Mode()),
		Kind:        recovery.PathKindFile,
		ContentHash: string(artifact.HashFileContent(projection)),
	}
}

func regularFilePermissionMode(info os.FileInfo) *recovery.PermissionMode {
	if !info.Mode().IsRegular() {
		return nil
	}
	return recovery.NewPermissionMode(info.Mode())
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

func observeRecoveryBackup(ctx context.Context, journalPath string, hostPath string) recoveryBackupObservation {
	info, err := os.Lstat(hostPath)
	if err != nil {
		if os.IsNotExist(err) {
			return recoveryBackupObservation{BackupPath: journalPath, Exists: false}
		}
		return recoveryBackupObservation{BackupPath: journalPath, Error: fmt.Sprintf("stat backup: %v", err)}
	}
	if !info.Mode().IsRegular() && !info.IsDir() {
		return recoveryBackupObservation{BackupPath: journalPath, Exists: true, Error: fmt.Sprintf("unsupported backup file mode %s", info.Mode())}
	}

	contentHash, artifactKind, err := access.HashPath(ctx, hostPath)
	if err != nil {
		return recoveryBackupObservation{BackupPath: journalPath, Exists: true, Error: fmt.Sprintf("hash backup: %v", err)}
	}
	if artifactKind != artifact.ArtifactKindFile && artifactKind != artifact.ArtifactKindDirectory {
		return recoveryBackupObservation{BackupPath: journalPath, Exists: true, Error: fmt.Sprintf("expected backup file or directory, found %s", artifactKind)}
	}

	return recoveryBackupObservation{
		BackupPath:  journalPath,
		Exists:      true,
		Kind:        string(artifactKind),
		ContentHash: string(contentHash),
	}
}
