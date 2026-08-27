package recoverygate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

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

// ForwardEffectPlan is the complete predictable StateDir work envelope for one
// forward operation before its first provider or host effect.
type ForwardEffectPlan struct {
	EnsureCalls             int
	BarrierValidationCalls  int
	StateDirValidationCalls int
	DescendantPath          string
	DescendantValidations   int
	DescendantFileCommits   int
}

// ForwardEffectAuthority consumes one atomically reserved forward-operation
// StateDir envelope and transfers its one descendant persistence reservation.
type ForwardEffectAuthority struct {
	mu                     sync.Mutex
	authority              EffectAuthority
	stateDir               *transaction.StateDirExecutionAuthority
	descendant             *transaction.StateDirDescendantReservation
	remainingEnsures       int
	remainingBarriers      int
	remainingStateDirCalls int
	descendantTaken        bool
}

type stateDirBarrierAuthority interface {
	PresentAtCapture() bool
	Validate(context.Context) error
	RequireClear(context.Context) error
	EnsureOwnedIncarnation(context.Context) (bool, error)
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
	return validateBarrier(ctx, authority.paths, authority.stateDir)
}

func validateBarrier(
	ctx context.Context,
	paths daempaths.Paths,
	stateDir stateDirBarrierAuthority,
) error {
	if stateDir.PresentAtCapture() {
		if err := normalizeStateDirValidation(stateDir.Validate(ctx)); err != nil {
			return err
		}
	}
	journalErr := journal.RequireNoInterruptedApply(ctx, paths.RecoveryDir)
	if err := ctx.Err(); err != nil {
		return err
	}
	fileSetErr := stateDir.RequireClear(ctx)
	if err := ctx.Err(); err != nil {
		return err
	}
	if errors.Is(fileSetErr, transaction.ErrStateDirAppeared) {
		stateErr := normalizeStateDirValidation(fileSetErr)
		if journalErr != nil {
			return errors.Join(journalErr, stateErr)
		}
		return stateErr
	}
	if journalErr != nil || fileSetErr != nil {
		return Combine(journalErr, fileSetErr)
	}
	return normalizeStateDirValidation(stateDir.Validate(ctx))
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

// ReserveForwardEffects atomically reserves all predictable barrier,
// first-incarnation, and descendant persistence work before forward effects.
func (authority EffectAuthority) ReserveForwardEffects(
	plan ForwardEffectPlan,
) (*ForwardEffectAuthority, error) {
	if err := authority.requireInitialized(); err != nil {
		return nil, err
	}
	stateValidations, createIfAbsent, err := forwardStateDirValidationPlan(
		authority.stateDir.PresentAtCapture(),
		plan,
	)
	if err != nil {
		return nil, err
	}
	fileSetCensuses, err := checkedForwardCount(plan.EnsureCalls, 2)
	if err != nil {
		return nil, err
	}
	fileSetCensuses, err = checkedForwardAdd(fileSetCensuses, plan.BarrierValidationCalls)
	if err != nil {
		return nil, err
	}
	operation, err := authority.stateDir.ReserveOperation(
		stateValidations,
		fileSetCensuses,
		createIfAbsent,
		plan.DescendantPath,
		plan.DescendantValidations,
		plan.DescendantFileCommits,
	)
	if err != nil {
		return nil, err
	}
	var descendant *transaction.StateDirDescendantReservation
	if plan.DescendantPath != "" {
		descendant, err = operation.TakeDescendant()
		if err != nil {
			return nil, err
		}
	}
	return &ForwardEffectAuthority{
		authority:              authority,
		stateDir:               operation.Execution(),
		descendant:             descendant,
		remainingEnsures:       plan.EnsureCalls,
		remainingBarriers:      plan.BarrierValidationCalls,
		remainingStateDirCalls: plan.StateDirValidationCalls,
	}, nil
}

// TakeDescendant transfers the one statefile persistence reservation.
func (authority *ForwardEffectAuthority) TakeDescendant() (*transaction.StateDirDescendantReservation, error) {
	if authority == nil {
		return nil, fmt.Errorf("forward recovery effect authority is required")
	}
	authority.mu.Lock()
	defer authority.mu.Unlock()
	if authority.descendantTaken || authority.descendant == nil {
		return nil, fmt.Errorf("forward StateDir descendant reservation was already transferred")
	}
	authority.descendantTaken = true
	return authority.descendant, nil
}

// Validate consumes one reserved complete journal and file-set barrier check.
func (authority *ForwardEffectAuthority) Validate(ctx context.Context) error {
	if authority == nil || authority.stateDir == nil {
		return fmt.Errorf("forward recovery effect authority is required")
	}
	if err := authority.consume(&authority.remainingBarriers, "recovery barrier validation"); err != nil {
		return err
	}
	return validateBarrier(ctx, authority.authority.paths, authority.stateDir)
}

// ValidateStateDir consumes one reserved StateDir-only identity check.
func (authority *ForwardEffectAuthority) ValidateStateDir(ctx context.Context) error {
	if authority == nil || authority.stateDir == nil {
		return fmt.Errorf("forward recovery effect authority is required")
	}
	if err := authority.consume(&authority.remainingStateDirCalls, "StateDir validation"); err != nil {
		return err
	}
	return normalizeStateDirValidation(authority.stateDir.Validate(ctx))
}

// EnsureStateDirForEffect consumes one reserved first-effect barrier envelope.
func (authority *ForwardEffectAuthority) EnsureStateDirForEffect(
	ctx context.Context,
	validatePeer func(context.Context) error,
) (bool, error) {
	if authority == nil || authority.stateDir == nil {
		return false, fmt.Errorf("forward recovery effect authority is required")
	}
	if ctx == nil {
		return false, fmt.Errorf("recovery effect context is required")
	}
	if validatePeer == nil {
		return false, fmt.Errorf("recovery effect peer validation is required")
	}
	if err := authority.consume(&authority.remainingEnsures, "StateDir effect establishment"); err != nil {
		return false, err
	}
	if err := validatePeer(ctx); err != nil {
		return false, err
	}
	if err := validateBarrier(ctx, authority.authority.paths, authority.stateDir); err != nil {
		return false, err
	}
	created, err := authority.stateDir.EnsureOwnedIncarnation(ctx)
	if err != nil {
		return created, normalizeStateDirValidation(err)
	}
	if err := validatePeer(ctx); err != nil {
		return created, err
	}
	if err := validateBarrier(ctx, authority.authority.paths, authority.stateDir); err != nil {
		return created, err
	}
	return created, nil
}

func (authority *ForwardEffectAuthority) consume(remaining *int, label string) error {
	authority.mu.Lock()
	defer authority.mu.Unlock()
	if *remaining == 0 {
		return fmt.Errorf("%s exceeded the reserved forward plan", label)
	}
	(*remaining)--
	return nil
}

func forwardStateDirValidationPlan(
	presentAtCapture bool,
	plan ForwardEffectPlan,
) (int, bool, error) {
	for _, value := range []struct {
		label string
		count int
	}{
		{label: "ensure call", count: plan.EnsureCalls},
		{label: "barrier validation", count: plan.BarrierValidationCalls},
		{label: "StateDir validation", count: plan.StateDirValidationCalls},
		{label: "descendant validation", count: plan.DescendantValidations},
		{label: "descendant file commit", count: plan.DescendantFileCommits},
	} {
		if value.count < 0 {
			return 0, false, fmt.Errorf("forward %s count must not be negative", value.label)
		}
	}
	barrierValidationMultiplier := 3
	if presentAtCapture {
		barrierValidationMultiplier = 4
	}
	validations, err := checkedForwardCount(
		plan.BarrierValidationCalls,
		barrierValidationMultiplier,
	)
	if err != nil {
		return 0, false, err
	}
	validations, err = checkedForwardAdd(validations, plan.StateDirValidationCalls)
	if err != nil {
		return 0, false, err
	}
	createIfAbsent := !presentAtCapture && plan.EnsureCalls != 0
	if plan.EnsureCalls != 0 {
		ensureValidations := 0
		if presentAtCapture {
			ensureValidations, err = checkedForwardCount(plan.EnsureCalls, 9)
		} else {
			// Planning-time absence retains the reserved authority through both
			// barrier censuses. The first ensure consumes three validations before
			// and three after creation; later ensures add one current-incarnation
			// check between the same two three-validation barrier envelopes.
			ensureValidations = 6
			if plan.EnsureCalls > 1 {
				remaining, multiplyErr := checkedForwardCount(plan.EnsureCalls-1, 7)
				if multiplyErr != nil {
					return 0, false, multiplyErr
				}
				ensureValidations, err = checkedForwardAdd(ensureValidations, remaining)
			}
		}
		if err != nil {
			return 0, false, err
		}
		validations, err = checkedForwardAdd(validations, ensureValidations)
		if err != nil {
			return 0, false, err
		}
	}
	return validations, createIfAbsent, nil
}

func checkedForwardAdd(left int, right int) (int, error) {
	if left < 0 || right < 0 || left > int(^uint(0)>>1)-right {
		return 0, fmt.Errorf("forward StateDir work count overflows")
	}
	return left + right, nil
}

func checkedForwardCount(value int, multiplier int) (int, error) {
	if value < 0 || multiplier < 0 || value != 0 && multiplier > int(^uint(0)>>1)/value {
		return 0, fmt.Errorf("forward StateDir work count overflows")
	}
	return value * multiplier, nil
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
