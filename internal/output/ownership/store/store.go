// Package store persists the shared managed-output ownership registry.
package store

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path/filepath"
	"sort"

	"github.com/isty2e/daem/internal/assurance/pathauthority"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	"github.com/isty2e/daem/internal/effect/mutation"
	ownershipmutation "github.com/isty2e/daem/internal/effect/mutation/ownership"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	storagecommit "github.com/isty2e/daem/internal/effect/storage/commit"
	"github.com/isty2e/daem/internal/encoding/jsonstrict"
	"github.com/isty2e/daem/internal/output/ownership"
)

const (
	currentVersion                    = 2
	maximumOwnershipRegistryBytes     = 16 << 20
	maximumOwnershipRegistryJSONDepth = 64
)

// Store owns strict serialization and atomic guarded writes for one registry path.
type Store struct {
	path                 string
	root                 *rootedpath.CapturedRoot
	destination          rootedpath.Destination
	maximumPhysicalDepth int
	physicalWorkBudget   rootedpath.PhysicalTraversalBudget
}

var _ ownershipmutation.RegistryStore = Store{}

// BindRooted constructs a registry store bound to retained physical-root authority.
func BindRooted(
	root *rootedpath.CapturedRoot,
	destination rootedpath.Destination,
	maximumPhysicalDepth int,
	budget rootedpath.PhysicalTraversalBudget,
) (ownershipmutation.RegistryStore, error) {
	store, err := NewRooted(root, destination, maximumPhysicalDepth, budget)
	if err != nil {
		return nil, err
	}
	return store, nil
}

// New validates and canonicalizes the absolute registry path for ambient
// read/write use.
func New(path string) (Store, error) {
	store, err := newLexicalStore(path)
	if err != nil {
		return Store{}, err
	}
	canonical, err := mutation.CanonicalDirectoryEntryPath(store.path)
	if err != nil {
		return Store{}, fmt.Errorf("canonicalize ownership registry path: %w", err)
	}
	store.path = canonical
	return store, nil
}

// NewRecoveryReader configures a recovery-only reader without observing the
// filesystem. LoadForClaimRemovals performs the only physical resolution under
// the caller's recovery budget.
func NewRecoveryReader(path string) (ownershipmutation.RegistryReader, error) {
	store, err := newLexicalStore(path)
	if err != nil {
		return nil, err
	}
	return store, nil
}

func newLexicalStore(path string) (Store, error) {
	if path == "" || !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return Store{}, fmt.Errorf("ownership registry path %q must be absolute and clean", path)
	}
	return Store{path: path}, nil
}

// NewRooted constructs a rooted store whose every physical authority
// observation consumes the supplied operation-wide budget. The caller owns
// root and must keep it open for the store's lifetime.
func NewRooted(
	root *rootedpath.CapturedRoot,
	destination rootedpath.Destination,
	maximumPhysicalDepth int,
	budget rootedpath.PhysicalTraversalBudget,
) (Store, error) {
	if maximumPhysicalDepth <= 0 {
		return Store{}, fmt.Errorf("ownership registry maximum physical depth must be positive")
	}
	if budget == nil {
		return Store{}, fmt.Errorf("ownership registry physical work budget is required")
	}
	if root == nil {
		return Store{}, fmt.Errorf("ownership registry root authority is required")
	}
	if err := destination.Validate(); err != nil {
		return Store{}, fmt.Errorf("ownership registry destination: %w", err)
	}
	capability, err := root.AcquireBounded(destination, maximumPhysicalDepth, budget)
	if err != nil {
		return Store{}, fmt.Errorf("bind ownership registry destination: %w", err)
	}
	path, pathErr := destination.LexicalPath()
	closeErr := capability.Close()
	if pathErr != nil || closeErr != nil {
		return Store{}, errors.Join(pathErr, closeErr)
	}
	return Store{
		path: path, root: root, destination: destination,
		maximumPhysicalDepth: maximumPhysicalDepth,
		physicalWorkBudget:   budget,
	}, nil
}

// Path returns the persisted registry path.
func (store Store) Path() string {
	return store.path
}

// Load reads current or exact empty retired ownership state. A missing file is empty.
func (store Store) Load(ctx context.Context) (ownership.Registry, error) {
	observations, err := store.newPathAuthorityObservationSession()
	if err != nil {
		return ownership.Registry{}, err
	}
	return store.load(ctx, nil, observations)
}

func (store Store) newPathAuthorityObservationSession() (*pathAuthorityObservationSession, error) {
	if store.physicalWorkBudget == nil {
		return newPathAuthorityObservationSession(), nil
	}
	return newBoundedPathAuthorityObservationSession(
		store.maximumPhysicalDepth,
		store.physicalWorkBudget,
	)
}

// LoadForClaimRemovals reads the registry while admitting a missing path only
// for an exact expected claim that durable recovery is authorized to remove.
// Every registry and claim-authority traversal consumes the caller's recovery
// planning budget before filesystem observation begins.
func (store Store) LoadForClaimRemovals(
	ctx context.Context,
	expected []ownership.Claim,
	maximumPhysicalDepth int,
	budget rootedpath.PhysicalTraversalBudget,
) (ownership.Registry, error) {
	for index, claim := range expected {
		if err := claim.Validate(); err != nil {
			return ownership.Registry{}, fmt.Errorf("removed ownership claim[%d]: %w", index, err)
		}
	}
	observations, err := newBoundedPathAuthorityObservationSession(
		maximumPhysicalDepth,
		budget,
	)
	if err != nil {
		return ownership.Registry{}, err
	}
	return store.load(ctx, expected, observations)
}

func (store Store) load(
	ctx context.Context,
	expectedRemovals []ownership.Claim,
	observations *pathAuthorityObservationSession,
) (ownership.Registry, error) {
	if ctx == nil {
		return ownership.Registry{}, fmt.Errorf("ownership registry context is required")
	}
	if err := ctx.Err(); err != nil {
		return ownership.Registry{}, err
	}
	if store.root != nil {
		return store.loadRooted(ctx, expectedRemovals, observations)
	}
	if observations.bounded() {
		return store.loadAmbientBounded(ctx, expectedRemovals, observations)
	}
	snapshot, err := storagecommit.ReadRegularFileSnapshotUpTo(ctx, store.path, maximumOwnershipRegistryBytes)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return ownership.EmptyRegistry(), nil
		}
		return ownership.Registry{}, fmt.Errorf("read ownership registry: %w", err)
	}
	if snapshot.Mode().Perm()&0o077 != 0 {
		return ownership.Registry{}, fmt.Errorf(
			"ownership registry %q permissions %04o expose authority metadata",
			store.path,
			snapshot.Mode().Perm(),
		)
	}
	if err := ctx.Err(); err != nil {
		return ownership.Registry{}, err
	}
	return decodePersistedRegistryForClaimRemovals(
		ctx,
		snapshot.Content(),
		expectedRemovals,
		observations,
	)
}

func decodePersistedRegistry(ctx context.Context, content []byte) (ownership.Registry, error) {
	return decodePersistedRegistryForClaimRemovals(
		ctx,
		content,
		nil,
		newPathAuthorityObservationSession(),
	)
}

func decodePersistedRegistryForClaimRemovals(
	ctx context.Context,
	content []byte,
	expected []ownership.Claim,
	observations *pathAuthorityObservationSession,
) (ownership.Registry, error) {
	registry, err := decode(content)
	if err != nil {
		return ownership.Registry{}, err
	}
	if err := validateCurrentAuthoritiesForClaimRemovals(
		ctx,
		registry,
		expected,
		observations,
	); err != nil {
		return ownership.Registry{}, err
	}
	return registry, nil
}

func validateCurrentAuthorities(ctx context.Context, registry ownership.Registry) error {
	return validateCurrentAuthoritiesForClaimRemovals(
		ctx,
		registry,
		nil,
		newPathAuthorityObservationSession(),
	)
}

func validateCurrentAuthoritiesForClaimRemovals(
	ctx context.Context,
	registry ownership.Registry,
	expected []ownership.Claim,
	observations *pathAuthorityObservationSession,
) error {
	if observations == nil {
		return fmt.Errorf("ownership path-authority observation session is required")
	}
	// Claim is a canonical comparable value, and direct equality is the same
	// exact persisted identity as Claim.Equal. Index once so registry admission
	// remains O(claims + expected removals).
	selectedForRemoval := make(map[ownership.Claim]struct{}, len(expected))
	for _, claim := range expected {
		selectedForRemoval[claim] = struct{}{}
	}
	for index, claim := range registry.Claims() {
		if err := ctx.Err(); err != nil {
			return err
		}
		if _, selected := selectedForRemoval[claim]; selected {
			observed, err := observations.observe(claim.Address().Path())
			if err != nil {
				return fmt.Errorf("observe ownership registry claims[%d] path authority: %w", index, err)
			}
			if err := validateClaimRemovalPathAuthority(claim, observed); err != nil {
				return fmt.Errorf("observe ownership registry claims[%d] path authority: %w", index, err)
			}
		} else {
			currentPath, err := observations.observePersisted(claim.Address().Path())
			if err != nil {
				return fmt.Errorf("observe ownership registry claims[%d] path authority: %w", index, err)
			}
			if !currentPath.Equal(claim.Address().PathAuthority()) {
				return fmt.Errorf(
					"ownership registry claims[%d] path authority %q with semantics %q is not current; observed %q with semantics %q",
					index,
					claim.Address().Path(),
					claim.Address().PathAuthority().Witness(),
					currentPath.Key(),
					currentPath.Witness(),
				)
			}
		}

		currentStatefile, err := observations.observePersisted(claim.Owner().StatefileKey())
		if err != nil {
			return fmt.Errorf("observe ownership registry claims[%d] statefile authority: %w", index, err)
		}
		if !currentStatefile.Equal(claim.Owner().StatefileAuthority()) {
			return fmt.Errorf(
				"ownership registry claims[%d] statefile authority %q with semantics %q is not current; observed %q with semantics %q",
				index,
				claim.Owner().StatefileKey(),
				claim.Owner().StatefileAuthority().Witness(),
				currentStatefile.Key(),
				currentStatefile.Witness(),
			)
		}
	}
	return nil
}

func validateClaimRemovalPathAuthority(
	claim ownership.Claim,
	observed mutation.DirectoryEntryAuthorityObservation,
) error {
	if exact, present := observed.Exact(); present {
		if exact.Equal(claim.Address().PathAuthority()) {
			return nil
		}
		return fmt.Errorf(
			"ownership claim path authority %q with semantics %q is not current; observed %q with semantics %q",
			claim.Address().Path(),
			claim.Address().PathAuthority().Witness(),
			exact.Key(),
			exact.Witness(),
		)
	}
	provisional, present := observed.Provisional()
	if !present {
		return fmt.Errorf("ownership claim path authority observation is empty")
	}
	if !provisional.MatchesMissingExact(claim.Address().PathAuthority()) {
		return fmt.Errorf(
			"ownership claim path authority %q with semantics %q does not match missing candidate %q with semantics %q",
			claim.Address().Path(),
			claim.Address().PathAuthority().Witness(),
			provisional.CandidateKey(),
			provisional.CandidateWitness(),
		)
	}
	return nil
}

func (store Store) loadRooted(
	ctx context.Context,
	expectedRemovals []ownership.Claim,
	observations *pathAuthorityObservationSession,
) (ownership.Registry, error) {
	if !observations.bounded() {
		return ownership.Registry{}, fmt.Errorf("rooted ownership registry budget is unavailable")
	}
	capability, err := store.root.AcquireBounded(
		store.destination,
		observations.maximumPhysicalDepth,
		observations.budget,
	)
	if err != nil {
		return ownership.Registry{}, fmt.Errorf("acquire ownership registry: %w", err)
	}
	return store.loadRootedCapability(ctx, capability, expectedRemovals, observations)
}

func (store Store) loadAmbientBounded(
	ctx context.Context,
	expectedRemovals []ownership.Claim,
	observations *pathAuthorityObservationSession,
) (ownership.Registry, error) {
	root, destination, err := rootedpath.CaptureDestinationBounded(
		store.path,
		observations.maximumPhysicalDepth,
		observations.budget,
	)
	if err != nil {
		return ownership.Registry{}, fmt.Errorf("bind ownership registry: %w", err)
	}
	capability, acquireErr := root.AcquireBounded(
		destination,
		observations.maximumPhysicalDepth,
		observations.budget,
	)
	if acquireErr != nil {
		return ownership.Registry{}, errors.Join(
			fmt.Errorf("acquire ownership registry: %w", acquireErr),
			root.Close(),
		)
	}
	registry, loadErr := store.loadRootedCapability(
		ctx,
		capability,
		expectedRemovals,
		observations,
	)
	return registry, errors.Join(loadErr, root.Close())
}

func (store Store) loadRootedCapability(
	ctx context.Context,
	capability rootedpath.CommitCapability,
	expectedRemovals []ownership.Claim,
	observations *pathAuthorityObservationSession,
) (ownership.Registry, error) {
	content, mode, _, readErr := storagecommit.ReadRootedRegularFileUpTo(
		ctx,
		capability,
		maximumOwnershipRegistryBytes,
	)
	closeErr := capability.Close()
	if readErr != nil {
		if errors.Is(readErr, fs.ErrNotExist) {
			return ownership.EmptyRegistry(), closeErr
		}
		return ownership.Registry{}, errors.Join(fmt.Errorf("read ownership registry: %w", readErr), closeErr)
	}
	if closeErr != nil {
		return ownership.Registry{}, closeErr
	}
	if mode.Perm()&0o077 != 0 {
		return ownership.Registry{}, fmt.Errorf(
			"ownership registry %q permissions %04o expose authority metadata",
			store.path,
			mode.Perm(),
		)
	}
	return decodePersistedRegistryForClaimRemovals(
		ctx,
		content,
		expectedRemovals,
		observations,
	)
}

type pathAuthorityObservationSession struct {
	maximumPhysicalDepth int
	budget               rootedpath.PhysicalTraversalBudget
	byPath               map[string]mutation.DirectoryEntryAuthorityObservation
}

func newPathAuthorityObservationSession() *pathAuthorityObservationSession {
	return &pathAuthorityObservationSession{
		byPath: make(map[string]mutation.DirectoryEntryAuthorityObservation),
	}
}

func newBoundedPathAuthorityObservationSession(
	maximumPhysicalDepth int,
	budget rootedpath.PhysicalTraversalBudget,
) (*pathAuthorityObservationSession, error) {
	if maximumPhysicalDepth <= 0 {
		return nil, fmt.Errorf("ownership path-authority maximum physical depth must be positive")
	}
	if budget == nil {
		return nil, fmt.Errorf("ownership path-authority traversal budget is required")
	}
	return &pathAuthorityObservationSession{
		maximumPhysicalDepth: maximumPhysicalDepth,
		budget:               budget,
		byPath:               make(map[string]mutation.DirectoryEntryAuthorityObservation),
	}, nil
}

func (session *pathAuthorityObservationSession) bounded() bool {
	return session != nil && session.budget != nil
}

func (session *pathAuthorityObservationSession) observe(
	path string,
) (mutation.DirectoryEntryAuthorityObservation, error) {
	if session == nil {
		return mutation.DirectoryEntryAuthorityObservation{}, fmt.Errorf(
			"ownership path-authority observation session is required",
		)
	}
	if observed, ok := session.byPath[path]; ok {
		return observed, nil
	}
	var (
		observed mutation.DirectoryEntryAuthorityObservation
		err      error
	)
	if session.bounded() {
		observed, err = mutation.ObserveDirectoryEntryAuthorityBounded(
			path,
			session.maximumPhysicalDepth,
			session.budget,
		)
	} else {
		observed, err = mutation.ObserveDirectoryEntryAuthority(path)
	}
	if err != nil {
		return mutation.DirectoryEntryAuthorityObservation{}, err
	}
	session.byPath[path] = observed
	return observed, nil
}

func (session *pathAuthorityObservationSession) observePersisted(
	path string,
) (pathauthority.Exact, error) {
	observed, err := session.observe(path)
	if err != nil {
		return pathauthority.Exact{}, err
	}
	exact, ok := observed.Exact()
	if !ok {
		return pathauthority.Exact{}, fmt.Errorf(
			"path %q has provisional authority until its normalization-sensitive entry becomes visible",
			path,
		)
	}
	return exact, nil
}

// Apply writes one exact expected-before transition without changing unrelated claims.
func (store Store) Apply(
	ctx context.Context,
	address ownership.ManagedAddress,
	expected ownership.ClaimValue,
	replacement ownership.ClaimValue,
) (ownership.Registry, error) {
	if ctx == nil {
		return ownership.Registry{}, fmt.Errorf("ownership registry context is required")
	}
	if err := ctx.Err(); err != nil {
		return ownership.Registry{}, err
	}

	current, expectedEntry, exists, capability, err := store.loadForCommit(ctx)
	if err != nil {
		return ownership.Registry{}, err
	}
	capabilityConsumed := false
	defer func() {
		if capability != nil && !capabilityConsumed {
			_ = capability.Close()
		}
	}()
	next, err := current.Apply(address, expected, replacement)
	if err != nil {
		return ownership.Registry{}, err
	}
	if expected.Equal(replacement) {
		return next, nil
	}
	content, err := encode(next)
	if err != nil {
		return ownership.Registry{}, err
	}

	var request storagecommit.FileCommit
	if capability != nil && exists {
		request, err = storagecommit.NewRootedFileReplacement(capability, content, 0o600, expectedEntry)
	} else if capability != nil {
		request, err = storagecommit.NewRootedFileCreate(capability, content, 0o600)
	} else if exists {
		request, err = storagecommit.NewFileReplacement(store.path, content, 0o600, expectedEntry)
	} else {
		request, err = storagecommit.NewFileCreate(store.path, content, 0o600)
	}
	if err != nil {
		return ownership.Registry{}, fmt.Errorf("prepare ownership registry commit: %w", err)
	}
	capabilityConsumed = capability != nil
	if err := storagecommit.CommitFile(ctx, request); err != nil {
		return ownership.Registry{}, fmt.Errorf("commit ownership registry: %w", err)
	}
	return next, nil
}

func (store Store) loadForCommit(
	ctx context.Context,
) (
	ownership.Registry,
	storagecommit.EntryIdentity,
	bool,
	rootedpath.CommitCapability,
	error,
) {
	if store.root != nil {
		observations, observationErr := store.newPathAuthorityObservationSession()
		if observationErr != nil {
			return ownership.Registry{}, storagecommit.EntryIdentity{}, false, nil, observationErr
		}
		if !observations.bounded() {
			return ownership.Registry{}, storagecommit.EntryIdentity{}, false, nil, fmt.Errorf(
				"rooted ownership registry budget is unavailable",
			)
		}
		capability, err := store.root.AcquireBounded(
			store.destination,
			observations.maximumPhysicalDepth,
			observations.budget,
		)
		if err != nil {
			return ownership.Registry{}, storagecommit.EntryIdentity{}, false, nil, fmt.Errorf(
				"acquire ownership registry: %w",
				err,
			)
		}
		content, mode, identity, err := storagecommit.ReadRootedRegularFileUpTo(
			ctx,
			capability,
			maximumOwnershipRegistryBytes,
		)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return ownership.EmptyRegistry(), storagecommit.EntryIdentity{}, false, capability, nil
			}
			_ = capability.Close()
			return ownership.Registry{}, storagecommit.EntryIdentity{}, false, nil, fmt.Errorf(
				"read ownership registry: %w",
				err,
			)
		}
		if mode.Perm()&0o077 != 0 {
			_ = capability.Close()
			return ownership.Registry{}, storagecommit.EntryIdentity{}, false, nil, fmt.Errorf(
				"ownership registry %q permissions %04o expose authority metadata",
				store.path,
				mode.Perm(),
			)
		}
		registry, err := decodePersistedRegistryForClaimRemovals(
			ctx,
			content,
			nil,
			observations,
		)
		if err != nil {
			_ = capability.Close()
			return ownership.Registry{}, storagecommit.EntryIdentity{}, false, nil, err
		}
		return registry, identity, true, capability, nil
	}
	identity, err := storagecommit.CaptureEntryIdentity(ctx, store.path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return ownership.EmptyRegistry(), storagecommit.EntryIdentity{}, false, nil, nil
		}
		return ownership.Registry{}, storagecommit.EntryIdentity{}, false, nil, fmt.Errorf("capture ownership registry identity: %w", err)
	}
	registry, err := store.Load(ctx)
	if err != nil {
		return ownership.Registry{}, storagecommit.EntryIdentity{}, false, nil, err
	}
	return registry, identity, true, nil, nil
}

type file struct {
	Version int           `json:"version"`
	Claims  []claimRecord `json:"claims"`
}

type claimRecord struct {
	PathAuthority      pathAuthorityRecord `json:"path_authority"`
	ContentPath        string              `json:"content_path,omitempty"`
	StatefileAuthority pathAuthorityRecord `json:"statefile_authority"`
	ManifestPath       string              `json:"manifest_path"`
	State              string              `json:"state"`
	OperationID        string              `json:"operation_id,omitempty"`
}

type pathAuthorityRecord struct {
	Key     string `json:"key"`
	Witness string `json:"semantics_witness"`
}

func decode(content []byte) (ownership.Registry, error) {
	if int64(len(content)) > maximumOwnershipRegistryBytes {
		return ownership.Registry{}, fmt.Errorf("ownership registry exceeds %d bytes", maximumOwnershipRegistryBytes)
	}
	version, err := jsonstrict.ValidateVersionedObject(
		content,
		"ownership registry",
		maximumOwnershipRegistryJSONDepth,
	)
	if err != nil {
		return ownership.Registry{}, err
	}
	switch version {
	case currentVersion:
		return decodeCurrent(content)
	case retiredOwnershipRegistryVersion:
		return decodeRetiredOwnershipRegistry(content)
	default:
		return ownership.Registry{}, unsupportedOwnershipRegistryVersion(version)
	}
}

func decodeCurrent(content []byte) (ownership.Registry, error) {
	var persisted file
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&persisted); err != nil {
		return ownership.Registry{}, fmt.Errorf("decode ownership registry: %w", err)
	}
	var extra json.RawMessage
	if err := decoder.Decode(&extra); err == nil {
		return ownership.Registry{}, fmt.Errorf("ownership registry contains multiple JSON values")
	} else if err != io.EOF {
		return ownership.Registry{}, fmt.Errorf("decode ownership registry trailer: %w", err)
	}
	if persisted.Version != currentVersion {
		return ownership.Registry{}, unsupportedOwnershipRegistryVersion(persisted.Version)
	}
	if persisted.Claims == nil {
		return ownership.Registry{}, fmt.Errorf("ownership registry requires claims array")
	}

	claims := make([]ownership.Claim, 0, len(persisted.Claims))
	for index, record := range persisted.Claims {
		path, err := pathauthority.NewExact(
			record.PathAuthority.Key,
			record.PathAuthority.Witness,
		)
		if err != nil {
			return ownership.Registry{}, fmt.Errorf("ownership registry claims[%d] path authority: %w", index, err)
		}
		address, err := ownership.NewManagedAddress(path, record.ContentPath)
		if err != nil {
			return ownership.Registry{}, fmt.Errorf("ownership registry claims[%d] address: %w", index, err)
		}
		statefile, err := pathauthority.NewExact(
			record.StatefileAuthority.Key,
			record.StatefileAuthority.Witness,
		)
		if err != nil {
			return ownership.Registry{}, fmt.Errorf("ownership registry claims[%d] statefile authority: %w", index, err)
		}
		authority, err := stateauthority.New(statefile, record.ManifestPath)
		if err != nil {
			return ownership.Registry{}, fmt.Errorf("ownership registry claims[%d] owner: %w", index, err)
		}
		var claim ownership.Claim
		switch ownership.ClaimState(record.State) {
		case ownership.ClaimReserved:
			claim, err = ownership.NewReservedClaim(address, authority, record.OperationID)
		case ownership.ClaimActive:
			if record.OperationID != "" {
				err = fmt.Errorf("active claim must not record operation_id")
			} else {
				claim, err = ownership.NewActiveClaim(address, authority)
			}
		default:
			err = fmt.Errorf("unsupported claim state %q", record.State)
		}
		if err != nil {
			return ownership.Registry{}, fmt.Errorf("ownership registry claims[%d]: %w", index, err)
		}
		claims = append(claims, claim)
	}
	return ownership.NewRegistry(claims)
}

func unsupportedOwnershipRegistryVersion(version int) error {
	if version > currentVersion {
		return fmt.Errorf(
			"unsupported ownership registry version %d; it was written by a newer daem, so upgrade daem before reading it",
			version,
		)
	}
	return fmt.Errorf(
		"unsupported ownership registry version %d; use the daem version that wrote it to recover or retire managed ownership before upgrading",
		version,
	)
}

func encode(registry ownership.Registry) ([]byte, error) {
	claims := registry.Claims()
	sort.Slice(claims, func(left int, right int) bool {
		return claims[left].Address().Less(claims[right].Address())
	})
	persisted := file{Version: currentVersion, Claims: make([]claimRecord, 0, len(claims))}
	for _, claim := range claims {
		path := claim.Address().PathAuthority()
		statefile := claim.Owner().StatefileAuthority()
		persisted.Claims = append(persisted.Claims, claimRecord{
			PathAuthority: pathAuthorityRecord{
				Key:     path.Key(),
				Witness: path.Witness(),
			},
			ContentPath: claim.Address().ContentPath(),
			StatefileAuthority: pathAuthorityRecord{
				Key:     statefile.Key(),
				Witness: statefile.Witness(),
			},
			ManifestPath: claim.Owner().ManifestPath(),
			State:        string(claim.State()),
			OperationID:  claim.OperationID(),
		})
	}
	content, err := json.MarshalIndent(persisted, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode ownership registry: %w", err)
	}
	return append(content, '\n'), nil
}
