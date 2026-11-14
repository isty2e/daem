// Package carrierclaim persists the shared daem-known global carrier claims.
package carrierclaim

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"

	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	"github.com/isty2e/daem/internal/assurance/pathauthority"
	"github.com/isty2e/daem/internal/effect/mutation"
	storagecommit "github.com/isty2e/daem/internal/effect/storage/commit"
)

const (
	currentVersion           = 2
	maximumRegistryBytes     = 16 << 20
	maximumRegistryJSONDepth = 64
)

// Store owns strict serialization and guarded atomic writes for one global
// carrier-claim registry path.
type Store struct {
	path       string
	commitFile func(context.Context, storagecommit.FileCommit) error
}

// New validates and canonicalizes one registry path.
func New(path string) (Store, error) {
	if path == "" || !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return Store{}, fmt.Errorf("carrier claim registry path %q must be absolute and clean", path)
	}
	canonical, err := mutation.CanonicalDirectoryEntryPath(path)
	if err != nil {
		return Store{}, fmt.Errorf("canonicalize carrier claim registry path: %w", err)
	}
	return Store{path: canonical, commitFile: storagecommit.CommitFile}, nil
}

// Load reads current or exact empty retired claim state. A missing file is empty.
func (store Store) Load(ctx context.Context) (durablecarrier.GlobalCarrierClaims, error) {
	document, _, _, err := store.loadForCommit(ctx)
	if err != nil {
		return durablecarrier.GlobalCarrierClaims{}, err
	}
	return document.registry, nil
}

func validateCurrentAuthorities(
	ctx context.Context,
	registry durablecarrier.GlobalCarrierClaims,
) error {
	return (registryDocument{registry: registry}).validateAuthorities(ctx)
}

func (document registryDocument) validateAuthorities(ctx context.Context) error {
	for index, claim := range document.registry.Claims() {
		if err := ctx.Err(); err != nil {
			return err
		}
		locator := document.locator(claim.Owner().StatefileAuthority())
		current, err := mutation.ObservePersistedDirectoryEntryAuthority(locator)
		if err != nil {
			return fmt.Errorf("observe carrier claim registry claims[%d] statefile authority: %w", index, err)
		}
		if !current.Exact().Equal(claim.Owner().StatefileAuthority()) {
			return fmt.Errorf(
				"carrier claim registry claims[%d] statefile authority %q with semantics %q is not current via locator %q; observed %q with semantics %q",
				index,
				claim.Owner().StatefileKey(),
				claim.Owner().StatefileAuthority().Witness(),
				locator,
				current.Exact().Key(),
				current.Exact().Witness(),
			)
		}
	}
	return nil
}

// LoadForSelectedAuthority loads the full registry, validates claims for the
// selected manifest against exact versioned path authority.
func (store Store) LoadForSelectedAuthority(
	ctx context.Context,
	statefilePath string,
	manifestPath string,
) (durablecarrier.GlobalCarrierClaims, error) {
	registry, err := store.Load(ctx)
	if err != nil {
		return durablecarrier.GlobalCarrierClaims{}, err
	}
	authority, err := mutation.ObservePersistedDirectoryEntryAuthority(statefilePath)
	if err != nil {
		return durablecarrier.GlobalCarrierClaims{}, fmt.Errorf(
			"canonicalize selected carrier state authority: %w",
			err,
		)
	}
	if err := validateSelectedAuthority(registry, authority.Exact(), manifestPath); err != nil {
		return durablecarrier.GlobalCarrierClaims{}, err
	}
	return registry, nil
}

func validateSelectedAuthority(
	registry durablecarrier.GlobalCarrierClaims,
	authority pathauthority.Exact,
	manifestPath string,
) error {
	for index, claim := range registry.Claims() {
		if claim.Owner().ManifestPath() != manifestPath {
			continue
		}
		if !claim.Owner().StatefileAuthority().Equal(authority) {
			return fmt.Errorf(
				"carrier claim registry claim[%d] for selected manifest has state authority %q with semantics %q, want %q with semantics %q",
				index,
				claim.Owner().StatefileKey(),
				claim.Owner().StatefileAuthority().Witness(),
				authority.Key(),
				authority.Witness(),
			)
		}
	}
	return nil
}

// Upsert writes one exact claim through entry-identity compare-and-swap.
func (store Store) Upsert(
	ctx context.Context,
	claim durablecarrier.ManagedCarrierClaim,
	references ...pathauthority.Reference,
) (durablecarrier.GlobalCarrierClaims, error) {
	return store.UpsertAll(ctx, []durablecarrier.ManagedCarrierClaim{claim}, references...)
}

// UpsertAll writes one exact claim batch through one entry-identity
// compare-and-swap. Either the whole canonical batch commits or none does.
// Located images require explicit lookup evidence for new owners and retain the
// recorded locators of surviving owners.
func (store Store) UpsertAll(
	ctx context.Context,
	claims []durablecarrier.ManagedCarrierClaim,
	references ...pathauthority.Reference,
) (durablecarrier.GlobalCarrierClaims, error) {
	return store.upsertAll(ctx, nil, claims, references)
}

// UpsertAllIfCurrent commits one exact batch only when the durable registry
// still equals the caller's confirmed baseline.
func (store Store) UpsertAllIfCurrent(
	ctx context.Context,
	expected durablecarrier.GlobalCarrierClaims,
	claims []durablecarrier.ManagedCarrierClaim,
	references ...pathauthority.Reference,
) (durablecarrier.GlobalCarrierClaims, error) {
	return store.upsertAll(ctx, &expected, claims, references)
}

func (store Store) upsertAll(
	ctx context.Context,
	expected *durablecarrier.GlobalCarrierClaims,
	claims []durablecarrier.ManagedCarrierClaim,
	references []pathauthority.Reference,
) (durablecarrier.GlobalCarrierClaims, error) {
	if ctx == nil {
		return durablecarrier.GlobalCarrierClaims{}, fmt.Errorf("carrier claim registry context is required")
	}
	if err := ctx.Err(); err != nil {
		return durablecarrier.GlobalCarrierClaims{}, err
	}
	current, identity, exists, err := store.loadForCommit(ctx)
	if err != nil {
		return durablecarrier.GlobalCarrierClaims{}, err
	}
	if expected != nil && !current.registry.Equal(*expected) {
		return durablecarrier.GlobalCarrierClaims{}, fmt.Errorf(
			"carrier claim registry changed since confirmed observation",
		)
	}
	next, changed, err := current.registry.WithClaims(claims)
	if err != nil {
		return durablecarrier.GlobalCarrierClaims{}, err
	}
	if !changed {
		return current.registry, nil
	}
	if err := store.commitRegistry(ctx, current, next, references, identity, exists); err != nil {
		return durablecarrier.GlobalCarrierClaims{}, err
	}
	return next, nil
}

// Remove retires one exact claim through entry-identity compare-and-swap.
// The caller remains responsible for proving verified absence or explicit
// unmanage authority before requesting this persistence transition.
func (store Store) Remove(
	ctx context.Context,
	claim durablecarrier.ManagedCarrierClaim,
) (durablecarrier.GlobalCarrierClaims, error) {
	if ctx == nil {
		return durablecarrier.GlobalCarrierClaims{}, fmt.Errorf("carrier claim registry context is required")
	}
	if err := ctx.Err(); err != nil {
		return durablecarrier.GlobalCarrierClaims{}, err
	}
	current, identity, exists, err := store.loadForCommit(ctx)
	if err != nil {
		return durablecarrier.GlobalCarrierClaims{}, err
	}
	next, changed, err := current.registry.WithoutClaim(claim)
	if err != nil {
		return durablecarrier.GlobalCarrierClaims{}, err
	}
	if !changed {
		return current.registry, nil
	}
	if err := store.commitRegistry(ctx, current, next, nil, identity, exists); err != nil {
		return durablecarrier.GlobalCarrierClaims{}, err
	}
	return next, nil
}

// RetireAllIfCurrent commits one strict retirement batch only when the durable
// registry still equals the caller's confirmed baseline. The caller remains
// responsible for proving transition-specific retirement authority for every
// claim before requesting persistence.
func (store Store) RetireAllIfCurrent(
	ctx context.Context,
	expected durablecarrier.GlobalCarrierClaims,
	claims []durablecarrier.ManagedCarrierClaim,
) (durablecarrier.GlobalCarrierClaims, error) {
	if ctx == nil {
		return durablecarrier.GlobalCarrierClaims{}, fmt.Errorf("carrier claim registry context is required")
	}
	if err := ctx.Err(); err != nil {
		return durablecarrier.GlobalCarrierClaims{}, err
	}
	current, identity, exists, err := store.loadForCommit(ctx)
	if err != nil {
		return durablecarrier.GlobalCarrierClaims{}, err
	}
	if !current.registry.Equal(expected) {
		return durablecarrier.GlobalCarrierClaims{}, fmt.Errorf(
			"carrier claim registry changed since confirmed observation",
		)
	}
	next, err := current.registry.RetireClaims(claims)
	if err != nil {
		return durablecarrier.GlobalCarrierClaims{}, err
	}
	if len(claims) == 0 {
		return current.registry, nil
	}
	if err := store.commitRegistry(ctx, current, next, nil, identity, exists); err != nil {
		return durablecarrier.GlobalCarrierClaims{}, err
	}
	return next, nil
}

func (store Store) commitRegistry(
	ctx context.Context,
	current registryDocument,
	next durablecarrier.GlobalCarrierClaims,
	references []pathauthority.Reference,
	identity storagecommit.EntryIdentity,
	exists bool,
) error {
	content, err := current.encodeSuccessor(ctx, next, references)
	if err != nil {
		return err
	}
	var request storagecommit.FileCommit
	if exists {
		request, err = storagecommit.NewFileReplacement(store.path, content, 0o600, identity)
	} else {
		request, err = storagecommit.NewFileCreate(store.path, content, 0o600)
	}
	if err != nil {
		return fmt.Errorf("prepare carrier claim registry commit: %w", err)
	}
	if store.commitFile == nil {
		return fmt.Errorf("carrier claim registry commit capability is required")
	}
	if err := store.commitFile(ctx, request); err != nil {
		return fmt.Errorf("commit carrier claim registry: %w", err)
	}
	return nil
}

func (store Store) loadForCommit(
	ctx context.Context,
) (registryDocument, storagecommit.EntryIdentity, bool, error) {
	if ctx == nil {
		return registryDocument{}, storagecommit.EntryIdentity{}, false, fmt.Errorf("carrier claim registry context is required")
	}
	if err := ctx.Err(); err != nil {
		return registryDocument{}, storagecommit.EntryIdentity{}, false, err
	}
	snapshot, err := storagecommit.ReadRegularFileSnapshotUpTo(ctx, store.path, maximumRegistryBytes)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return emptyLocatedRegistryDocument(), storagecommit.EntryIdentity{}, false, nil
		}
		return registryDocument{}, storagecommit.EntryIdentity{}, false, fmt.Errorf("read carrier claim registry: %w", err)
	}
	if snapshot.Mode().Perm()&0o077 != 0 {
		return registryDocument{}, storagecommit.EntryIdentity{}, false, fmt.Errorf("carrier claim registry %q permissions %04o expose authority metadata", store.path, snapshot.Mode().Perm())
	}
	identity, valid := snapshot.Identity().(storagecommit.EntryIdentity)
	if !valid {
		return registryDocument{}, storagecommit.EntryIdentity{}, false, fmt.Errorf("carrier claim registry snapshot has unsupported entry identity %T", snapshot.Identity())
	}
	document, err := decodeRegistryDocument(snapshot.Content())
	if err != nil {
		return registryDocument{}, storagecommit.EntryIdentity{}, false, err
	}
	if err := document.validateAuthorities(ctx); err != nil {
		return registryDocument{}, storagecommit.EntryIdentity{}, false, err
	}
	if err := ctx.Err(); err != nil {
		return registryDocument{}, storagecommit.EntryIdentity{}, false, err
	}
	return document, identity, true, nil
}
