package transaction

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"sync"

	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	storagecommit "github.com/isty2e/daem/internal/effect/storage/commit"
)

const (
	stateDirAuthorityFingerprintDomain      = "daem-state-dir-authority-v1"
	defaultStateDirMaximumPhysicalDepth     = mutationfs.MaximumPhysicalPathDepth
	defaultStateDirMaximumPathComponentWork = mutationfs.MaximumPhysicalPathComponentVisits
)

// ErrStateDirAppeared reports a planning-time absence that another operation
// filled before this operation accepted its own creation transition.
var ErrStateDirAppeared = errors.New("file-set state directory appeared after planning")

type stateDirAuthorityState struct {
	mu         sync.Mutex
	creationMu sync.Mutex

	path                 string
	namespacePath        string
	namespaceIdentity    storagecommit.EntryIdentity
	namespaceMount       string
	namespaceIncarnation string
	plannedPresent       bool
	plannedIdentity      storagecommit.EntryIdentity
	plannedMount         string
	plannedIncarnation   string
	currentPresent       bool
	currentIdentity      storagecommit.EntryIdentity
	currentMount         string
	currentIncarnation   string
	maximumPhysicalDepth int
	physicalWorkBudget   rootedpath.PhysicalTraversalBudget
}

type stateDirAuthoritySnapshot struct {
	path                 string
	namespacePath        string
	namespaceIdentity    storagecommit.EntryIdentity
	namespaceMount       string
	namespaceIncarnation string
	plannedPresent       bool
	plannedIdentity      storagecommit.EntryIdentity
	plannedMount         string
	plannedIncarnation   string
}

// StateDirAuthority is shared operation-local evidence for one selected
// StateDir namespace. A planning-time absence remains absent until this
// authority creates and binds the exact directory through storage evidence.
type StateDirAuthority struct {
	state *stateDirAuthorityState
}

type stateDirPathBudget struct {
	used int
}

func (budget *stateDirPathBudget) AdmitPathComponents(count int) error {
	if count < 0 || count > defaultStateDirMaximumPhysicalDepth {
		return fmt.Errorf("file-set state directory path depth is invalid")
	}
	if count > defaultStateDirMaximumPathComponentWork-budget.used {
		return fmt.Errorf(
			"file-set state directory path work exceeds operation limit %d",
			defaultStateDirMaximumPathComponentWork,
		)
	}
	budget.used += count
	return nil
}

// CaptureStateDirAuthority captures a no-follow existing namespace ancestor
// plus the present StateDir object identity. Native handles are not retained.
func CaptureStateDirAuthority(ctx context.Context, stateDir string) (StateDirAuthority, error) {
	return CaptureStateDirAuthorityBounded(
		ctx,
		stateDir,
		defaultStateDirMaximumPhysicalDepth,
		&stateDirPathBudget{},
	)
}

// CaptureStateDirAuthorityBounded captures StateDir authority while charging
// canonicalization, namespace selection, and current-entry observation to one
// operation-owned physical traversal budget.
func CaptureStateDirAuthorityBounded(
	ctx context.Context,
	stateDir string,
	maximumPhysicalDepth int,
	physicalWorkBudget rootedpath.PhysicalTraversalBudget,
) (StateDirAuthority, error) {
	if ctx == nil {
		return StateDirAuthority{}, fmt.Errorf("file-set transaction context is required")
	}
	if maximumPhysicalDepth <= 0 {
		return StateDirAuthority{}, fmt.Errorf("file-set state directory maximum physical depth must be positive")
	}
	if physicalWorkBudget == nil {
		return StateDirAuthority{}, fmt.Errorf("file-set state directory physical work budget is required")
	}
	if err := ctx.Err(); err != nil {
		return StateDirAuthority{}, err
	}
	canonical, err := canonicalStateDirBounded(stateDir, maximumPhysicalDepth, physicalWorkBudget)
	if err != nil {
		return StateDirAuthority{}, err
	}
	namespacePath, namespaceIdentity, namespaceMount, namespaceIncarnation, err := captureStateDirNamespace(
		ctx,
		canonical,
		maximumPhysicalDepth,
		physicalWorkBudget,
	)
	if err != nil {
		return StateDirAuthority{}, err
	}
	present, identity, mount, incarnation, err := observeCurrentStateDir(
		ctx,
		canonical,
		maximumPhysicalDepth,
		physicalWorkBudget,
	)
	if err != nil {
		return StateDirAuthority{}, err
	}
	return StateDirAuthority{state: &stateDirAuthorityState{
		path:                 canonical,
		namespacePath:        namespacePath,
		namespaceIdentity:    namespaceIdentity,
		namespaceMount:       namespaceMount,
		namespaceIncarnation: namespaceIncarnation,
		plannedPresent:       present,
		plannedIdentity:      identity,
		plannedMount:         mount,
		plannedIncarnation:   incarnation,
		currentPresent:       present,
		currentIdentity:      identity,
		currentMount:         mount,
		currentIncarnation:   incarnation,
		maximumPhysicalDepth: maximumPhysicalDepth,
		physicalWorkBudget:   physicalWorkBudget,
	}}, nil
}

func captureStateDirNamespace(
	ctx context.Context,
	stateDir string,
	maximumPhysicalDepth int,
	physicalWorkBudget rootedpath.PhysicalTraversalBudget,
) (string, storagecommit.EntryIdentity, string, string, error) {
	candidate := filepath.Dir(stateDir)
	for {
		if err := ctx.Err(); err != nil {
			return "", storagecommit.EntryIdentity{}, "", "", err
		}
		if err := chargeStateDirPath(candidate, maximumPhysicalDepth, physicalWorkBudget); err != nil {
			return "", storagecommit.EntryIdentity{}, "", "", err
		}
		identity, err := storagecommit.ObserveEntryIdentity(ctx, candidate)
		if err == nil {
			if identity.Kind() != mutationfs.EntryKindDirectory {
				return "", storagecommit.EntryIdentity{}, "", "", wrapFileSetAccessUnprovable(
					fmt.Errorf("file-set state directory namespace %q is not a directory", candidate),
				)
			}
			if err := chargeStateDirPath(candidate, maximumPhysicalDepth, physicalWorkBudget); err != nil {
				return "", storagecommit.EntryIdentity{}, "", "", err
			}
			mount, incarnation, platformErr := observeStateDirPlatform(ctx, candidate)
			if platformErr != nil {
				return "", storagecommit.EntryIdentity{}, "", "", wrapFileSetAccessUnprovable(
					fmt.Errorf("capture file-set state directory namespace platform identity: %w", platformErr),
				)
			}
			return candidate, identity, mount, incarnation, nil
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return "", storagecommit.EntryIdentity{}, "", "", wrapFileSetAccessUnprovable(
				fmt.Errorf("capture file-set state directory namespace: %w", err),
			)
		}
		parent := filepath.Dir(candidate)
		if parent == candidate {
			return "", storagecommit.EntryIdentity{}, "", "", wrapFileSetAccessUnprovable(
				fmt.Errorf("file-set state directory has no observable namespace ancestor"),
			)
		}
		candidate = parent
	}
}

func (authority StateDirAuthority) snapshot() (stateDirAuthoritySnapshot, bool) {
	if authority.state == nil {
		return stateDirAuthoritySnapshot{}, false
	}
	authority.state.mu.Lock()
	defer authority.state.mu.Unlock()
	if authority.state.path == "" || authority.state.namespacePath == "" {
		return stateDirAuthoritySnapshot{}, false
	}
	return stateDirAuthoritySnapshot{
		path:                 authority.state.path,
		namespacePath:        authority.state.namespacePath,
		namespaceIdentity:    authority.state.namespaceIdentity,
		namespaceMount:       authority.state.namespaceMount,
		namespaceIncarnation: authority.state.namespaceIncarnation,
		plannedPresent:       authority.state.plannedPresent,
		plannedIdentity:      authority.state.plannedIdentity,
		plannedMount:         authority.state.plannedMount,
		plannedIncarnation:   authority.state.plannedIncarnation,
	}, true
}

// Equal reports whether two observations bind the same planning-time
// namespace and StateDir incarnation. It grants no filesystem authority.
func (authority StateDirAuthority) Equal(other StateDirAuthority) bool {
	left, leftOK := authority.snapshot()
	right, rightOK := other.snapshot()
	if !leftOK || !rightOK || left.path != right.path ||
		left.namespacePath != right.namespacePath ||
		left.namespaceMount != right.namespaceMount ||
		left.namespaceIncarnation != right.namespaceIncarnation ||
		left.plannedPresent != right.plannedPresent ||
		!left.namespaceIdentity.SameObject(right.namespaceIdentity) {
		return false
	}
	return !left.plannedPresent ||
		left.plannedMount == right.plannedMount &&
			left.plannedIncarnation == right.plannedIncarnation &&
			left.plannedIdentity.SameObject(right.plannedIdentity)
}

// IdentityFingerprint returns an opaque immutable planning identity for
// workflow fingerprints. It is not durable recovery evidence.
func (authority StateDirAuthority) IdentityFingerprint() (string, error) {
	snapshot, ok := authority.snapshot()
	if !ok {
		return "", fmt.Errorf("file-set state directory authority is uninitialized")
	}
	namespace, err := snapshot.namespaceIdentity.ObjectFingerprint()
	if err != nil {
		return "", fmt.Errorf("fingerprint file-set state directory namespace: %w", err)
	}
	state := "absent"
	if snapshot.plannedPresent {
		state, err = snapshot.plannedIdentity.ObjectFingerprint()
		if err != nil {
			return "", fmt.Errorf("fingerprint file-set state directory identity: %w", err)
		}
	}
	canonical, err := json.Marshal(struct {
		Path                 string
		NamespacePath        string
		Namespace            string
		NamespaceMount       string
		NamespaceIncarnation string
		Present              bool
		State                string
		StateMount           string
		StateIncarnation     string
	}{
		Path:                 snapshot.path,
		NamespacePath:        snapshot.namespacePath,
		Namespace:            namespace,
		NamespaceMount:       snapshot.namespaceMount,
		NamespaceIncarnation: snapshot.namespaceIncarnation,
		Present:              snapshot.plannedPresent,
		State:                state,
		StateMount:           snapshot.plannedMount,
		StateIncarnation:     snapshot.plannedIncarnation,
	})
	if err != nil {
		return "", fmt.Errorf("fingerprint file-set state directory authority: %w", err)
	}
	hasher := sha256.New()
	hasher.Write([]byte(stateDirAuthorityFingerprintDomain))
	hasher.Write([]byte{0})
	hasher.Write(canonical)
	return "sha256:" + hex.EncodeToString(hasher.Sum(nil)), nil
}

// PresentAtCapture reports whether planning observed a StateDir directory.
func (authority StateDirAuthority) PresentAtCapture() bool {
	snapshot, ok := authority.snapshot()
	return ok && snapshot.plannedPresent
}

// Validate proves that the selected StateDir still names the planning-time
// namespace and bound directory incarnation.
func (authority StateDirAuthority) Validate(ctx context.Context) error {
	snapshot, ok := authority.snapshot()
	if !ok {
		return fmt.Errorf("file-set state directory authority is uninitialized")
	}
	if ctx == nil {
		return fmt.Errorf("file-set transaction context is required")
	}
	if err := validateStateDirNamespace(
		ctx,
		snapshot,
		authority.state.maximumPhysicalDepth,
		authority.state.physicalWorkBudget,
	); err != nil {
		return err
	}
	present, observedIdentity, observedMount, observedIncarnation, err := observeCurrentStateDir(
		ctx,
		snapshot.path,
		authority.state.maximumPhysicalDepth,
		authority.state.physicalWorkBudget,
	)
	if err != nil {
		return err
	}

	authority.state.mu.Lock()
	defer authority.state.mu.Unlock()
	if authority.state.currentPresent {
		if !present || authority.state.currentMount != observedMount ||
			authority.state.currentIncarnation != observedIncarnation ||
			!authority.state.currentIdentity.SameObject(observedIdentity) {
			return wrapFileSetAccessUnprovable(fmt.Errorf(
				"file-set state directory identity changed after planning",
			))
		}
		return nil
	}
	if !present {
		return nil
	}
	return ErrStateDirAppeared
}

func validateStateDirNamespace(
	ctx context.Context,
	snapshot stateDirAuthoritySnapshot,
	maximumPhysicalDepth int,
	physicalWorkBudget rootedpath.PhysicalTraversalBudget,
) error {
	if err := chargeStateDirPath(snapshot.namespacePath, maximumPhysicalDepth, physicalWorkBudget); err != nil {
		return err
	}
	current, err := storagecommit.ObserveEntryIdentity(ctx, snapshot.namespacePath)
	if err != nil {
		return wrapFileSetAccessUnprovable(
			fmt.Errorf("recapture file-set state directory namespace: %w", err),
		)
	}
	if err := chargeStateDirPath(snapshot.namespacePath, maximumPhysicalDepth, physicalWorkBudget); err != nil {
		return err
	}
	currentMount, currentIncarnation, err := observeStateDirPlatform(ctx, snapshot.namespacePath)
	if err != nil {
		return wrapFileSetAccessUnprovable(
			fmt.Errorf("recapture file-set state directory namespace platform identity: %w", err),
		)
	}
	if current.Kind() != mutationfs.EntryKindDirectory ||
		currentMount != snapshot.namespaceMount ||
		currentIncarnation != snapshot.namespaceIncarnation ||
		!snapshot.namespaceIdentity.SameObject(current) {
		return wrapFileSetAccessUnprovable(fmt.Errorf(
			"file-set state directory namespace changed after planning",
		))
	}
	return nil
}

type stateDirCreationWitness struct {
	path     string
	identity storagecommit.EntryIdentity
}

// EnsureOwnedIncarnation creates and binds a missing StateDir from exact
// storage-produced creation evidence. It never adopts a directory that merely
// appeared after planning.
func (authority StateDirAuthority) EnsureOwnedIncarnation(ctx context.Context) (bool, error) {
	snapshot, ok := authority.snapshot()
	if !ok {
		return false, fmt.Errorf("file-set state directory authority is uninitialized")
	}
	if ctx == nil {
		return false, fmt.Errorf("file-set transaction context is required")
	}
	authority.state.creationMu.Lock()
	defer authority.state.creationMu.Unlock()

	authority.state.mu.Lock()
	currentPresent := authority.state.currentPresent
	authority.state.mu.Unlock()
	if currentPresent {
		return false, authority.Validate(ctx)
	}
	if err := validateStateDirNamespace(
		ctx,
		snapshot,
		authority.state.maximumPhysicalDepth,
		authority.state.physicalWorkBudget,
	); err != nil {
		return false, err
	}
	witness, cleanup, err := createStateDirIncarnation(
		ctx,
		snapshot.path,
		authority.state.maximumPhysicalDepth,
		authority.state.physicalWorkBudget,
	)
	if err != nil {
		return false, err
	}
	defer cleanup.Close()
	if err := authority.acceptCreationWitness(ctx, snapshot, witness); err != nil {
		return false, err
	}
	return true, nil
}

func createStateDirIncarnation(
	ctx context.Context,
	path string,
	maximumPhysicalDepth int,
	physicalWorkBudget rootedpath.PhysicalTraversalBudget,
) (stateDirCreationWitness, *storagecommit.AncestorCleanup, error) {
	probe := filepath.Join(path, ".state-dir-creation")
	if err := chargeStateDirPath(probe, maximumPhysicalDepth, physicalWorkBudget); err != nil {
		return stateDirCreationWitness{}, nil, err
	}
	cleanup := &storagecommit.AncestorCleanup{}
	if err := cleanup.PrepareParent(ctx, probe); err != nil {
		cleanup.Close()
		return stateDirCreationWitness{}, nil, err
	}
	identity, created, err := cleanup.CreatedDirectoryIdentity(path)
	if err != nil {
		cleanup.Close()
		return stateDirCreationWitness{}, nil, err
	}
	if !created {
		cleanup.Close()
		return stateDirCreationWitness{}, nil, ErrStateDirAppeared
	}
	return stateDirCreationWitness{path: path, identity: identity}, cleanup, nil
}

func (authority StateDirAuthority) acceptCreationWitness(
	ctx context.Context,
	snapshot stateDirAuthoritySnapshot,
	witness stateDirCreationWitness,
) error {
	if witness.path != snapshot.path || witness.identity.Kind() != mutationfs.EntryKindDirectory {
		return fmt.Errorf("file-set state directory creation witness is invalid")
	}
	if err := validateStateDirNamespace(
		ctx,
		snapshot,
		authority.state.maximumPhysicalDepth,
		authority.state.physicalWorkBudget,
	); err != nil {
		return err
	}
	present, observedIdentity, observedMount, observedIncarnation, err := observeCurrentStateDir(
		ctx,
		snapshot.path,
		authority.state.maximumPhysicalDepth,
		authority.state.physicalWorkBudget,
	)
	if err != nil {
		return err
	}
	if !present || !witness.identity.SameObject(observedIdentity) {
		return wrapFileSetAccessUnprovable(fmt.Errorf(
			"file-set state directory creation identity changed before acceptance",
		))
	}
	authority.state.mu.Lock()
	defer authority.state.mu.Unlock()
	if authority.state.currentPresent {
		if authority.state.currentMount != observedMount ||
			authority.state.currentIncarnation != observedIncarnation ||
			!authority.state.currentIdentity.SameObject(observedIdentity) {
			return wrapFileSetAccessUnprovable(fmt.Errorf(
				"file-set state directory identity changed after planning",
			))
		}
		return nil
	}
	authority.state.currentPresent = true
	authority.state.currentIdentity = observedIdentity
	authority.state.currentMount = observedMount
	authority.state.currentIncarnation = observedIncarnation
	return nil
}

func observeCurrentStateDir(
	ctx context.Context,
	path string,
	maximumPhysicalDepth int,
	physicalWorkBudget rootedpath.PhysicalTraversalBudget,
) (bool, storagecommit.EntryIdentity, string, string, error) {
	if ctx == nil {
		return false, storagecommit.EntryIdentity{}, "", "", fmt.Errorf("file-set transaction context is required")
	}
	if err := ctx.Err(); err != nil {
		return false, storagecommit.EntryIdentity{}, "", "", err
	}
	if err := chargeStateDirPath(path, maximumPhysicalDepth, physicalWorkBudget); err != nil {
		return false, storagecommit.EntryIdentity{}, "", "", err
	}
	observed, err := storagecommit.ObserveEntryIdentity(ctx, path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, storagecommit.EntryIdentity{}, "", "", nil
	}
	if err != nil {
		return false, storagecommit.EntryIdentity{}, "", "", wrapFileSetAccessUnprovable(
			fmt.Errorf("recapture file-set state directory identity: %w", err),
		)
	}
	if observed.Kind() != mutationfs.EntryKindDirectory {
		return false, storagecommit.EntryIdentity{}, "", "", wrapFileSetAccessUnprovable(
			fmt.Errorf("file-set state directory is not a directory"),
		)
	}
	if err := chargeStateDirPath(path, maximumPhysicalDepth, physicalWorkBudget); err != nil {
		return false, storagecommit.EntryIdentity{}, "", "", err
	}
	mount, incarnation, err := observeStateDirPlatform(ctx, path)
	if err != nil {
		return false, storagecommit.EntryIdentity{}, "", "", wrapFileSetAccessUnprovable(
			fmt.Errorf("recapture file-set state directory platform identity: %w", err),
		)
	}
	if err := ctx.Err(); err != nil {
		return false, storagecommit.EntryIdentity{}, "", "", err
	}
	return true, observed, mount, incarnation, nil
}

func chargeStateDirPath(
	path string,
	maximumPhysicalDepth int,
	physicalWorkBudget rootedpath.PhysicalTraversalBudget,
) error {
	if err := rootedpath.ChargeAbsolutePath(path, maximumPhysicalDepth, physicalWorkBudget); err != nil {
		return wrapFileSetAccessUnprovable(fmt.Errorf(
			"admit file-set state directory path work: %w",
			err,
		))
	}
	return nil
}

// RequireClear validates the retained StateDir identity before and after the
// complete file-set fence observation.
func (authority StateDirAuthority) RequireClear(ctx context.Context) error {
	if err := authority.Validate(ctx); err != nil {
		return err
	}
	snapshot, ok := authority.snapshot()
	if !ok {
		return fmt.Errorf("file-set state directory authority is uninitialized")
	}
	fenceErr := requireClearFileSetAtCanonicalPath(ctx, snapshot.path)
	if err := authority.Validate(ctx); err != nil {
		return err
	}
	return fenceErr
}
