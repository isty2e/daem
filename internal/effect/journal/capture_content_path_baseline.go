package journal

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"

	journalnamespace "github.com/isty2e/daem/internal/effect/journal/namespace"

	journalrecord "github.com/isty2e/daem/internal/effect/journal/record"

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
	selection aggregate.Selection
	codec     aggregate.Codec
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
	filesystem    mutationfs.Reader
}

func newRecoveryContentPathBaselineCache(
	actions []journalrecord.CaptureMutation,
	codecs aggregate.CodecCatalog,
	filesystem mutationfs.Reader,
) (*recoveryContentPathBaselineCache, error) {
	if filesystem == nil {
		return nil, fmt.Errorf("recovery content-path filesystem is required")
	}
	if err := journalrecord.ValidateCaptureMutations(actions); err != nil {
		return nil, err
	}
	contractsByDestination := make(map[recoveryContentPathBaselineKey][]aggregate.ProjectionContract)
	selected := make(map[recoveryContentPathBaselineSelectionKey]struct{})
	for _, action := range actions {
		if action.ContentPath() == "" {
			continue
		}
		key := recoveryContentPathBaselineKey{scope: action.Scope(), destination: action.Destination()}
		selection := recoveryContentPathBaselineSelectionKey{
			recoveryContentPathBaselineKey: key,
			contentPath:                    action.ContentPath(),
		}
		if _, duplicate := selected[selection]; duplicate {
			return nil, fmt.Errorf(
				"recovery content-path baseline repeats destination %q content path %q",
				action.Destination(),
				action.ContentPath(),
			)
		}
		selected[selection] = struct{}{}
		contract, present := action.AggregateContract()
		if !present {
			return nil, fmt.Errorf(
				"recovery content-path baseline requires aggregate contract for destination %q content path %q",
				action.Destination(),
				action.ContentPath(),
			)
		}
		address := contract.Address()
		contentPath, err := address.LegacyContentPath()
		if err != nil {
			return nil, err
		}
		document := address.Document()
		if document.Scope() != action.Scope() ||
			document.AggregateRoot() != action.Destination() ||
			contentPath != aggregate.ContentPath(action.ContentPath()) {
			return nil, fmt.Errorf(
				"recovery aggregate contract address %q%q does not match baseline %q%q",
				document.AggregateRoot(),
				contentPath,
				action.Destination(),
				action.ContentPath(),
			)
		}
		contractsByDestination[key] = append(contractsByDestination[key], contract)
	}
	requests := make(map[recoveryContentPathBaselineKey]recoveryContentPathBaselineRequest, len(contractsByDestination))
	for key, contracts := range contractsByDestination {
		selection, err := aggregate.NewSelection(contracts)
		if err != nil {
			return nil, fmt.Errorf("recovery content-path baseline selection: %w", err)
		}
		codec, ok := codecs.Lookup(selection.CodecContractID())
		if !ok {
			return nil, fmt.Errorf(
				"unsupported recovery aggregate codec %q",
				selection.CodecContractID(),
			)
		}
		requests[key] = recoveryContentPathBaselineRequest{selection: selection, codec: codec}
	}
	return &recoveryContentPathBaselineCache{
		requests:      requests,
		byDestination: make(map[recoveryContentPathBaselineKey]recoveryContentPathBaseline),
		filesystem:    filesystem,
	}, nil
}

func (cache *recoveryContentPathBaselineCache) capture(
	ctx context.Context,
	action journalrecord.CaptureMutation,
	resolver func(destination output.Destination) (string, error),
	manifestAuthority *manifestAuthoritySession,
	rootedCapability RootedCapabilityResolver,
) (recoveryContentPathBaseline, error) {
	if cache == nil {
		return recoveryContentPathBaseline{}, fmt.Errorf("recovery content-path baseline cache is required")
	}
	if err := action.Validate(); err != nil {
		return recoveryContentPathBaseline{}, err
	}
	key := recoveryContentPathBaselineKey{scope: action.Scope(), destination: action.Destination()}
	if baseline, ok := cache.byDestination[key]; ok {
		return baseline, nil
	}
	request, ok := cache.requests[key]
	if !ok {
		return recoveryContentPathBaseline{}, fmt.Errorf(
			"recovery content-path baseline request is missing for destination %q",
			action.Destination(),
		)
	}

	var (
		content []byte
		mode    fs.FileMode
		err     error
	)
	switch action.Scope() {
	case target.ScopeProject:
		content, mode, err = readProjectRecoveryRegularFile(
			ctx,
			cache.filesystem,
			action.Destination(),
			manifestAuthority,
			request.codec.MaximumDocumentBytes(),
		)
	case target.ScopeGlobal:
		if resolver == nil {
			return recoveryContentPathBaseline{}, fmt.Errorf("recovery destination resolver is required")
		}
		var hostPath string
		hostPath, err = resolver(action.Destination())
		if err != nil {
			return recoveryContentPathBaseline{}, fmt.Errorf("resolve destination %q: %w", action.Destination(), err)
		}
		if rootedCapability != nil {
			var capability rootedpath.CommitCapability
			var present bool
			capability, present, err = journalnamespace.AcquireMatchingCapability(
				action.Destination(),
				hostPath,
				rootedCapability,
				nil,
			)
			if err == nil && !present {
				err = fmt.Errorf("destination %q has no retained root authority", action.Destination())
			}
			if err == nil {
				content, mode, _, err = cache.filesystem.ReadRootedRegularFileUpTo(
					ctx,
					capability,
					request.codec.MaximumDocumentBytes(),
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
					request.codec.MaximumDocumentBytes(),
				)
				if err == nil {
					content = snapshot.Content()
					mode = snapshot.Mode()
				}
			}
		}
		if err != nil {
			err = fmt.Errorf("read destination %q for content-path recovery: %w", action.Destination(), err)
		}
	default:
		return recoveryContentPathBaseline{}, fmt.Errorf(
			"recovery content-path destination %q has unsupported scope %q",
			action.Destination(),
			action.Scope(),
		)
	}
	if err != nil {
		return recoveryContentPathBaseline{}, err
	}
	projections, err := deriveRecoveryContentPathProjections(
		content,
		action.Destination(),
		request,
	)
	if err != nil {
		return recoveryContentPathBaseline{}, err
	}
	baseline := recoveryContentPathBaseline{
		mode:        mode,
		projections: projections,
	}
	if action.Scope() == target.ScopeProject {
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
	request recoveryContentPathBaselineRequest,
) (map[output.ContentPath]recoveryContentPathProjectionBaseline, error) {
	contracts := request.selection.Contracts()
	result := make(map[output.ContentPath]recoveryContentPathProjectionBaseline, len(contracts))
	selected := make(map[output.ContentPath]struct{}, len(contracts))
	for _, contract := range contracts {
		address := contract.Address()
		contentPath, err := address.LegacyContentPath()
		if err != nil {
			return nil, err
		}
		if address.Document().AggregateRoot() != destination ||
			address.Document() != request.selection.DocumentAddress() {
			return nil, fmt.Errorf(
				"recovery aggregate contract address %q%q does not match baseline document %q",
				address.Document().AggregateRoot(),
				contentPath,
				request.selection.DocumentAddress().AggregateRoot(),
			)
		}
		selected[output.ContentPath(contentPath)] = struct{}{}
	}
	snapshot, failure := request.codec.Read(aggregate.ExistingDocument(content), request.selection)
	if failure != nil {
		return nil, failure
	}
	states := snapshot.States()
	if len(states) != len(contracts) {
		return nil, fmt.Errorf(
			"recovery aggregate codec returned %d states for %d content paths",
			len(states),
			len(contracts),
		)
	}
	for _, state := range states {
		legacyPath, err := state.Contract().Address().LegacyContentPath()
		if err != nil {
			return nil, err
		}
		contentPath := output.ContentPath(legacyPath)
		if _, ok := selected[contentPath]; !ok {
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
