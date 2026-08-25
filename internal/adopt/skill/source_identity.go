package skill

import (
	"context"
	"errors"
	"fmt"
	"math"
	"path/filepath"

	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/supply/artifact/access"
)

const (
	maximumSourceIdentityOperationEntries = 400_000
	maximumSourceIdentityOperationBytes   = int64(16 << 30)
)

var errSourceIdentityLimitExceeded = errors.New("skill source identity observation limit exceeded")

type sourceIdentityMeasurement struct {
	entries int
	bytes   int64
}

type sourceIdentityObserver func(
	context.Context,
	string,
	access.TraversalLimit,
) (artifact.ContentHash, sourceIdentityMeasurement, error)

type sourceIdentityBudget struct {
	maximumEntries int
	maximumBytes   int64
	entries        int
	bytes          int64
}

func newSourceIdentityBudget(maximumEntries int, maximumBytes int64) (*sourceIdentityBudget, error) {
	if maximumEntries < 0 {
		return nil, fmt.Errorf("skill source identity entries must not be negative")
	}
	if maximumBytes < 0 {
		return nil, fmt.Errorf("skill source identity bytes must not be negative")
	}
	return &sourceIdentityBudget{
		maximumEntries: maximumEntries,
		maximumBytes:   maximumBytes,
	}, nil
}

func (budget *sourceIdentityBudget) traversalLimit() (access.TraversalLimit, error) {
	if budget == nil {
		return access.TraversalLimit{}, fmt.Errorf("skill source identity budget is required")
	}
	remainingEntries := budget.maximumEntries - budget.entries
	remainingBytes := budget.maximumBytes - budget.bytes
	return access.NewTraversalLimit(uint64(remainingEntries)+1, remainingBytes)
}

func (budget *sourceIdentityBudget) consume(measurement sourceIdentityMeasurement) error {
	if budget == nil {
		return fmt.Errorf("skill source identity budget is required")
	}
	entries := measurement.entries
	bytes := measurement.bytes
	if entries < 0 || bytes < 0 {
		return fmt.Errorf("skill source identity measurement is invalid")
	}
	if entries > budget.maximumEntries-budget.entries {
		return budget.limitError("operation_entries", int64(budget.maximumEntries), int64(budget.entries)+int64(entries))
	}
	if bytes > budget.maximumBytes-budget.bytes {
		return budget.limitError("operation_bytes", budget.maximumBytes, saturatedAdd(budget.bytes, bytes))
	}
	budget.entries += entries
	budget.bytes += bytes
	return nil
}

func (budget *sourceIdentityBudget) traversalError(err error, previousBytes int64) error {
	if errors.Is(err, access.ErrTraversalEntryLimitExceeded) {
		return budget.limitError(
			"operation_entries",
			int64(budget.maximumEntries),
			int64(budget.maximumEntries)+1,
		)
	}
	var byteLimit *access.LimitError
	if errors.As(err, &byteLimit) {
		return budget.limitError(
			"operation_bytes",
			budget.maximumBytes,
			saturatedAdd(previousBytes, byteLimit.Observed()),
		)
	}
	return nil
}

func (budget *sourceIdentityBudget) limitError(kind string, limit int64, observed int64) error {
	return fmt.Errorf(
		"%w: %s observed=%d limit=%d",
		errSourceIdentityLimitExceeded,
		kind,
		observed,
		limit,
	)
}

func saturatedAdd(left int64, right int64) int64 {
	if right > math.MaxInt64-left {
		return math.MaxInt64
	}
	return left + right
}

// SourceIdentityCache owns successful skill-directory identity observations
// and classified eligibility skips for one candidate-collection pass. It also
// owns that pass's aggregate entry and byte budget and must not outlive one
// BuildPlan call.
type SourceIdentityCache struct {
	contentHashes map[string]artifact.ContentHash
	classified    map[string]error
	observe       sourceIdentityObserver
	budget        *sourceIdentityBudget
}

// NewSourceIdentityCache constructs an empty operation-local identity cache
// with the caller-selected tree-shape limit and the import observation envelope.
func NewSourceIdentityCache(structureLimit access.TreeStructureLimit) *SourceIdentityCache {
	return newSourceIdentityCache(func(
		ctx context.Context,
		readPath string,
		traversalLimit access.TraversalLimit,
	) (artifact.ContentHash, sourceIdentityMeasurement, error) {
		return observeSkillDirectoryIdentity(ctx, readPath, traversalLimit, structureLimit)
	})
}

func newSourceIdentityCache(observer sourceIdentityObserver) *SourceIdentityCache {
	return newSourceIdentityCacheWithLimits(
		observer,
		maximumSourceIdentityOperationEntries,
		maximumSourceIdentityOperationBytes,
	)
}

func newSourceIdentityCacheWithLimits(
	observer sourceIdentityObserver,
	maximumEntries int,
	maximumBytes int64,
) *SourceIdentityCache {
	budget, err := newSourceIdentityBudget(maximumEntries, maximumBytes)
	if err != nil {
		panic(err)
	}
	return &SourceIdentityCache{
		contentHashes: make(map[string]artifact.ContentHash),
		classified:    make(map[string]error),
		observe:       observer,
		budget:        budget,
	}
}

// ContentHash returns the exact content hash of one fully resolved skill route.
// Classified eligibility skips are memoized; operational failures are not.
// The cache is operation-local and intentionally not safe for concurrent use.
func (cache *SourceIdentityCache) ContentHash(
	ctx context.Context,
	readPath string,
) (artifact.ContentHash, error) {
	if cache == nil || cache.observe == nil || cache.contentHashes == nil || cache.classified == nil || cache.budget == nil {
		return "", fmt.Errorf("skill source identity cache is required")
	}
	if ctx == nil {
		return "", fmt.Errorf("skill source identity context is required")
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if !filepath.IsAbs(readPath) || filepath.Clean(readPath) != readPath {
		return "", fmt.Errorf("skill read path %q must be canonical and absolute", readPath)
	}
	if contentHash, exists := cache.contentHashes[readPath]; exists {
		return contentHash, nil
	}
	if classified, exists := cache.classified[readPath]; exists {
		return "", classified
	}
	traversalLimit, err := cache.budget.traversalLimit()
	if err != nil {
		return "", err
	}
	previousBytes := cache.budget.bytes
	contentHash, measurement, observationErr := cache.observe(ctx, readPath, traversalLimit)
	if err := cache.budget.consume(measurement); err != nil {
		return "", err
	}
	if observationErr != nil {
		if limitErr := cache.budget.traversalError(observationErr, previousBytes); limitErr != nil {
			return "", limitErr
		}
		if !classifiedSkillSkip(observationErr) {
			return "", observationErr
		}
		if err := ctx.Err(); err != nil {
			return "", err
		}
		cache.classified[readPath] = observationErr
		return "", observationErr
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if err := contentHash.Validate(); err != nil {
		return "", fmt.Errorf("skill source identity for %q: %w", readPath, err)
	}
	cache.contentHashes[readPath] = contentHash
	return contentHash, nil
}

func classifiedSkillSkip(err error) bool {
	return errors.Is(err, access.ErrRequiredRootRegularFile) || errors.Is(err, access.ErrUnsupportedSymlink)
}

func observeSkillDirectoryIdentity(
	ctx context.Context,
	readPath string,
	traversalLimit access.TraversalLimit,
	structureLimit access.TreeStructureLimit,
) (artifact.ContentHash, sourceIdentityMeasurement, error) {
	view, err := access.OpenNoFollowView(readPath)
	if err != nil {
		return "", sourceIdentityMeasurement{}, fmt.Errorf("open skill tree %q: %w", readPath, err)
	}
	if view.Kind() != artifact.ArtifactKindDirectory {
		return "", sourceIdentityMeasurement{}, fmt.Errorf("skill tree %q is not a directory", readPath)
	}
	contentHash, measurement, err := view.HashDirectoryRequiringRootFile(
		ctx,
		"SKILL.md",
		traversalLimit,
		structureLimit,
	)
	observed := sourceIdentityMeasurement{
		entries: measurement.DescendantEntries(),
		bytes:   measurement.RegularFileBytes(),
	}
	if err != nil {
		return "", observed, fmt.Errorf("hash skill tree %q: %w", readPath, err)
	}
	return contentHash, observed, nil
}
