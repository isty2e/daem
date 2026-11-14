package commit

import (
	"fmt"
	"math"
	"strings"

	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
)

// FlatRootedTreePublicationWork bounds prepare, population, Commit and one
// returned-error Abort for a flat regular-file tree. It excludes later cleanup
// of the published tree and work performed by the population callback itself.
type FlatRootedTreePublicationWork struct {
	pathWork  int
	entryWork int
	byteWork  int64
}

// MaximumFlatRootedTreePublicationWork derives the aggregate storage work from
// the exact per-pass limits and destination. It does not reserve a budget or
// observe paths. Callers that admit it once use an unbudgeted capability and
// the same limits for PrepareRootedTreeWithLimits.
func MaximumFlatRootedTreePublicationWork(
	destination rootedpath.Destination,
	limits mutationfs.TreeTraversalLimits,
) (FlatRootedTreePublicationWork, error) {
	if err := limits.Validate(); err != nil {
		return FlatRootedTreePublicationWork{}, err
	}
	if limits.MaximumDepth() != 0 {
		return FlatRootedTreePublicationWork{}, fmt.Errorf("publication work requires a flat regular-file tree")
	}
	rootWork, err := destination.RootValidationWork()
	if err != nil {
		return FlatRootedTreePublicationWork{}, err
	}
	retainedChainWork, err := destination.ParentChainValidationWork()
	if err != nil {
		return FlatRootedTreePublicationWork{}, err
	}
	parentDepth := strings.Count(destination.Relative().Path(), "/")
	entries := limits.MaximumEntries()
	maximumInt := int(^uint(0) >> 1)
	if entries > (maximumInt-2)/26 || parentDepth > (maximumInt-13)/2 ||
		limits.MaximumBytes() > (math.MaxInt64-3)/9 {
		return FlatRootedTreePublicationWork{}, fmt.Errorf("flat tree publication work overflows")
	}

	// Population visits N entries. Capture, five whole seals, sync and modes
	// visit 16N; failure normalization and removal visit at most 9N. The main
	// and cleanup paths each have one terminating overflow-name probe.
	entryWork := 26*entries + 2
	// Include source reads and writes (2S), content capture/sync/modes and
	// failure normalization (4S), and removal's size accounting (3S). Content
	// and source overflow probes can consume at most three extra bytes.
	byteWork := 9*limits.MaximumBytes() + 3

	// Non-retained chains additionally revalidate the root for each parent
	// handle. Direct full handle validations include the concurrent ancestor
	// creation branch. Root opening and parent traversal add R+P.
	parentRootWork, err := treePublicationWorkProduct(parentDepth, rootWork)
	if err != nil {
		return FlatRootedTreePublicationWork{}, err
	}
	chainWork, err := treePublicationWorkSum(retainedChainWork, parentRootWork)
	if err != nil {
		return FlatRootedTreePublicationWork{}, err
	}
	chains, err := treePublicationWorkProduct(6*entries+19, chainWork)
	if err != nil {
		return FlatRootedTreePublicationWork{}, err
	}
	directChecks, err := treePublicationWorkSum(11*entries+13, 2*parentDepth)
	if err != nil {
		return FlatRootedTreePublicationWork{}, err
	}
	directWork, err := treePublicationWorkProduct(directChecks, rootWork)
	if err != nil {
		return FlatRootedTreePublicationWork{}, err
	}
	pathWork, err := treePublicationWorkSum(chains, directWork, rootWork, parentDepth)
	if err != nil {
		return FlatRootedTreePublicationWork{}, err
	}
	return FlatRootedTreePublicationWork{pathWork: pathWork, entryWork: entryWork, byteWork: byteWork}, nil
}

func (work FlatRootedTreePublicationWork) PathWork() int   { return work.pathWork }
func (work FlatRootedTreePublicationWork) EntryWork() int  { return work.entryWork }
func (work FlatRootedTreePublicationWork) ByteWork() int64 { return work.byteWork }

func treePublicationWorkProduct(left, right int) (int, error) {
	if left < 0 || right < 0 || right != 0 && left > int(^uint(0)>>1)/right {
		return 0, fmt.Errorf("flat tree publication work overflows")
	}
	return left * right, nil
}

func treePublicationWorkSum(values ...int) (int, error) {
	result := 0
	for _, value := range values {
		if value < 0 || value > int(^uint(0)>>1)-result {
			return 0, fmt.Errorf("flat tree publication work overflows")
		}
		result += value
	}
	return result, nil
}
