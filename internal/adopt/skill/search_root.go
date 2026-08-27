package skill

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/supply/artifact/access"
)

const (
	maximumSearchRootEntries        = 100_000
	maximumSearchRootNameBytes      = int64(32 << 20)
	maximumSearchRootEntryNameBytes = int64(4_096)
)

// ErrSearchRootLimitExceeded classifies bounded Skill search-root enumeration
// exhaustion.
var ErrSearchRootLimitExceeded = errors.New("skill search-root enumeration limit exceeded")

type searchRootLimits struct {
	maximumEntries        int
	maximumNameBytes      int64
	maximumEntryNameBytes int64
}

func newSearchRootLimits(
	maximumEntries int,
	maximumNameBytes int64,
	maximumEntryNameBytes int64,
) (searchRootLimits, error) {
	limits := searchRootLimits{
		maximumEntries:        maximumEntries,
		maximumNameBytes:      maximumNameBytes,
		maximumEntryNameBytes: maximumEntryNameBytes,
	}
	if err := limits.validate(); err != nil {
		return searchRootLimits{}, err
	}
	return limits, nil
}

func defaultSearchRootLimits() searchRootLimits {
	return searchRootLimits{
		maximumEntries:        maximumSearchRootEntries,
		maximumNameBytes:      maximumSearchRootNameBytes,
		maximumEntryNameBytes: maximumSearchRootEntryNameBytes,
	}
}

func (limits searchRootLimits) validate() error {
	if limits.maximumEntries <= 0 || limits.maximumNameBytes <= 0 || limits.maximumEntryNameBytes <= 0 {
		return fmt.Errorf("skill search-root limits must be positive")
	}
	if limits.maximumEntryNameBytes > limits.maximumNameBytes {
		return fmt.Errorf("skill search-root entry-name limit exceeds aggregate name-byte limit")
	}
	defaults := defaultSearchRootLimits()
	if limits.maximumEntries > defaults.maximumEntries ||
		limits.maximumNameBytes > defaults.maximumNameBytes ||
		limits.maximumEntryNameBytes > defaults.maximumEntryNameBytes {
		return fmt.Errorf("skill search-root limits must not exceed package defaults")
	}
	return nil
}

type searchRootLimitError struct {
	limits searchRootLimits
}

func (err *searchRootLimitError) Error() string {
	if err == nil {
		return ErrSearchRootLimitExceeded.Error()
	}
	return fmt.Sprintf(
		"%s (entries=%d name_bytes=%d entry_name_bytes=%d)",
		ErrSearchRootLimitExceeded,
		err.limits.maximumEntries,
		err.limits.maximumNameBytes,
		err.limits.maximumEntryNameBytes,
	)
}

func (err *searchRootLimitError) Unwrap() error { return ErrSearchRootLimitExceeded }

type searchRootBudget struct {
	limits    searchRootLimits
	entries   int
	nameBytes int64
	exhausted *searchRootLimitError
}

func newSearchRootBudget(limits searchRootLimits) (*searchRootBudget, error) {
	if err := limits.validate(); err != nil {
		return nil, err
	}
	return &searchRootBudget{limits: limits}, nil
}

func (budget *searchRootBudget) admitName(nameBytes int) error {
	if budget == nil {
		return fmt.Errorf("skill search-root budget is required")
	}
	if nameBytes < 0 {
		return fmt.Errorf("skill search-root entry-name byte count must not be negative")
	}
	if budget.exhausted != nil {
		return budget.exhausted
	}
	observedNameBytes := int64(nameBytes)
	if observedNameBytes > budget.limits.maximumEntryNameBytes ||
		budget.entries == budget.limits.maximumEntries ||
		observedNameBytes > budget.limits.maximumNameBytes-budget.nameBytes {
		budget.exhausted = &searchRootLimitError{limits: budget.limits}
		return budget.exhausted
	}
	budget.entries++
	budget.nameBytes += observedNameBytes
	return nil
}

type searchRootObserver func(
	context.Context,
	string,
	func(string) error,
) error

// SearchRootCache owns one candidate-collection pass's bounded, sorted Skill
// search-root listings. It is intentionally not safe for concurrent use.
type SearchRootCache struct {
	listings map[string][]string
	observe  searchRootObserver
	budget   *searchRootBudget
}

// NewSearchRootCache constructs an empty operation-local search-root cache.
func NewSearchRootCache() *SearchRootCache {
	cache, err := newSearchRootCache(observeSearchRoot, defaultSearchRootLimits())
	if err != nil {
		panic(err)
	}
	return cache
}

func newSearchRootCache(
	observer searchRootObserver,
	limits searchRootLimits,
) (*SearchRootCache, error) {
	if observer == nil {
		return nil, fmt.Errorf("skill search-root observer is required")
	}
	budget, err := newSearchRootBudget(limits)
	if err != nil {
		return nil, err
	}
	return &SearchRootCache{
		listings: make(map[string][]string),
		observe:  observer,
		budget:   budget,
	}, nil
}

func (cache *SearchRootCache) entries(ctx context.Context, liveRoot string) ([]string, error) {
	if cache == nil || cache.observe == nil || cache.listings == nil || cache.budget == nil {
		return nil, fmt.Errorf("skill search-root cache is required")
	}
	if ctx == nil {
		return nil, fmt.Errorf("skill search-root context is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	readRoot, err := resolvedImportSkillReadPath(liveRoot)
	if err != nil {
		return nil, fmt.Errorf("resolve skill search root %q: %w", liveRoot, err)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if names, exists := cache.listings[readRoot]; exists {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return append([]string(nil), names...), nil
	}

	names := make([]string, 0, min(cache.budget.limits.maximumEntries, 256))
	if err := cache.observe(ctx, readRoot, func(name string) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := cache.budget.admitName(len(name)); err != nil {
			return err
		}
		names = append(names, name)
		return nil
	}); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	slices.Sort(names)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	cache.listings[readRoot] = append([]string(nil), names...)
	return names, nil
}

func observeSearchRoot(
	ctx context.Context,
	readRoot string,
	visit func(string) error,
) error {
	if ctx == nil {
		return fmt.Errorf("skill search-root context is required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	view, err := access.OpenNoFollowView(readRoot)
	if err != nil {
		return fmt.Errorf("open skill search root %q: %w", readRoot, err)
	}
	if view.Kind() != artifact.ArtifactKindDirectory {
		return fmt.Errorf("skill search root %q is not a directory", readRoot)
	}
	if err := view.VisitDirectoryNames(ctx, ".", visit); err != nil {
		return fmt.Errorf("enumerate skill search root %q: %w", readRoot, err)
	}
	return nil
}
