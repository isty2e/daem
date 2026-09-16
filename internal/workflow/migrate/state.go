// Package migrate coordinates explicit user-state authority transfer without
// changing declarations, installed content, or host-owned package state.
package migrate

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/durable/carrier"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	"github.com/isty2e/daem/internal/assurance/statefile"
	"github.com/isty2e/daem/internal/effect/fileset"
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/effect/storage/carrierclaim"
	"github.com/isty2e/daem/internal/output/ownership"
	ownershipstore "github.com/isty2e/daem/internal/output/ownership/store"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/recoverygate"
)

type StateInput struct {
	ManifestPath string
	Recover      bool
}

// StateDisclosure describes metadata effects only. Installed outputs are not
// read as new baselines or rewritten by migration.
type StateDisclosure struct {
	ManifestPath         string
	SourceStatefile      string
	DestinationStatefile string
	OutputRegistryPath   string
	CarrierRegistryPath  string
	Action               string
	OutputClaims         int
	CarrierClaims        int
	ManagedPaths         int
	ManagedAggregates    int
}

// PreparedState owns the observations authorized by one disclosure. It is
// single-use, including failed execution, so retry requires a fresh preview.
type PreparedState struct {
	mu                 sync.Mutex
	closed             bool
	input              StateInput
	paths              daempaths.Paths
	legacy             daempaths.Paths
	sourceBarrier      recoverygate.EffectAuthority
	targetBarrier      recoverygate.EffectAuthority
	revisions          mutation.RevisionSet
	from               stateauthority.Authority
	targetStatefileKey string
	snapshot           durable.Snapshot
	outputs            ownership.Registry
	carriers           carrier.GlobalCarrierClaims
	disclosure         StateDisclosure
}

func (prepared *PreparedState) Disclosure() StateDisclosure { return prepared.disclosure }

func (prepared *PreparedState) Close() {
	prepared.mu.Lock()
	defer prepared.mu.Unlock()
	prepared.closed = true
}

func PlanState(ctx context.Context, input StateInput) (*PreparedState, error) {
	if ctx == nil {
		return nil, fmt.Errorf("state migration context is required")
	}
	paths, err := daempaths.Resolve(input.ManifestPath)
	if err != nil {
		return nil, err
	}
	legacy, err := paths.LegacyUserState()
	if err != nil {
		return nil, err
	}
	// This operation inspects both authorities explicitly, rather than entering
	// the normal-operation guard which requires migration to have finished.
	paths.LegacyUserStateDir = ""
	prepared := &PreparedState{input: input, paths: paths, legacy: legacy}
	prepared.targetStatefileKey, err = mutation.CanonicalDirectoryEntryKey(paths.StatefilePath)
	if err != nil {
		return nil, err
	}
	legacyKey, err := mutation.CanonicalDirectoryEntryKey(legacy.StatefilePath)
	if err != nil {
		return nil, err
	}
	if legacyKey == prepared.targetStatefileKey {
		return nil, fmt.Errorf("legacy and selected state already use the same authority; no migration is needed")
	}
	prepared.disclosure = StateDisclosure{
		ManifestPath: paths.ManifestPath, SourceStatefile: legacy.StatefilePath,
		DestinationStatefile: paths.StatefilePath, Action: "migrate",
		OutputRegistryPath: paths.OwnershipRegistryPath, CarrierRegistryPath: paths.CarrierClaimRegistryPath,
	}
	prepared.sourceBarrier, err = recoverygate.NewEffectAuthority(ctx, legacy)
	if err != nil {
		return nil, err
	}
	prepared.targetBarrier, err = recoverygate.NewEffectAuthority(ctx, paths)
	if err != nil {
		return nil, err
	}
	requests, err := prepared.revisionRequests()
	if err != nil {
		return nil, err
	}
	prepared.revisions, err = mutation.CaptureRevisionSet(ctx, requests...)
	if err != nil {
		return nil, err
	}
	if err := prepared.sourceBarrier.Validate(ctx); err != nil {
		return nil, recoverygate.LegacyUserStateRecoveryError{StateDir: legacy.StateDir, Err: err}
	}
	if input.Recover {
		if err := prepared.targetBarrier.ValidateFileSetRecovery(ctx); err != nil {
			return nil, err
		}
		action, err := fileset.InspectFileSetRecovery(ctx, paths.StateDir, prepared.targetPaths())
		if err != nil {
			return nil, err
		}
		prepared.disclosure.Action = string(action)
	} else {
		if err := prepared.targetBarrier.Validate(ctx); err != nil {
			return nil, fmt.Errorf("inspect migration destination (use daem migrate state --recover --dry-run for an interrupted migration): %w", err)
		}
		if err := prepared.loadTransfer(ctx); err != nil {
			return nil, err
		}
	}
	if err := prepared.validateRevisions(ctx); err != nil {
		return nil, err
	}
	return prepared, nil
}

func authorityFor(paths daempaths.Paths) (stateauthority.Authority, error) {
	observed, err := mutation.ObservePersistedDirectoryEntryAuthority(paths.StatefilePath)
	if err != nil {
		return stateauthority.Authority{}, err
	}
	return stateauthority.New(observed.Exact(), paths.ManifestPath)
}

func (prepared *PreparedState) loadTransfer(ctx context.Context) error {
	from, err := authorityFor(prepared.legacy)
	if err != nil {
		return err
	}
	prepared.from = from
	receipt, relocated, err := statefile.LoadRelocation(ctx, prepared.legacy.StatefilePath)
	sourceMissing := errors.Is(err, os.ErrNotExist)
	if err != nil && !sourceMissing {
		return fmt.Errorf("read legacy state: %w", err)
	}
	if relocated {
		to, err := authorityFor(prepared.paths)
		if err != nil {
			return err
		}
		if !receipt.From().Equal(from) || !receipt.To().Equal(to) {
			return fmt.Errorf("state relocation receipt does not match selected authorities")
		}
		snapshot, err := statefile.Load(ctx, prepared.paths.StatefilePath)
		if err != nil {
			return err
		}
		prepared.disclosure.ManagedPaths = len(snapshot.ManagedPaths())
		prepared.disclosure.ManagedAggregates = len(snapshot.ManagedAggregates())
		prepared.disclosure.Action = "already_migrated"
		return prepared.loadRegistries(ctx, to, true)
	}
	if _, err := os.Lstat(prepared.paths.StatefilePath); err == nil {
		return fmt.Errorf("destination state %q already exists; migration does not merge states", prepared.paths.StatefilePath)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	prepared.snapshot, err = statefile.LoadOptional(ctx, prepared.legacy.StatefilePath)
	if err != nil {
		return err
	}
	if err := prepared.validateSnapshotProvenance(); err != nil {
		return err
	}
	prepared.disclosure.ManagedPaths = len(prepared.snapshot.ManagedPaths())
	prepared.disclosure.ManagedAggregates = len(prepared.snapshot.ManagedAggregates())
	if err := prepared.loadRegistries(ctx, from, false); err != nil {
		return err
	}
	if sourceMissing {
		if prepared.disclosure.OutputClaims != 0 {
			return fmt.Errorf("legacy output ownership exists without its state snapshot; restore the missing snapshot before migration")
		}
		if prepared.disclosure.CarrierClaims == 0 {
			return fmt.Errorf("no legacy state or ownership requires migration")
		}
	}
	return prepared.validateMetadataSeparation()
}

func (prepared *PreparedState) loadRegistries(ctx context.Context, expected stateauthority.Authority, relocated bool) error {
	outputStore, err := ownershipstore.New(prepared.paths.OwnershipRegistryPath)
	if err != nil {
		return err
	}
	prepared.outputs, err = outputStore.Load(ctx)
	if err != nil {
		return err
	}
	carrierStore, err := carrierclaim.New(prepared.paths.CarrierClaimRegistryPath)
	if err != nil {
		return err
	}
	prepared.carriers, err = carrierStore.Load(ctx)
	if err != nil {
		return err
	}
	for _, claim := range prepared.outputs.Claims() {
		if err := prepared.validateRegistryOwner(claim.Owner(), expected, relocated); err != nil {
			return err
		}
		if claim.OwnedBy(expected) {
			if claim.State() != ownership.ClaimActive {
				return fmt.Errorf("reserved ownership requires recovery before state migration")
			}
			prepared.disclosure.OutputClaims++
		}
	}
	for _, claim := range prepared.carriers.Claims() {
		if err := prepared.validateRegistryOwner(claim.Owner(), expected, relocated); err != nil {
			return err
		}
		if claim.Owner().Equal(expected) {
			prepared.disclosure.CarrierClaims++
		}
	}
	return nil
}

func (prepared *PreparedState) validateRegistryOwner(owner, expected stateauthority.Authority, relocated bool) error {
	if !relocated && owner.StatefileKey() == prepared.targetStatefileKey {
		return fmt.Errorf("destination authority already owns a registry claim; migration does not merge authorities")
	}
	if relocated && owner.Equal(prepared.from) {
		return fmt.Errorf("relocation receipt conflicts with a retained legacy ownership claim")
	}
	matches, err := sameManifest(owner.ManifestPath(), prepared.paths.ManifestPath)
	if err != nil {
		return err
	}
	if owner.Equal(expected) && !matches {
		return fmt.Errorf("state authority is shared with another manifest %q; migration cannot split its state", owner.ManifestPath())
	}
	if matches && !owner.Equal(expected) {
		return fmt.Errorf("selected manifest has a conflicting state authority %q", owner.StatefileKey())
	}
	return nil
}

func sameManifest(left, right string) (bool, error) {
	if left == right {
		return true, nil
	}
	leftKey, err := mutation.CanonicalDirectoryEntryKey(left)
	if err != nil {
		return false, err
	}
	rightKey, err := mutation.CanonicalDirectoryEntryKey(right)
	return leftKey == rightKey, err
}

func (prepared *PreparedState) validateSnapshotProvenance() error {
	owners := make([]stateauthority.Authority, 0)
	for _, claim := range prepared.snapshot.ManagedCarrierClaims() {
		owners = append(owners, claim.Owner())
	}
	for _, pending := range prepared.snapshot.PendingCarrierInstalls() {
		owners = append(owners, pending.Owner())
	}
	for _, pending := range prepared.snapshot.PendingCarrierRemovals() {
		owners = append(owners, pending.Owner())
	}
	for _, owner := range owners {
		if err := prepared.validateRegistryOwner(owner, prepared.from, false); err != nil {
			return err
		}
	}
	return nil
}

func (prepared *PreparedState) targetPaths() []string {
	return []string{
		prepared.legacy.StatefilePath, prepared.paths.StatefilePath,
		prepared.paths.OwnershipRegistryPath, prepared.paths.CarrierClaimRegistryPath,
	}
}

func (prepared *PreparedState) revisionRequests() ([]mutation.RevisionRequest, error) {
	requests, err := mutation.BoundedFileRevisionRequests(64<<20, prepared.targetPaths()...)
	if err != nil {
		return nil, err
	}
	marker, err := fileset.FileSetAuthorityPath(prepared.paths.StateDir)
	if err != nil {
		return nil, err
	}
	requests = append(requests, mutation.NewBoundedContentRevisionRequest(marker, mutation.PathEffectDirectoryEntry))
	requests = append(requests, prepared.sourceBarrier.RevisionRequests()...)
	requests = append(requests, prepared.targetBarrier.RevisionRequests()...)
	return requests, nil
}

func (prepared *PreparedState) validateRevisions(ctx context.Context) error {
	matches, err := prepared.revisions.MatchesCurrent(ctx)
	if err != nil {
		return err
	}
	if !matches {
		return mutation.StaleSnapshotError{}
	}
	return nil
}
