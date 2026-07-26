package authoring

import (
	"context"
	"fmt"

	"github.com/isty2e/daem/internal/assurance/durable"
	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	"github.com/isty2e/daem/internal/assurance/statefile"
	declarationmanifest "github.com/isty2e/daem/internal/declaration/manifest"
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/effect/storage/carrierclaim"
	"github.com/isty2e/daem/internal/target"
)

type unmanageCandidate struct {
	request         UnmanageExtensionRequest
	document        ManifestDocument
	manifestContent []byte
	manifestChanged bool
	lockfile        LockfileChange
	localPaths      []string
	nextState       durable.Snapshot
	stateChanged    bool
	nextRegistry    durablecarrier.GlobalCarrierClaims
	registryChanged bool
	selected        selection
}

func buildUnmanageCandidate(
	ctx context.Context,
	request UnmanageExtensionRequest,
	buildLockfile bool,
) (unmanageCandidate, error) {
	if ctx == nil {
		return unmanageCandidate{}, fmt.Errorf("unmanage context is required")
	}
	if err := ctx.Err(); err != nil {
		return unmanageCandidate{}, err
	}
	request, err := request.validate()
	if err != nil {
		return unmanageCandidate{}, err
	}
	document, err := LoadManifestDocument(request.ManifestPath)
	if err != nil {
		return unmanageCandidate{}, err
	}
	environment, err := declarationmanifest.Decode(document.Content)
	if err != nil {
		return unmanageCandidate{}, fmt.Errorf("invalid manifest: %w", err)
	}
	statefileKey, err := mutation.CanonicalDirectoryEntryKey(document.Paths.StatefilePath)
	if err != nil {
		return unmanageCandidate{}, fmt.Errorf("canonicalize state authority: %w", err)
	}
	owner, err := durablecarrier.NewStateAuthority(statefileKey, document.Path)
	if err != nil {
		return unmanageCandidate{}, err
	}
	state, err := statefile.LoadOptional(ctx, document.Paths.StatefilePath)
	if err != nil {
		return unmanageCandidate{}, err
	}
	registryStore, err := carrierclaim.New(document.Paths.CarrierClaimRegistryPath)
	if err != nil {
		return unmanageCandidate{}, err
	}
	registry, err := registryStore.Load(ctx)
	if err != nil {
		return unmanageCandidate{}, err
	}
	selected, err := selectExtensionManagement(environment, state, registry, owner, request)
	if err != nil {
		return unmanageCandidate{}, err
	}

	manifestContent := append([]byte(nil), document.Content...)
	manifestChanged := selected.declaration != nil
	if manifestChanged {
		change, err := BuildRemoveExtensionChange(
			document,
			RemoveExtensionRequest{
				ID:      request.ID,
				Targets: optionalTarget(request.Target),
				Scope:   string(request.Scope),
			},
		)
		if err != nil {
			return unmanageCandidate{}, err
		}
		manifestContent = change.Content
	}

	nextState := state
	stateChanged := false
	nextRegistry := registry
	registryChanged := false
	if selected.hasIdentity {
		nextState, stateChanged, err = state.WithoutCarrierManagement(owner, selected.identity)
		if err != nil {
			return unmanageCandidate{}, err
		}
		for _, claim := range registry.Claims() {
			if !claim.Owner().ExactEqual(owner) ||
				!claim.Identity().ExactEqual(selected.identity) {
				continue
			}
			nextRegistry, registryChanged, err = nextRegistry.WithoutClaim(claim)
			if err != nil {
				return unmanageCandidate{}, err
			}
		}
	}

	lockInput := LockfileChangeInput{
		ManifestPath:       document.Path,
		Paths:              document.Paths,
		LockfilePath:       request.LockfilePath,
		ManifestBytes:      manifestContent,
		UsePersistentCache: request.Mode == UnmanageModeWrite,
	}
	localPaths, err := ConsumedLocalPaths(lockInput)
	if err != nil {
		return unmanageCandidate{}, err
	}
	lockfile := LockfileChange{
		path: lockInput.LockfilePath,
	}
	if lockfile.path == "" {
		lockfile.path = document.Paths.LockfilePath
	}
	if buildLockfile {
		lockfile, err = BuildLockfileChange(ctx, lockInput)
		if err != nil {
			return unmanageCandidate{}, err
		}
	}
	return unmanageCandidate{
		request:         request,
		document:        document,
		manifestContent: manifestContent,
		manifestChanged: manifestChanged,
		lockfile:        lockfile,
		localPaths:      localPaths,
		nextState:       nextState,
		stateChanged:    stateChanged,
		nextRegistry:    nextRegistry,
		registryChanged: registryChanged,
		selected:        selected,
	}, nil
}

func optionalTarget(value target.Target) []string {
	if value == "" {
		return nil
	}
	return []string{string(value)}
}
