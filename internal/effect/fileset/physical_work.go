package fileset

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"

	"github.com/isty2e/daem/internal/contractversion"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
)

const (
	maximumFileSetOperationEntries       = 400_000
	maximumFileSetOperationBytes   int64 = 16 << 30
)

// PhysicalWorkBudget is the shared operation envelope used by StateDir
// identity work and file-set fence census. Identity capture lives in
// recoverygate; this package charges census work and inspects fence evidence.
type PhysicalWorkBudget interface {
	rootedpath.PhysicalTraversalBudget
	AdmitPhysicalWork(pathComponents int, entries int, bytes int64) error
}

// FenceCensusWork is the maximum physical work one StateDir fence census
// may consume. Recoverygate converts it into StateDir reservation demand.
type FenceCensusWork struct {
	PathComponents int
	Entries        int
	Bytes          int64
}

// MaximumFenceCensusWork returns the envelope for one complete fence census
// at the named StateDir. It does not observe the directory.
func MaximumFenceCensusWork(stateDir string, maximumPhysicalDepth int) (FenceCensusWork, error) {
	work, err := maximumFileSetFenceCensusWork(stateDir, maximumPhysicalDepth)
	if err != nil {
		return FenceCensusWork{}, err
	}
	return FenceCensusWork{
		PathComponents: work.pathComponents,
		Entries:        work.entries,
		Bytes:          work.bytes,
	}, nil
}

// MaximumFenceCensusWork4 returns a conservative envelope for one generation-4
// fence observation, including the marker and all retained evidence images.
func MaximumFenceCensusWork4(stateDir string, maximumPhysicalDepth int) (FenceCensusWork, error) {
	base, err := maximumFileSetFenceCensusWork(stateDir, maximumPhysicalDepth)
	if err != nil {
		return FenceCensusWork{}, err
	}
	evidencePathWork := 0
	for index := range maximumFileSetTargets {
		path := filepath.Join(transactionDir(stateDir), fmt.Sprintf("target-%03d.before", index))
		pathWork, err := fileSetAbsolutePathWork(path, maximumPhysicalDepth)
		if err != nil {
			return FenceCensusWork{}, err
		}
		evidencePathWork, err = fileSetWorkAdd(evidencePathWork, pathWork)
		if err != nil {
			return FenceCensusWork{}, err
		}
	}
	imagePathWork, err := fileSetAbsolutePathWork(
		filepath.Join(transactionDir(stateDir), referenceAfterImageName),
		maximumPhysicalDepth,
	)
	if err != nil {
		return FenceCensusWork{}, err
	}
	evidencePathWork, err = fileSetWorkAdd(evidencePathWork, imagePathWork)
	if err != nil {
		return FenceCensusWork{}, err
	}
	evidencePathWork, err = fileSetWorkMultiply(evidencePathWork, 2)
	if err != nil {
		return FenceCensusWork{}, err
	}
	targetPathWork, err := fileSetWorkMultiply(maximumFileSetTargets, maximumPhysicalDepth)
	if err != nil {
		return FenceCensusWork{}, err
	}
	pathComponents, err := fileSetWorkAdd(base.pathComponents, targetPathWork)
	if err != nil {
		return FenceCensusWork{}, err
	}
	pathComponents, err = fileSetWorkAdd(pathComponents, evidencePathWork)
	if err != nil {
		return FenceCensusWork{}, err
	}
	evidenceEntries, err := fileSetWorkMultiply(maximumFileSetTargets+1, 2)
	if err != nil {
		return FenceCensusWork{}, err
	}
	entries, err := fileSetWorkAdd(base.entries, evidenceEntries)
	if err != nil {
		return FenceCensusWork{}, err
	}
	bytes := base.bytes + maximumMarkerBytes + maximumStagedBeforeImageBytes
	return FenceCensusWork{
		PathComponents: pathComponents,
		Entries:        entries,
		Bytes:          bytes,
	}, nil
}

// WrapFileSetAccessUnprovable marks a fence or StateDir observation failure
// as access-unprovable without changing the underlying cause.
func WrapFileSetAccessUnprovable(err error) error {
	return wrapFileSetAccessUnprovable(err)
}

// CanonicalStateDirBounded admits one StateDir spelling against a caller-owned
// physical traversal budget. It does not retain incarnation authority.
func CanonicalStateDirBounded(
	path string,
	maximumPhysicalDepth int,
	budget rootedpath.PhysicalTraversalBudget,
) (string, error) {
	return canonicalStateDirBounded(path, maximumPhysicalDepth, budget)
}

// observeClearFence inspects published file-set evidence without retaining
// StateDir incarnation authority. Production gates capture identity first and
// call ObserveClearFenceAt.
func observeClearFence(ctx context.Context, stateDir string) error {
	if ctx == nil {
		return fmt.Errorf("file-set transaction context is required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	canonical, err := canonicalStateDir(stateDir)
	if err != nil {
		return err
	}
	return ObserveClearFenceAt(
		ctx,
		canonical,
		mutationfs.MaximumPhysicalPathDepth,
		&fenceObservationBudget{},
	)
}

// ObserveClearFenceAt inspects published evidence at an absolute clean StateDir
// spelling while charging the caller-owned budget. It does not establish
// canonical selection or incarnation authority; unprovable access is refused.
func ObserveClearFenceAt(
	ctx context.Context,
	stateDir string,
	maximumPhysicalDepth int,
	physicalWorkBudget PhysicalWorkBudget,
) error {
	return requireClearFileSetAtCanonicalPath(
		ctx,
		stateDir,
		maximumPhysicalDepth,
		physicalWorkBudget,
	)
}

// ObserveClearPublicationFenceAt recognizes legacy-3 and containing-intent-5
// markers without opening the intent or target paths. Inactive siblings are not
// transaction evidence. This observation grants no recovery or GC authority.
func ObserveClearPublicationFenceAt(
	ctx context.Context,
	evidenceRoot string,
	maximumPhysicalDepth int,
	physicalWorkBudget PhysicalWorkBudget,
) error {
	return requireClearFileSetAtCanonicalPathVersion(
		ctx,
		evidenceRoot,
		maximumPhysicalDepth,
		physicalWorkBudget,
		contractversion.MetadataTransactionIntent,
	)
}

// ObserveClearFenceAt4 validates a generation-4 marker and every retained
// evidence image while charging the caller-owned fence budget.
func ObserveClearFenceAt4(
	ctx context.Context,
	canonicalStateDir string,
	maximumPhysicalDepth int,
	physicalWorkBudget PhysicalWorkBudget,
) error {
	return requireClearFileSetAtCanonicalPathVersion(
		ctx,
		canonicalStateDir,
		maximumPhysicalDepth,
		physicalWorkBudget,
		contractversion.MetadataTransactionReference,
	)
}

func fileSetAbsolutePathWork(path string, maximumPhysicalDepth int) (int, error) {
	counter := &fileSetPathWorkCounter{}
	if err := rootedpath.ChargeAbsolutePath(path, maximumPhysicalDepth, counter); err != nil {
		return 0, err
	}
	return counter.count, nil
}

type fileSetPathWorkCounter struct {
	count int
}

func (counter *fileSetPathWorkCounter) AdmitPathComponents(count int) error {
	if count < 0 {
		return fmt.Errorf("file-set path work is invalid")
	}
	if count > mutationfs.MaximumPhysicalPathComponentVisits-counter.count {
		return fmt.Errorf(
			"file-set path work exceeds operation limit %d",
			mutationfs.MaximumPhysicalPathComponentVisits,
		)
	}
	counter.count += count
	return nil
}

type fenceObservationBudget struct {
	mu      sync.Mutex
	paths   int
	entries int
	bytes   int64
}

func (budget *fenceObservationBudget) AdmitPathComponents(count int) error {
	return budget.AdmitPhysicalWork(count, 0, 0)
}

type fileSetPhysicalWork struct {
	pathComponents int
	entries        int
	bytes          int64
}

func fileSetWorkAdd(left int, right int) (int, error) {
	if left < 0 || right < 0 || left > int(^uint(0)>>1)-right {
		return 0, fmt.Errorf("file-set physical work overflows")
	}
	return left + right, nil
}

func fileSetWorkMultiply(value int, count int) (int, error) {
	if value < 0 || count < 0 {
		return 0, fmt.Errorf("file-set physical work is invalid")
	}
	if count != 0 && value > int(^uint(0)>>1)/count {
		return 0, fmt.Errorf("file-set physical work overflows")
	}
	return value * count, nil
}

func (budget *fenceObservationBudget) AdmitPhysicalWork(
	pathComponents int,
	entries int,
	bytes int64,
) error {
	if budget == nil {
		return fmt.Errorf("file-set physical work budget is required")
	}
	budget.mu.Lock()
	defer budget.mu.Unlock()
	if pathComponents < 0 || entries < 0 || bytes < 0 {
		return fmt.Errorf("file-set physical work is invalid")
	}
	if pathComponents > mutationfs.MaximumPhysicalPathComponentVisits-budget.paths {
		return fmt.Errorf(
			"file-set path work exceeds operation limit %d",
			mutationfs.MaximumPhysicalPathComponentVisits,
		)
	}
	if entries > maximumFileSetOperationEntries-budget.entries {
		return fmt.Errorf(
			"file-set entry work exceeds operation limit %d",
			maximumFileSetOperationEntries,
		)
	}
	if bytes > maximumFileSetOperationBytes-budget.bytes {
		return fmt.Errorf(
			"file-set byte work exceeds operation limit %d",
			maximumFileSetOperationBytes,
		)
	}
	budget.paths += pathComponents
	budget.entries += entries
	budget.bytes += bytes
	return nil
}
