package authoring

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"

	declarationmanifest "github.com/isty2e/daem/internal/declaration/manifest"
	"github.com/isty2e/daem/internal/declaration/transaction"
	"github.com/isty2e/daem/internal/declarationartifact"
	"github.com/isty2e/daem/internal/effect/fileset"
	"github.com/isty2e/daem/internal/effect/journal"
	"github.com/isty2e/daem/internal/effect/mutation"
	daempaths "github.com/isty2e/daem/internal/paths"
	aggregatecodec "github.com/isty2e/daem/internal/realization/aggregate/codec"
	hookcodec "github.com/isty2e/daem/internal/realization/aggregate/codec/hook"
	mcpcodec "github.com/isty2e/daem/internal/realization/aggregate/codec/mcp"
	"github.com/isty2e/daem/internal/realization/lockfile"
	"github.com/isty2e/daem/internal/recoverygate"
	lockgenerate "github.com/isty2e/daem/internal/workflow/lock/generate"
)

// LockfileStatus describes how an authoring command would affect the lockfile.
type LockfileStatus string

const (
	// LockfileStatusWouldWrite reports that a dry-run would write a new lockfile.
	LockfileStatusWouldWrite LockfileStatus = "would_write"
	// LockfileStatusUnchanged reports that the generated lockfile matches the current lockfile.
	LockfileStatusUnchanged LockfileStatus = "unchanged"
	// LockfileStatusWritten reports that the lockfile was written by an authoring transaction.
	LockfileStatusWritten LockfileStatus = "written"
)

// LockfileChangeInput describes the prospective manifest bytes that should be locked.
type LockfileChangeInput struct {
	ManifestPath           string
	Paths                  daempaths.Paths
	LockfilePath           string
	ManifestBytes          []byte
	UsePersistentCache     bool
	PreparePersistentCache func(context.Context) error
}

// LockfileChange is the lockfile half of an authoring manifest+lock transaction.
type LockfileChange struct {
	path     string
	content  []byte
	status   LockfileStatus
	stateDir string
}

// Path returns the absolute lockfile path affected by this change.
func (change LockfileChange) Path() string {
	return change.path
}

// Status returns whether this change would write, wrote, or left the lockfile unchanged.
func (change LockfileChange) Status() LockfileStatus {
	return change.status
}

// Content returns a defensive copy of the prospective canonical lockfile.
func (change LockfileChange) Content() []byte {
	return append([]byte(nil), change.content...)
}

// BuildLockfileChange builds the prospective lockfile for manifest authoring without writing files.
func BuildLockfileChange(ctx context.Context, input LockfileChangeInput) (LockfileChange, error) {
	paths := input.Paths
	if paths.ManifestPath == "" {
		resolvedPaths, err := daempaths.Resolve(input.ManifestPath)
		if err != nil {
			return LockfileChange{}, err
		}
		paths = resolvedPaths
	}
	outputPath := input.LockfilePath
	if outputPath == "" {
		outputPath = paths.LockfilePath
	}
	current, currentErr := lockfile.ReadReplacementContent(ctx, outputPath)
	if currentErr != nil && !os.IsNotExist(currentErr) {
		return LockfileChange{}, fmt.Errorf("read lockfile: %w", currentErr)
	}

	environment, err := declarationmanifest.Decode(input.ManifestBytes)
	if err != nil {
		return LockfileChange{}, fmt.Errorf("invalid prospective manifest: %w", err)
	}
	if err := declarationmanifest.ValidatePlacement(paths, environment); err != nil {
		return LockfileChange{}, fmt.Errorf("invalid prospective manifest: %w", err)
	}
	environment, err = declarationmanifest.ResolveSelectedCarrierSources(paths, environment)
	if err != nil {
		return LockfileChange{}, fmt.Errorf("invalid prospective manifest: %w", err)
	}
	snapshot, err := lockgenerate.Build(ctx, lockgenerate.Input{
		Paths:                  paths,
		Environment:            environment,
		UsePersistentCache:     input.UsePersistentCache,
		PreparePersistentCache: input.PreparePersistentCache,
		HookEncoder:            hookcodec.CanonicalHookContribution,
		MCPEncoder:             mcpcodec.CanonicalMCPBindingContribution,
		ExtensionOrderIdentity: aggregatecodec.ExtensionOrderIdentityResolver(paths),
	})
	if err != nil {
		return LockfileChange{}, err
	}
	content := snapshot.Content()

	status := LockfileStatusWouldWrite
	if currentErr == nil {
		if bytes.Equal(current, content) {
			status = LockfileStatusUnchanged
		}
	}

	return LockfileChange{
		path:     outputPath,
		content:  content,
		status:   status,
		stateDir: paths.StateDir,
	}, nil
}

func consumedLocalPaths(input LockfileChangeInput) ([]string, error) {
	paths := input.Paths
	if paths.ManifestPath == "" {
		resolvedPaths, err := daempaths.Resolve(input.ManifestPath)
		if err != nil {
			return nil, err
		}
		paths = resolvedPaths
	}
	environment, err := declarationmanifest.Decode(input.ManifestBytes)
	if err != nil {
		return nil, fmt.Errorf("invalid prospective manifest: %w", err)
	}
	return lockgenerate.ConsumedLocalPaths(lockgenerate.Input{
		Paths:       paths,
		Environment: environment,
	})
}

// ConsumedLocalPaths returns canonical local source paths read while building
// the prospective lockfile. Cross-owner authoring workflows lease these paths
// before stale revalidation and commit.
func ConsumedLocalPaths(input LockfileChangeInput) ([]string, error) {
	return consumedLocalPaths(input)
}

func commitManifestAndLockfile(ctx context.Context, manifestPath string, manifestBytes []byte, lockfileChange LockfileChange) (LockfileChange, error) {
	writtenLockfile := lockfileChange
	if writtenLockfile.status == LockfileStatusWouldWrite {
		writtenLockfile.status = LockfileStatusWritten
	}
	err := transaction.CommitTransaction(ctx, transaction.TransactionInput{
		ManifestPath:      manifestPath,
		LockfilePath:      lockfileChange.path,
		StateDir:          lockfileChange.stateDir,
		ManifestBytes:     manifestBytes,
		LockfileBytes:     lockfileChange.content,
		SkipLockfileWrite: lockfileChange.status == LockfileStatusUnchanged,
	})
	if err != nil {
		return LockfileChange{}, err
	}

	return writtenLockfile, nil
}

type authoringCandidate struct {
	document   ManifestDocument
	change     Change
	localPaths []string
	barrier    recoverygate.EffectAuthority
}

func buildAuthoringCandidate(
	ctx context.Context,
	build authoringChangeBuilder,
	manifestPath string,
	barrier recoverygate.EffectAuthority,
) (authoringCandidate, error) {
	document, err := LoadManifestDocument(ctx, manifestPath)
	if err != nil {
		return authoringCandidate{}, OperationError{Phase: OperationPhaseLoadManifest, Err: err}
	}
	return buildAuthoringCandidateFromDocument(build, document, barrier)
}

func reloadAuthoringCandidate(
	ctx context.Context,
	build authoringChangeBuilder,
	paths daempaths.Paths,
	barrier recoverygate.EffectAuthority,
) (authoringCandidate, error) {
	document, err := loadManifestDocument(ctx, paths)
	if err != nil {
		return authoringCandidate{}, OperationError{Phase: OperationPhaseLoadManifest, Err: err}
	}
	return buildAuthoringCandidateFromDocument(build, document, barrier)
}

func buildAuthoringCandidateFromDocument(
	build authoringChangeBuilder,
	document ManifestDocument,
	barrier recoverygate.EffectAuthority,
) (authoringCandidate, error) {
	change, err := build(document)
	if err != nil {
		return authoringCandidate{}, OperationError{Phase: OperationPhaseBuildManifestChange, Err: err}
	}
	localPaths, err := consumedLocalPaths(LockfileChangeInput{
		ManifestPath:  change.ManifestPath,
		Paths:         document.Paths,
		ManifestBytes: change.Content,
	})
	if err != nil {
		return authoringCandidate{}, OperationError{Phase: OperationPhaseBuildLockfile, Err: err}
	}
	return authoringCandidate{
		document:   document,
		change:     change,
		localPaths: localPaths,
		barrier:    barrier,
	}, nil
}

func executeAuthoringMutation(
	ctx context.Context,
	options ExecutionOptions,
	build authoringChangeBuilder,
	optimistic authoringCandidate,
	usePersistentCache bool,
) (result OperationResult, returnErr error) {
	lockfilePath := options.LockfilePath
	if lockfilePath == "" {
		lockfilePath = optimistic.document.Paths.LockfilePath
	}
	markerPath, err := transaction.AuthorityPath(optimistic.document.Paths.StateDir)
	if err != nil {
		return OperationResult{}, OperationError{Phase: OperationPhaseCommit, Err: err}
	}
	domains, err := authoringMutationDomains(
		optimistic.change.ManifestPath,
		lockfilePath,
		markerPath,
		optimistic.localPaths,
	)
	if err != nil {
		return OperationResult{}, OperationError{Phase: OperationPhaseCommit, Err: err}
	}
	domains = append(domains, optimistic.barrier.Domains()...)
	store, err := mutation.NewStore(optimistic.document.Paths.DataDir)
	if err != nil {
		return OperationResult{}, OperationError{Phase: OperationPhaseCommit, Err: err}
	}
	leases, err := store.Acquire(ctx, domains...)
	if err != nil {
		return OperationResult{}, OperationError{Phase: OperationPhaseCommit, Err: err}
	}
	defer func() {
		if err := leases.Release(); err != nil {
			returnErr = errors.Join(returnErr, err)
		}
	}()
	if matches, err := leases.DomainsMatchCurrent(ctx); err != nil {
		return OperationResult{}, OperationError{Phase: OperationPhaseCommit, Err: err}
	} else if !matches {
		return OperationResult{}, OperationError{Phase: OperationPhaseCommit, Err: mutation.StaleSnapshotError{}}
	}
	if err := optimistic.barrier.ValidateFileSetRecovery(ctx); err != nil {
		return OperationResult{}, OperationError{Phase: OperationPhaseCommit, Err: err}
	}

	if err := transaction.RecoverInterruptedTransaction(
		ctx,
		optimistic.document.Paths.StateDir,
		optimistic.change.ManifestPath,
		lockfilePath,
	); err != nil {
		return OperationResult{}, OperationError{Phase: OperationPhaseCommit, Err: err}
	}
	revisionRequests, err := authoringRevisionRequests(
		optimistic.change.ManifestPath,
		lockfilePath,
		markerPath,
		optimistic.localPaths,
	)
	if err != nil {
		return OperationResult{}, OperationError{Phase: OperationPhaseCommit, Err: err}
	}
	revisionRequests = append(revisionRequests, optimistic.barrier.RevisionRequests()...)
	revisions, err := mutation.CaptureRevisionSet(ctx, revisionRequests...)
	if err != nil {
		return OperationResult{}, OperationError{Phase: OperationPhaseCommit, Err: err}
	}
	current, err := reloadAuthoringCandidate(ctx, build, optimistic.document.Paths, optimistic.barrier)
	if err != nil {
		return OperationResult{}, err
	}
	if !equalAuthoringPathLists(optimistic.localPaths, current.localPaths) ||
		!optimistic.barrier.Equal(current.barrier) {
		return OperationResult{}, OperationError{Phase: OperationPhaseCommit, Err: mutation.StaleSnapshotError{}}
	}
	if err := optimistic.barrier.Validate(ctx); err != nil {
		return OperationResult{}, OperationError{Phase: OperationPhaseCommit, Err: err}
	}
	lockfile, err := BuildLockfileChange(ctx, LockfileChangeInput{
		ManifestPath:       current.change.ManifestPath,
		Paths:              current.document.Paths,
		LockfilePath:       options.LockfilePath,
		ManifestBytes:      current.change.Content,
		UsePersistentCache: usePersistentCache,
		PreparePersistentCache: func(ctx context.Context) error {
			return ensureStateDirForAuthoringEffect(ctx, optimistic.barrier, revisions, leases)
		},
	})
	if err != nil {
		return OperationResult{}, OperationError{Phase: OperationPhaseBuildLockfile, Err: err}
	}
	if err := ensureStateDirForAuthoringEffect(ctx, optimistic.barrier, revisions, leases); err != nil {
		return OperationResult{}, OperationError{Phase: OperationPhaseCommit, Err: err}
	}
	matches, err := revisions.MatchesCurrent(ctx)
	if err != nil {
		return OperationResult{}, OperationError{Phase: OperationPhaseCommit, Err: err}
	}
	if !matches {
		return OperationResult{}, OperationError{Phase: OperationPhaseCommit, Err: mutation.StaleSnapshotError{}}
	}
	if matches, err := leases.DomainsMatchCurrent(ctx); err != nil {
		return OperationResult{}, OperationError{Phase: OperationPhaseCommit, Err: err}
	} else if !matches {
		return OperationResult{}, OperationError{Phase: OperationPhaseCommit, Err: mutation.StaleSnapshotError{}}
	}
	if err := optimistic.barrier.Validate(ctx); err != nil {
		return OperationResult{}, OperationError{Phase: OperationPhaseCommit, Err: err}
	}
	lockfile, err = commitManifestAndLockfile(ctx, current.change.ManifestPath, current.change.Content, lockfile)
	if err != nil {
		return OperationResult{}, OperationError{Phase: OperationPhaseCommit, Err: err}
	}
	return operationResultFromChange(current.change, lockfile, options.Mode), nil
}

func ensureStateDirForAuthoringEffect(
	ctx context.Context,
	barrier recoverygate.EffectAuthority,
	revisions mutation.RevisionSet,
	leases *mutation.LeaseSet,
) error {
	_, err := barrier.EnsureStateDirForEffect(ctx, func(ctx context.Context) error {
		matches, err := revisions.MatchesCurrent(ctx)
		if err != nil {
			return err
		}
		if !matches {
			return mutation.StaleSnapshotError{}
		}
		matches, err = leases.DomainsMatchCurrent(ctx)
		if err != nil {
			return err
		}
		if !matches {
			return mutation.StaleSnapshotError{}
		}
		return nil
	})
	return err
}

func authoringMutationDomains(
	manifestPath string,
	lockfilePath string,
	markerPath string,
	localPaths []string,
) ([]mutation.Domain, error) {
	requests := []mutation.LogicalPathRequest{
		{Path: manifestPath, Access: mutation.AccessExclusive, Effect: mutation.PathEffectDirectoryEntry},
		{Path: manifestPath, Access: mutation.AccessShared, Effect: mutation.PathEffectReferent},
		{Path: lockfilePath, Access: mutation.AccessExclusive, Effect: mutation.PathEffectDirectoryEntry},
		{Path: lockfilePath, Access: mutation.AccessShared, Effect: mutation.PathEffectReferent},
		{Path: markerPath, Access: mutation.AccessExclusive, Effect: mutation.PathEffectDirectoryEntry},
	}
	for _, path := range localPaths {
		requests = append(requests, mutation.LogicalPathRequest{Path: path, Access: mutation.AccessShared, Effect: mutation.PathEffectReferent})
	}
	domains := make([]mutation.Domain, 0, len(requests))
	for _, request := range requests {
		domain, err := mutation.NewLogicalPathDomain(request)
		if err != nil {
			return nil, fmt.Errorf("build authoring mutation domain: %w", err)
		}
		domains = append(domains, domain)
	}
	return domains, nil
}

func metadataMutationDomains(
	targetPaths []string,
	markerPath string,
	localPaths []string,
) ([]mutation.Domain, error) {
	requests := make(
		[]mutation.LogicalPathRequest,
		0,
		len(targetPaths)*2+1+len(localPaths),
	)
	for _, path := range targetPaths {
		requests = append(
			requests,
			mutation.LogicalPathRequest{
				Path: path, Access: mutation.AccessExclusive, Effect: mutation.PathEffectDirectoryEntry,
			},
			mutation.LogicalPathRequest{
				Path: path, Access: mutation.AccessShared, Effect: mutation.PathEffectReferent,
			},
		)
	}
	requests = append(requests, mutation.LogicalPathRequest{
		Path: markerPath, Access: mutation.AccessExclusive, Effect: mutation.PathEffectDirectoryEntry,
	})
	for _, path := range localPaths {
		requests = append(requests, mutation.LogicalPathRequest{
			Path: path, Access: mutation.AccessShared, Effect: mutation.PathEffectReferent,
		})
	}
	domains := make([]mutation.Domain, 0, len(requests))
	for _, request := range requests {
		domain, err := mutation.NewLogicalPathDomain(request)
		if err != nil {
			return nil, err
		}
		domains = append(domains, domain)
	}
	return domains, nil
}

func recoverMetadataFileSetBeforeRead(
	ctx context.Context,
	paths daempaths.Paths,
	targetPaths []string,
	barrier recoverygate.EffectAuthority,
) (returnErr error) {
	observationErr := barrier.Validate(ctx)
	if observationErr == nil {
		return nil
	}
	state := recoverygate.StateOf(observationErr)
	if state.Journal() != journal.InterruptionClear ||
		state.FileSet() != fileset.FileSetFencePublishedTransaction {
		return observationErr
	}
	markerPath, err := fileset.FileSetAuthorityPath(paths.StateDir)
	if err != nil {
		return err
	}
	domains, err := metadataMutationDomains(targetPaths, markerPath, nil)
	if err != nil {
		return err
	}
	domains = append(domains, barrier.Domains()...)
	store, err := mutation.NewStore(paths.DataDir)
	if err != nil {
		return err
	}
	leases, err := store.Acquire(ctx, domains...)
	if err != nil {
		return err
	}
	defer func() {
		if err := leases.Release(); err != nil {
			returnErr = errors.Join(returnErr, err)
		}
	}()
	if matches, err := leases.DomainsMatchCurrent(ctx); err != nil {
		return err
	} else if !matches {
		return mutation.StaleSnapshotError{}
	}
	if err := barrier.ValidateFileSetRecovery(ctx); err != nil {
		return err
	}
	if err := fileset.RecoverFileSet(ctx, paths.StateDir, targetPaths); err != nil {
		return err
	}
	return barrier.Validate(ctx)
}

func authoringRevisionRequests(
	manifestPath string,
	lockfilePath string,
	markerPath string,
	localPaths []string,
) ([]mutation.RevisionRequest, error) {
	requests, err := mutation.BoundedFileRevisionRequests(
		declarationartifact.MaximumBytes,
		manifestPath,
		lockfilePath,
	)
	if err != nil {
		return nil, err
	}
	requests = append(
		requests,
		mutation.NewBoundedContentRevisionRequest(markerPath, mutation.PathEffectDirectoryEntry),
	)
	for _, path := range localPaths {
		requests = append(
			requests,
			mutation.NewBoundedContentRevisionRequest(path, mutation.PathEffectReferent),
		)
	}
	return requests, nil
}

func equalAuthoringPathLists(left []string, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
