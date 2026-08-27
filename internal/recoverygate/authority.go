package recoverygate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/isty2e/daem/internal/declaration/transaction"
	"github.com/isty2e/daem/internal/effect/journal"
	"github.com/isty2e/daem/internal/effect/mutation"
	daempaths "github.com/isty2e/daem/internal/paths"
)

// EffectAuthority preserves RecoveryDir lease/revision evidence and the
// planning-time StateDir namespace/object identity through one forward
// operation's durable or host effect phases.
type EffectAuthority struct {
	paths     daempaths.Paths
	stateDir  transaction.StateDirAuthority
	domains   []mutation.Domain
	revisions []mutation.RevisionRequest
}

// NewEffectAuthority captures the StateDir identity before any recovery
// barrier observation and constructs the complete peer mutation evidence.
func NewEffectAuthority(ctx context.Context, paths daempaths.Paths) (EffectAuthority, error) {
	stateDir, err := transaction.CaptureStateDirAuthority(ctx, paths.StateDir)
	if err != nil {
		return EffectAuthority{}, err
	}
	domains := make([]mutation.Domain, 0, 4)
	revisions := make([]mutation.RevisionRequest, 0, 4)
	stateDirAccess := mutation.AccessShared
	if !stateDir.PresentAtCapture() {
		stateDirAccess = mutation.AccessExclusive
	}
	for _, path := range []struct {
		value  string
		access mutation.AccessMode
	}{
		{value: paths.RecoveryDir, access: mutation.AccessExclusive},
		{value: paths.StateDir, access: stateDirAccess},
	} {
		for _, effect := range []mutation.PathEffect{
			mutation.PathEffectDirectoryEntry,
			mutation.PathEffectReferent,
		} {
			domain, err := mutation.NewLogicalPathDomain(mutation.LogicalPathRequest{
				Path:   path.value,
				Access: path.access,
				Effect: effect,
			})
			if err != nil {
				return EffectAuthority{}, fmt.Errorf("build recovery barrier domain: %w", err)
			}
			domains = append(domains, domain)
			if path.value != paths.StateDir {
				revisions = append(
					revisions,
					mutation.NewBoundedContentRevisionRequest(path.value, effect),
				)
			}
		}
	}
	return EffectAuthority{
		paths:     paths,
		stateDir:  stateDir,
		domains:   domains,
		revisions: revisions,
	}, nil
}

// Domains returns owned copies of the complete RecoveryDir and StateDir lease set.
func (authority EffectAuthority) Domains() []mutation.Domain {
	return append([]mutation.Domain(nil), authority.domains...)
}

// RevisionRequests returns owned RecoveryDir revision requests. StateDir
// object identity is retained separately by this authority.
func (authority EffectAuthority) RevisionRequests() []mutation.RevisionRequest {
	return append([]mutation.RevisionRequest(nil), authority.revisions...)
}

// Equal reports whether two plans preserve the same paths and StateDir
// incarnation.
func (authority EffectAuthority) Equal(other EffectAuthority) bool {
	return authority.paths.RecoveryDir == other.paths.RecoveryDir &&
		authority.paths.StateDir == other.paths.StateDir &&
		authority.stateDir.Equal(other.stateDir) &&
		len(authority.domains) == len(other.domains) &&
		len(authority.revisions) == len(other.revisions)
}

// IdentityFingerprint returns the opaque operation-local peer-barrier identity.
func (authority EffectAuthority) IdentityFingerprint() (string, error) {
	if err := authority.requireInitialized(); err != nil {
		return "", err
	}
	stateDir, err := authority.stateDir.IdentityFingerprint()
	if err != nil {
		return "", err
	}
	canonical, err := json.Marshal(struct {
		RecoveryDir string
		StateDir    string
		State       string
	}{
		RecoveryDir: authority.paths.RecoveryDir,
		StateDir:    authority.paths.StateDir,
		State:       stateDir,
	})
	if err != nil {
		return "", fmt.Errorf("fingerprint recovery effect authority: %w", err)
	}
	digest := sha256.Sum256(canonical)
	return "sha256:" + hex.EncodeToString(digest[:]), nil
}

// Validate requires the retained workspace's journal and file-set barriers to
// remain clear under the planning-time StateDir identity.
func (authority EffectAuthority) Validate(ctx context.Context) error {
	if err := authority.requireInitialized(); err != nil {
		return err
	}
	if authority.stateDir.PresentAtCapture() {
		if err := normalizeStateDirValidation(authority.stateDir.Validate(ctx)); err != nil {
			return err
		}
	}
	journalErr := journal.RequireNoInterruptedApply(ctx, authority.paths.RecoveryDir)
	if err := ctx.Err(); err != nil {
		return err
	}
	var fileSetErr error
	if authority.stateDir.PresentAtCapture() {
		fileSetErr = authority.stateDir.RequireClear(ctx)
	} else {
		fileSetErr = transaction.RequireClearFileSet(ctx, authority.paths.StateDir)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if journalErr != nil || fileSetErr != nil {
		return Combine(journalErr, fileSetErr)
	}
	return normalizeStateDirValidation(authority.stateDir.Validate(ctx))
}

func normalizeStateDirValidation(err error) error {
	if errors.Is(err, transaction.ErrStateDirAppeared) {
		return errors.Join(mutation.StaleSnapshotError{}, err)
	}
	return err
}

func (authority EffectAuthority) ensureStateDir(ctx context.Context) (bool, error) {
	if err := authority.requireInitialized(); err != nil {
		return false, err
	}
	created, err := authority.stateDir.EnsureOwnedIncarnation(ctx)
	return created, normalizeStateDirValidation(err)
}

// EnsureStateDirForEffect validates peer workflow authority and the recovery
// barrier before StateDir creation, then revalidates both after that first
// authorized visibility effect.
func (authority EffectAuthority) EnsureStateDirForEffect(
	ctx context.Context,
	validatePeer func(context.Context) error,
) (bool, error) {
	if ctx == nil {
		return false, fmt.Errorf("recovery effect context is required")
	}
	if validatePeer == nil {
		return false, fmt.Errorf("recovery effect peer validation is required")
	}
	if err := validatePeer(ctx); err != nil {
		return false, err
	}
	if err := authority.Validate(ctx); err != nil {
		return false, err
	}
	created, err := authority.ensureStateDir(ctx)
	if err != nil {
		return created, err
	}
	if err := validatePeer(ctx); err != nil {
		return created, err
	}
	if err := authority.Validate(ctx); err != nil {
		return created, err
	}
	return created, nil
}

// ValidateStateDir requires only the planning-time StateDir namespace and
// adopted directory incarnation to remain current. It is used after a workflow
// has intentionally published its own apply journal.
func (authority EffectAuthority) ValidateStateDir(ctx context.Context) error {
	if err := authority.requireInitialized(); err != nil {
		return err
	}
	return normalizeStateDirValidation(authority.stateDir.Validate(ctx))
}

// ValidateFileSetRecovery requires the journal to be clear while preserving
// the planning-time StateDir identity. It intentionally permits the known
// published file-set marker that the owning workflow is about to recover.
func (authority EffectAuthority) ValidateFileSetRecovery(ctx context.Context) error {
	if err := authority.requireInitialized(); err != nil {
		return err
	}
	if err := normalizeStateDirValidation(authority.stateDir.Validate(ctx)); err != nil {
		return err
	}
	journalErr := journal.RequireNoInterruptedApply(ctx, authority.paths.RecoveryDir)
	if err := normalizeStateDirValidation(authority.stateDir.Validate(ctx)); err != nil {
		return Combine(journalErr, err)
	}
	return journalErr
}

func (authority EffectAuthority) requireInitialized() error {
	if len(authority.domains) == 0 || len(authority.revisions) == 0 {
		return fmt.Errorf("recovery effect authority is uninitialized")
	}
	return nil
}
