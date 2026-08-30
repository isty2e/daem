package recoverygate

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"

	"github.com/isty2e/daem/internal/effect/fileset"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
)

// StateDirDescendantReservation transfers a bounded StateDir descendant
// persistence envelope from planning to later execution. Reserving performs no
// filesystem I/O and grants no mutation authority.
type StateDirDescendantReservation struct {
	mu       sync.Mutex
	stateDir StateDirAuthority
	relative rootedpath.RelativeDestination
	budget   *stateDirReservedWorkBudget
	consumed bool
}

// StateDirDescendantAuthority retains the exact StateDir root and one bounded
// descendant entry through a reserved execution envelope.
type StateDirDescendantAuthority struct {
	mu       sync.Mutex
	stateDir StateDirAuthority
	root     *rootedpath.CapturedRoot
	entry    *rootedpath.EntryAuthority
	budget   stateDirPhysicalWorkBudget
	closed   bool
}

// StateDirExecutionAuthority consumes one pre-reserved StateDir validation
// and creation envelope. It grants no descendant mutation authority.
type StateDirExecutionAuthority struct {
	stateDir StateDirAuthority
	budget   stateDirPhysicalWorkBudget
}

// StateDirOperationReservation atomically transfers independent StateDir
// barrier and descendant-persistence budgets from one operation-wide limit.
type StateDirOperationReservation struct {
	mu             sync.Mutex
	execution      *StateDirExecutionAuthority
	descendant     *StateDirDescendantReservation
	descendantUsed bool
}

func (authority StateDirAuthority) planDescendantAuthority(
	path string,
	futureValidations int,
	fileCommits int,
) (rootedpath.RelativeDestination, int, error) {
	snapshot, ok := authority.snapshot()
	if !ok {
		return rootedpath.RelativeDestination{}, 0, fmt.Errorf("file-set state directory authority is uninitialized")
	}
	if futureValidations < 0 {
		return rootedpath.RelativeDestination{}, 0, fmt.Errorf("StateDir descendant future validation count must not be negative")
	}
	if fileCommits < 0 {
		return rootedpath.RelativeDestination{}, 0, fmt.Errorf("StateDir descendant file commit count must not be negative")
	}
	selected, err := filepath.Abs(path)
	if err != nil {
		return rootedpath.RelativeDestination{}, 0, fmt.Errorf("resolve StateDir descendant path: %w", err)
	}
	selected = filepath.Clean(selected)
	relativeValue, err := filepath.Rel(snapshot.selectedPath, selected)
	if err != nil {
		return rootedpath.RelativeDestination{}, 0, fmt.Errorf("derive StateDir descendant path: %w", err)
	}
	if relativeValue == "." || relativeValue == ".." ||
		strings.HasPrefix(relativeValue, ".."+string(filepath.Separator)) {
		return rootedpath.RelativeDestination{}, 0, fmt.Errorf("StateDir descendant %q must be below %q", selected, snapshot.selectedPath)
	}
	absolute := filepath.Join(snapshot.path, relativeValue)
	relative, err := rootedpath.NewRelativeDestination(filepath.ToSlash(relativeValue))
	if err != nil {
		return rootedpath.RelativeDestination{}, 0, err
	}

	rootDepth, err := stateDirAbsolutePathWork(snapshot.path, authority.state.maximumPhysicalDepth)
	if err != nil {
		return rootedpath.RelativeDestination{}, 0, err
	}
	entryDepth, err := stateDirAbsolutePathWork(absolute, authority.state.maximumPhysicalDepth)
	if err != nil {
		return rootedpath.RelativeDestination{}, 0, err
	}
	validationWork, err := stateDirValidationPathWork(snapshot, authority.state.maximumPhysicalDepth)
	if err != nil {
		return rootedpath.RelativeDestination{}, 0, err
	}
	perValidation, err := checkedStateDirWorkAdd(rootDepth, validationWork)
	if err != nil {
		return rootedpath.RelativeDestination{}, 0, err
	}
	// Binding performs StateDir validation before and after root capture, then
	// one initial root-plus-StateDir validation after exact entry binding.
	bindingWork, err := checkedStateDirWorkMultiply(perValidation, 3)
	if err != nil {
		return rootedpath.RelativeDestination{}, 0, err
	}
	futureValidationWork, err := checkedStateDirWorkMultiply(perValidation, futureValidations)
	if err != nil {
		return rootedpath.RelativeDestination{}, 0, err
	}
	// One rooted statefile commit acquires the retained root with two checks,
	// then opens the destination once for identity and once for create/replace.
	perCommitRootWork, err := checkedStateDirWorkMultiply(rootDepth, 2)
	if err != nil {
		return rootedpath.RelativeDestination{}, 0, err
	}
	perCommitEntryWork, err := checkedStateDirWorkMultiply(entryDepth, 2)
	if err != nil {
		return rootedpath.RelativeDestination{}, 0, err
	}
	perCommitWork, err := checkedStateDirWorkAdd(perCommitRootWork, perCommitEntryWork)
	if err != nil {
		return rootedpath.RelativeDestination{}, 0, err
	}
	commitWork, err := checkedStateDirWorkMultiply(perCommitWork, fileCommits)
	if err != nil {
		return rootedpath.RelativeDestination{}, 0, err
	}
	total, err := checkedStateDirWorkAdd(bindingWork, futureValidationWork)
	if err != nil {
		return rootedpath.RelativeDestination{}, 0, err
	}
	total, err = checkedStateDirWorkAdd(total, commitWork)
	if err != nil {
		return rootedpath.RelativeDestination{}, 0, err
	}
	return relative, total, nil
}

func newStateDirDescendantReservation(
	authority StateDirAuthority,
	relative rootedpath.RelativeDestination,
	total int,
) *StateDirDescendantReservation {
	return &StateDirDescendantReservation{
		stateDir: authority,
		relative: relative,
		budget: newStateDirReservedWorkBudget(stateDirPhysicalWork{
			pathComponents: total,
		}),
	}
}

// ReserveOperation atomically reserves all predictable StateDir validation,
// file-set census, optional first-incarnation creation, and one descendant
// persistence envelope.
func (authority StateDirAuthority) ReserveOperation(
	stateValidations int,
	fileSetCensuses int,
	createIfAbsent bool,
	descendantPath string,
	descendantValidations int,
	descendantFileCommits int,
) (*StateDirOperationReservation, error) {
	snapshot, ok := authority.snapshot()
	if !ok {
		return nil, fmt.Errorf("file-set state directory authority is uninitialized")
	}
	if stateValidations < 0 {
		return nil, fmt.Errorf("StateDir validation count must not be negative")
	}
	if fileSetCensuses < 0 {
		return nil, fmt.Errorf("StateDir file-set census count must not be negative")
	}
	validationWork, err := stateDirValidationPathWork(snapshot, authority.state.maximumPhysicalDepth)
	if err != nil {
		return nil, err
	}
	statePathWork, err := checkedStateDirWorkMultiply(validationWork, stateValidations)
	if err != nil {
		return nil, err
	}
	stateWork := stateDirPhysicalWork{pathComponents: statePathWork}
	if createIfAbsent && !snapshot.plannedPresent {
		creationWork, creationErr := stateDirCreationPathWork(snapshot, authority.state.maximumPhysicalDepth)
		if creationErr != nil {
			return nil, creationErr
		}
		stateWork, err = stateWork.add(stateDirPhysicalWork{pathComponents: creationWork})
		if err != nil {
			return nil, err
		}
	}
	if fileSetCensuses != 0 {
		census, censusErr := fileset.MaximumFenceCensusWork(
			snapshot.path,
			authority.state.maximumPhysicalDepth,
		)
		if censusErr != nil {
			return nil, censusErr
		}
		censusWork, censusErr := stateDirPhysicalWork{
			pathComponents: census.PathComponents,
			entries:        census.Entries,
			bytes:          census.Bytes,
		}.multiply(fileSetCensuses)
		if censusErr != nil {
			return nil, censusErr
		}
		stateWork, err = stateWork.add(censusWork)
		if err != nil {
			return nil, err
		}
	}
	var (
		relative       rootedpath.RelativeDestination
		descendantWork int
	)
	if descendantPath == "" {
		if descendantValidations != 0 || descendantFileCommits != 0 {
			return nil, fmt.Errorf("StateDir descendant path is required for reserved persistence work")
		}
	} else {
		relative, descendantWork, err = authority.planDescendantAuthority(
			descendantPath,
			descendantValidations,
			descendantFileCommits,
		)
		if err != nil {
			return nil, err
		}
	}
	total, err := stateWork.add(stateDirPhysicalWork{pathComponents: descendantWork})
	if err != nil {
		return nil, err
	}
	if err := authority.state.physicalWorkBudget.AdmitPhysicalWork(
		total.pathComponents,
		total.entries,
		total.bytes,
	); err != nil {
		return nil, fileset.WrapFileSetAccessUnprovable(fmt.Errorf(
			"reserve complete file-set StateDir operation physical work: %w",
			err,
		))
	}
	reservation := &StateDirOperationReservation{
		execution: &StateDirExecutionAuthority{
			stateDir: authority,
			budget:   newStateDirReservedWorkBudget(stateWork),
		},
	}
	if descendantPath != "" {
		reservation.descendant = newStateDirDescendantReservation(authority, relative, descendantWork)
	}
	return reservation, nil
}

// Execution returns the reserved StateDir validation and creation authority.
func (reservation *StateDirOperationReservation) Execution() *StateDirExecutionAuthority {
	if reservation == nil {
		return nil
	}
	return reservation.execution
}

// TakeDescendant transfers the one reserved descendant persistence envelope.
func (reservation *StateDirOperationReservation) TakeDescendant() (*StateDirDescendantReservation, error) {
	if reservation == nil {
		return nil, fmt.Errorf("StateDir operation reservation is required")
	}
	reservation.mu.Lock()
	defer reservation.mu.Unlock()
	if reservation.descendantUsed || reservation.descendant == nil {
		return nil, fmt.Errorf("StateDir descendant reservation was already transferred")
	}
	reservation.descendantUsed = true
	return reservation.descendant, nil
}

// PresentAtCapture reports the planning-time StateDir state.
func (authority *StateDirExecutionAuthority) PresentAtCapture() bool {
	return authority != nil && authority.stateDir.PresentAtCapture()
}

// Validate consumes one reserved StateDir identity validation.
func (authority *StateDirExecutionAuthority) Validate(ctx context.Context) error {
	if authority == nil || authority.budget == nil {
		return fmt.Errorf("reserved StateDir execution authority is required")
	}
	return authority.stateDir.validateWithBudget(ctx, authority.budget)
}

// RequireClear validates identity around one complete file-set fence census.
func (authority *StateDirExecutionAuthority) RequireClear(ctx context.Context) error {
	if authority == nil || authority.budget == nil {
		return fmt.Errorf("reserved StateDir execution authority is required")
	}
	return authority.stateDir.requireClearWithBudget(ctx, authority.budget)
}

// EnsureOwnedIncarnation consumes the reserved first-incarnation envelope.
func (authority *StateDirExecutionAuthority) EnsureOwnedIncarnation(ctx context.Context) (bool, error) {
	if authority == nil || authority.budget == nil {
		return false, fmt.Errorf("reserved StateDir execution authority is required")
	}
	return authority.stateDir.ensureOwnedIncarnationWithBudget(ctx, authority.budget)
}

// Bind establishes the reserved retained-root and exact-entry authority after
// the owning workflow has created or confirmed the StateDir incarnation.
func (reservation *StateDirDescendantReservation) Bind(
	ctx context.Context,
) (*StateDirDescendantAuthority, error) {
	if reservation == nil || reservation.budget == nil {
		return nil, fmt.Errorf("StateDir descendant reservation is required")
	}
	reservation.mu.Lock()
	if reservation.consumed {
		reservation.mu.Unlock()
		return nil, fmt.Errorf("StateDir descendant reservation was already consumed")
	}
	reservation.consumed = true
	reservation.mu.Unlock()

	if err := reservation.stateDir.validateWithBudget(ctx, reservation.budget); err != nil {
		return nil, err
	}
	snapshot, ok := reservation.stateDir.snapshot()
	if !ok {
		return nil, fmt.Errorf("file-set state directory authority is uninitialized")
	}
	root, err := rootedpath.CaptureCanonicalRootNoFollowBounded(
		snapshot.path,
		reservation.stateDir.state.maximumPhysicalDepth,
		reservation.budget,
	)
	if err != nil {
		return nil, fileset.WrapFileSetAccessUnprovable(fmt.Errorf(
			"capture canonical file-set StateDir root: %w",
			err,
		))
	}
	if err := reservation.stateDir.validateWithBudget(ctx, reservation.budget); err != nil {
		return nil, errors.Join(err, root.Close())
	}
	rootAuthority, err := root.AuthorityBounded(reservation.budget)
	if err != nil {
		return nil, errors.Join(err, root.Close())
	}
	destination, err := rootAuthority.Bind(reservation.relative)
	if err != nil {
		return nil, errors.Join(err, root.Close())
	}
	entry, err := rootedpath.BindCapturedEntryAuthorityBounded(
		root,
		destination,
		reservation.stateDir.state.maximumPhysicalDepth,
		reservation.budget,
	)
	if err != nil {
		return nil, errors.Join(err, root.Close())
	}
	bound := &StateDirDescendantAuthority{
		stateDir: reservation.stateDir,
		root:     root,
		entry:    entry,
		budget:   reservation.budget,
	}
	if err := bound.Validate(ctx); err != nil {
		return nil, errors.Join(err, bound.Close())
	}
	return bound, nil
}

// Entry returns the exact bounded descendant entry authority.
func (authority *StateDirDescendantAuthority) Entry() *rootedpath.EntryAuthority {
	if authority == nil {
		return nil
	}
	authority.mu.Lock()
	defer authority.mu.Unlock()
	if authority.closed {
		return nil
	}
	return authority.entry
}

// Validate checks both the retained root witness and the current StateDir path
// authority through the transferred execution budget.
func (authority *StateDirDescendantAuthority) Validate(ctx context.Context) error {
	if authority == nil {
		return fmt.Errorf("StateDir descendant authority is uninitialized")
	}
	if ctx == nil {
		return fmt.Errorf("file-set transaction context is required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	authority.mu.Lock()
	defer authority.mu.Unlock()
	if authority.closed || authority.root == nil || authority.entry == nil || authority.budget == nil {
		return fmt.Errorf("StateDir descendant authority is uninitialized")
	}
	_, rootErr := authority.root.AuthorityBounded(authority.budget)
	stateDirErr := authority.stateDir.validateWithBudget(ctx, authority.budget)
	return errors.Join(rootErr, stateDirErr)
}

// Close releases the exact entry and retained StateDir root.
func (authority *StateDirDescendantAuthority) Close() error {
	if authority == nil {
		return nil
	}
	authority.mu.Lock()
	defer authority.mu.Unlock()
	if authority.closed {
		return nil
	}
	authority.closed = true
	entryErr := authority.entry.Close()
	rootErr := authority.root.Close()
	authority.entry = nil
	authority.root = nil
	return errors.Join(entryErr, rootErr)
}

type stateDirPathWorkCounter struct {
	count int
}

func (counter *stateDirPathWorkCounter) AdmitPathComponents(count int) error {
	next, err := checkedStateDirWorkAdd(counter.count, count)
	if err != nil {
		return err
	}
	counter.count = next
	return nil
}

type stateDirReservedWorkBudget struct {
	mu        sync.Mutex
	remaining stateDirPhysicalWork
}

func newStateDirReservedWorkBudget(work stateDirPhysicalWork) *stateDirReservedWorkBudget {
	return &stateDirReservedWorkBudget{remaining: work}
}

func (budget *stateDirReservedWorkBudget) AdmitPathComponents(count int) error {
	return budget.AdmitPhysicalWork(count, 0, 0)
}

func (budget *stateDirReservedWorkBudget) AdmitPhysicalWork(
	pathComponents int,
	entries int,
	bytes int64,
) error {
	if budget == nil {
		return fmt.Errorf("reserved StateDir physical work budget is required")
	}
	budget.mu.Lock()
	defer budget.mu.Unlock()
	if pathComponents < 0 || entries < 0 || bytes < 0 ||
		pathComponents > budget.remaining.pathComponents ||
		entries > budget.remaining.entries || bytes > budget.remaining.bytes {
		return fmt.Errorf("reserved StateDir physical work is exhausted")
	}
	budget.remaining.pathComponents -= pathComponents
	budget.remaining.entries -= entries
	budget.remaining.bytes -= bytes
	return nil
}

func stateDirAbsolutePathWork(path string, maximumPhysicalDepth int) (int, error) {
	counter := &stateDirPathWorkCounter{}
	if err := rootedpath.ChargeAbsolutePath(path, maximumPhysicalDepth, counter); err != nil {
		return 0, err
	}
	return counter.count, nil
}

func stateDirValidationPathWork(
	snapshot stateDirAuthoritySnapshot,
	maximumPhysicalDepth int,
) (int, error) {
	namespace, err := stateDirAbsolutePathWork(snapshot.namespacePath, maximumPhysicalDepth)
	if err != nil {
		return 0, err
	}
	stateDir, err := stateDirAbsolutePathWork(snapshot.path, maximumPhysicalDepth)
	if err != nil {
		return 0, err
	}
	namespace, err = checkedStateDirWorkMultiply(namespace, 2)
	if err != nil {
		return 0, err
	}
	stateDir, err = checkedStateDirWorkMultiply(stateDir, 2)
	if err != nil {
		return 0, err
	}
	return checkedStateDirWorkAdd(namespace, stateDir)
}

func stateDirCreationPathWork(
	snapshot stateDirAuthoritySnapshot,
	maximumPhysicalDepth int,
) (int, error) {
	namespace, err := stateDirAbsolutePathWork(snapshot.namespacePath, maximumPhysicalDepth)
	if err != nil {
		return 0, err
	}
	stateDir, err := stateDirAbsolutePathWork(snapshot.path, maximumPhysicalDepth)
	if err != nil {
		return 0, err
	}
	probe, err := stateDirAbsolutePathWork(
		filepath.Join(snapshot.path, ".state-dir-creation"),
		maximumPhysicalDepth,
	)
	if err != nil {
		return 0, err
	}
	namespace, err = checkedStateDirWorkMultiply(namespace, 4)
	if err != nil {
		return 0, err
	}
	stateDir, err = checkedStateDirWorkMultiply(stateDir, 2)
	if err != nil {
		return 0, err
	}
	total, err := checkedStateDirWorkAdd(namespace, stateDir)
	if err != nil {
		return 0, err
	}
	return checkedStateDirWorkAdd(total, probe)
}

func checkedStateDirWorkAdd(left int, right int) (int, error) {
	if left < 0 || right < 0 || left > int(^uint(0)>>1)-right {
		return 0, fmt.Errorf("StateDir descendant path work overflows")
	}
	return left + right, nil
}

func checkedStateDirWorkMultiply(value int, count int) (int, error) {
	if value < 0 || count < 0 {
		return 0, fmt.Errorf("StateDir descendant path work must not be negative")
	}
	if value != 0 && count > int(^uint(0)>>1)/value {
		return 0, fmt.Errorf("StateDir descendant path work overflows")
	}
	return value * count, nil
}
