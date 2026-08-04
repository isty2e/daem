package source

import (
	"errors"
	"fmt"
	"sync"
)

const (
	defaultRootListingRoots          int64 = 1_024
	defaultRootListingEntries        int64 = 100_000
	defaultRootListingNameBytes      int64 = 32 << 20
	defaultRootListingEntryNameBytes int64 = 4_096
)

// ErrRootListingLimitExceeded classifies bounded source-root enumeration failures.
var ErrRootListingLimitExceeded = errors.New("source root enumeration limit exceeded")

// rootListingLimits is the immutable backend-neutral source-root enumeration policy.
type rootListingLimits struct {
	maximumRoots          int64
	maximumEntries        int64
	maximumNameBytes      int64
	maximumEntryNameBytes int64
}

// newRootListingLimits constructs a positive source-root enumeration policy
// that is no looser than the package defaults.
func newRootListingLimits(
	maximumRoots int64,
	maximumEntries int64,
	maximumNameBytes int64,
	maximumEntryNameBytes int64,
) (rootListingLimits, error) {
	limits := rootListingLimits{
		maximumRoots:          maximumRoots,
		maximumEntries:        maximumEntries,
		maximumNameBytes:      maximumNameBytes,
		maximumEntryNameBytes: maximumEntryNameBytes,
	}
	if err := limits.validate(); err != nil {
		return rootListingLimits{}, err
	}
	return limits, nil
}

func defaultRootListingLimits() rootListingLimits {
	return rootListingLimits{
		maximumRoots:          defaultRootListingRoots,
		maximumEntries:        defaultRootListingEntries,
		maximumNameBytes:      defaultRootListingNameBytes,
		maximumEntryNameBytes: defaultRootListingEntryNameBytes,
	}
}

func (limits rootListingLimits) validate() error {
	if limits.maximumRoots <= 0 || limits.maximumEntries <= 0 ||
		limits.maximumNameBytes <= 0 || limits.maximumEntryNameBytes <= 0 {
		return fmt.Errorf("source root listing limits must be positive")
	}
	if limits.maximumEntryNameBytes > limits.maximumNameBytes {
		return fmt.Errorf("source root entry-name limit exceeds cumulative name-byte limit")
	}
	defaults := defaultRootListingLimits()
	if limits.maximumRoots > defaults.maximumRoots ||
		limits.maximumEntries > defaults.maximumEntries ||
		limits.maximumNameBytes > defaults.maximumNameBytes ||
		limits.maximumEntryNameBytes > defaults.maximumEntryNameBytes {
		return fmt.Errorf("source root listing limits must not exceed package defaults")
	}
	return nil
}

// RootListingLimitError reports exhaustion without source-controlled identity.
// The error intentionally omits the first failing source and dimension so
// concurrent scheduling cannot alter the authoritative failure identity.
type RootListingLimitError struct {
	limits rootListingLimits
}

func (err *RootListingLimitError) Error() string {
	if err == nil {
		return ErrRootListingLimitExceeded.Error()
	}
	return fmt.Sprintf(
		"%s (roots=%d entries=%d name_bytes=%d entry_name_bytes=%d)",
		ErrRootListingLimitExceeded,
		err.limits.maximumRoots,
		err.limits.maximumEntries,
		err.limits.maximumNameBytes,
		err.limits.maximumEntryNameBytes,
	)
}

func (err *RootListingLimitError) Unwrap() error { return ErrRootListingLimitExceeded }

// RootListingBudget accounts for one source-root listing phase. It is safe to
// share across concurrent backend operations.
type RootListingBudget struct {
	mutex     sync.Mutex
	limits    rootListingLimits
	entries   int64
	nameBytes int64
	exhausted *RootListingLimitError
}

// NewRootListingBudget constructs one budget using the package defaults.
func NewRootListingBudget() *RootListingBudget {
	return &RootListingBudget{limits: defaultRootListingLimits()}
}

func newRootListingBudgetWithLimits(limits rootListingLimits) (*RootListingBudget, error) {
	if err := limits.validate(); err != nil {
		return nil, err
	}
	return &RootListingBudget{limits: limits}, nil
}

// CheckRootCount validates the declaration-level root count without consuming
// entry budget. Callers perform this check before backend work begins.
func (budget *RootListingBudget) CheckRootCount(count int) error {
	if budget == nil {
		return fmt.Errorf("source root listing budget is required")
	}
	if count < 0 {
		return fmt.Errorf("source root listing count must not be negative")
	}
	budget.mutex.Lock()
	defer budget.mutex.Unlock()
	if budget.exhausted != nil {
		return budget.exhausted
	}
	if int64(count) > budget.limits.maximumRoots {
		return budget.exhaustLocked()
	}
	return nil
}

// AdmitEntryName accounts for one observed direct entry before it is retained.
func (budget *RootListingBudget) AdmitEntryName(nameBytes int) error {
	if budget == nil {
		return fmt.Errorf("source root listing budget is required")
	}
	if nameBytes < 0 {
		return fmt.Errorf("source root entry-name byte count must not be negative")
	}
	budget.mutex.Lock()
	defer budget.mutex.Unlock()
	if budget.exhausted != nil {
		return budget.exhausted
	}
	observedNameBytes := int64(nameBytes)
	if observedNameBytes > budget.limits.maximumEntryNameBytes ||
		budget.entries == budget.limits.maximumEntries ||
		observedNameBytes > budget.limits.maximumNameBytes-budget.nameBytes {
		return budget.exhaustLocked()
	}
	budget.entries++
	budget.nameBytes += observedNameBytes
	return nil
}

// Err returns the stable operation-wide exhaustion, when any.
func (budget *RootListingBudget) Err() error {
	if budget == nil {
		return fmt.Errorf("source root listing budget is required")
	}
	budget.mutex.Lock()
	defer budget.mutex.Unlock()
	if budget.exhausted == nil {
		return nil
	}
	return budget.exhausted
}

// MaximumEntryNameBytes returns the immutable per-entry ceiling.
func (budget *RootListingBudget) MaximumEntryNameBytes() int64 {
	if budget == nil {
		return 0
	}
	budget.mutex.Lock()
	defer budget.mutex.Unlock()
	return budget.limits.maximumEntryNameBytes
}

func (budget *RootListingBudget) exhaustLocked() error {
	if budget.exhausted == nil {
		budget.exhausted = &RootListingLimitError{limits: budget.limits}
	}
	return budget.exhausted
}
