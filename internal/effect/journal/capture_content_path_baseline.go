package journal

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"

	"github.com/isty2e/daem/internal/effect/mutation"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/target"
)

type recoveryContentPathBaselineKey struct {
	scope       target.Scope
	destination output.Destination
}

type recoveryContentPathBaselineRequest struct {
	contentPaths       []output.ContentPath
	aggregateContracts map[output.ContentPath]aggregate.ProjectionContract
}

type recoveryContentPathBaselineSelectionKey struct {
	recoveryContentPathBaselineKey
	contentPath output.ContentPath
}

type recoveryContentPathProjectionBaseline struct {
	content       []byte
	present       bool
	parentPresent bool
}

// recoveryContentPathBaseline is an operation-local immutable projection view.
// Its project-only path hash is transient TOCTOU evidence and is never persisted.
type recoveryContentPathBaseline struct {
	mode            fs.FileMode
	pathContentHash artifact.ContentHash
	projections     map[output.ContentPath]recoveryContentPathProjectionBaseline
}

func (baseline recoveryContentPathBaseline) projection(
	contentPath output.ContentPath,
) ([]byte, bool, error) {
	projection, ok := baseline.projections[contentPath]
	if !ok {
		return nil, false, fmt.Errorf("recovery baseline did not select content path %q", contentPath)
	}
	return bytes.Clone(projection.content), projection.present, nil
}

func (baseline recoveryContentPathBaseline) parentExisted(contentPath output.ContentPath) (bool, error) {
	projection, ok := baseline.projections[contentPath]
	if !ok {
		return false, fmt.Errorf("recovery baseline did not select content path %q", contentPath)
	}
	return projection.parentPresent, nil
}

type recoveryContentPathBaselineCache struct {
	requests      map[recoveryContentPathBaselineKey]recoveryContentPathBaselineRequest
	byDestination map[recoveryContentPathBaselineKey]recoveryContentPathBaseline
	codecs        aggregate.CodecCatalog
	filesystem    mutationfs.Reader
}

func newRecoveryContentPathBaselineCache(
	actions []pathMutation,
	codecs aggregate.CodecCatalog,
	filesystem mutationfs.Reader,
) (*recoveryContentPathBaselineCache, error) {
	if filesystem == nil {
		return nil, fmt.Errorf("recovery content-path filesystem is required")
	}
	requests := make(map[recoveryContentPathBaselineKey]recoveryContentPathBaselineRequest)
	selected := make(map[recoveryContentPathBaselineSelectionKey]struct{})
	for _, action := range actions {
		if action.ContentPath == "" {
			continue
		}
		key := recoveryContentPathBaselineKey{scope: action.Scope, destination: action.Destination}
		selection := recoveryContentPathBaselineSelectionKey{
			recoveryContentPathBaselineKey: key,
			contentPath:                    action.ContentPath,
		}
		if _, duplicate := selected[selection]; duplicate {
			return nil, fmt.Errorf(
				"recovery content-path baseline repeats destination %q content path %q",
				action.Destination,
				action.ContentPath,
			)
		}
		selected[selection] = struct{}{}
		if action.AggregateContract == nil {
			return nil, fmt.Errorf(
				"recovery content-path baseline requires aggregate contract for destination %q content path %q",
				action.Destination,
				action.ContentPath,
			)
		}
		request := requests[key]
		request.contentPaths = append(request.contentPaths, action.ContentPath)
		if request.aggregateContracts == nil {
			request.aggregateContracts = make(map[output.ContentPath]aggregate.ProjectionContract)
		}
		request.aggregateContracts[action.ContentPath] = action.AggregateContract.Clone()
		requests[key] = request
	}
	return &recoveryContentPathBaselineCache{
		requests:      requests,
		byDestination: make(map[recoveryContentPathBaselineKey]recoveryContentPathBaseline),
		codecs:        codecs,
		filesystem:    filesystem,
	}, nil
}

func (cache *recoveryContentPathBaselineCache) capture(
	ctx context.Context,
	action pathMutation,
	resolver func(destination output.Destination) (string, error),
	projectAuthority *projectAuthoritySession,
	rootedCapability RootedCapabilityResolver,
) (recoveryContentPathBaseline, error) {
	if cache == nil {
		return recoveryContentPathBaseline{}, fmt.Errorf("recovery content-path baseline cache is required")
	}
	key := recoveryContentPathBaselineKey{scope: action.Scope, destination: action.Destination}
	if baseline, ok := cache.byDestination[key]; ok {
		return baseline, nil
	}
	request, ok := cache.requests[key]
	if !ok {
		return recoveryContentPathBaseline{}, fmt.Errorf(
			"recovery content-path baseline request is missing for destination %q",
			action.Destination,
		)
	}

	var (
		content []byte
		mode    fs.FileMode
		err     error
	)
	switch action.Scope {
	case target.ScopeProject:
		content, mode, err = readProjectRecoveryRegularFile(
			ctx,
			cache.filesystem,
			action.Destination,
			projectAuthority,
		)
	case target.ScopeGlobal:
		if resolver == nil {
			return recoveryContentPathBaseline{}, fmt.Errorf("recovery destination resolver is required")
		}
		var hostPath string
		hostPath, err = resolver(action.Destination)
		if err != nil {
			return recoveryContentPathBaseline{}, fmt.Errorf("resolve destination %q: %w", action.Destination, err)
		}
		if rootedCapability != nil {
			var capability rootedpath.CommitCapability
			var present bool
			capability, present, err = acquireMatchingRootedCapability(
				action.Destination,
				hostPath,
				rootedCapability,
			)
			if err == nil && !present {
				err = fmt.Errorf("destination %q has no retained root authority", action.Destination)
			}
			if err == nil {
				content, mode, _, err = cache.filesystem.ReadRootedRegularFileUpTo(
					ctx,
					capability,
					MaximumRecoveryBackupFileBytes,
				)
				err = errors.Join(err, capability.Close())
			}
		} else {
			var commitPath string
			commitPath, err = mutation.CanonicalDirectoryEntryPath(hostPath)
			if err == nil {
				var snapshot mutationfs.RegularFileSnapshot
				snapshot, err = cache.filesystem.ReadRegularFileSnapshotUpTo(
					ctx,
					commitPath,
					MaximumRecoveryBackupFileBytes,
				)
				if err == nil {
					content = snapshot.Content()
					mode = snapshot.Mode()
				}
			}
		}
		if err != nil {
			err = fmt.Errorf("read destination %q for content-path recovery: %w", action.Destination, err)
		}
	default:
		return recoveryContentPathBaseline{}, fmt.Errorf(
			"recovery content-path destination %q has unsupported scope %q",
			action.Destination,
			action.Scope,
		)
	}
	if err != nil {
		return recoveryContentPathBaseline{}, err
	}
	projections, err := deriveRecoveryContentPathProjections(
		content,
		action.Destination,
		request.contentPaths,
		request.aggregateContracts,
		cache.codecs,
	)
	if err != nil {
		return recoveryContentPathBaseline{}, err
	}
	baseline := recoveryContentPathBaseline{
		mode:        mode,
		projections: projections,
	}
	if action.Scope == target.ScopeProject {
		baseline.pathContentHash = artifact.HashFileContentWithExecutable(
			content,
			mode.Perm()&0o111 != 0,
		)
	}
	cache.byDestination[key] = baseline
	return baseline, nil
}

func deriveRecoveryContentPathProjections(
	content []byte,
	destination output.Destination,
	contentPaths []output.ContentPath,
	aggregateContracts map[output.ContentPath]aggregate.ProjectionContract,
	codecs aggregate.CodecCatalog,
) (map[output.ContentPath]recoveryContentPathProjectionBaseline, error) {
	result := make(map[output.ContentPath]recoveryContentPathProjectionBaseline, len(contentPaths))
	if len(contentPaths) == 0 {
		return result, nil
	}
	contracts := make([]aggregate.ProjectionContract, 0, len(contentPaths))
	for _, contentPath := range contentPaths {
		contract, ok := aggregateContracts[contentPath]
		if !ok {
			return nil, fmt.Errorf(
				"recovery content path %q has no aggregate contract",
				contentPath,
			)
		}
		address := contract.Address()
		if address.Document().AggregateRoot() != string(destination) ||
			address.ContentPath() != aggregate.ContentPath(contentPath) {
			return nil, fmt.Errorf(
				"recovery aggregate contract address %q%q does not match baseline %q%q",
				address.Document().AggregateRoot(),
				address.ContentPath(),
				destination,
				contentPath,
			)
		}
		contracts = append(contracts, contract)
	}
	selection, err := aggregate.NewSelection(contracts)
	if err != nil {
		return nil, err
	}
	codec, ok := codecs.Lookup(selection.CodecContractID())
	if !ok {
		return nil, fmt.Errorf(
			"unsupported recovery aggregate codec %q",
			selection.CodecContractID(),
		)
	}
	snapshot, failure := codec.Read(aggregate.ExistingDocument(content), selection)
	if failure != nil {
		return nil, failure
	}
	states := snapshot.States()
	if len(states) != len(contentPaths) {
		return nil, fmt.Errorf(
			"recovery aggregate codec returned %d states for %d content paths",
			len(states),
			len(contentPaths),
		)
	}
	for _, state := range states {
		contentPath := output.ContentPath(state.Contract().Address().ContentPath())
		if _, selected := aggregateContracts[contentPath]; !selected {
			return nil, fmt.Errorf(
				"recovery aggregate codec returned unselected content path %q",
				contentPath,
			)
		}
		result[contentPath] = recoveryContentPathProjectionBaseline{
			content:       []byte(state.CanonicalProjection()),
			present:       state.Present(),
			parentPresent: state.ParentPresent(),
		}
	}
	return result, nil
}
