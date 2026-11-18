package adopt

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	nativehostpath "github.com/isty2e/daem/internal/output/hostpath/native"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/realization/profile/configuration"
)

// Lookup spelling and comparison authority are deliberately retained separately.
type capturePath struct {
	lookup   string
	observed mutation.DirectoryEntryAuthorityObservation
}

func captureReferent(path string, budget *physicalwork.Budget) (capturePath, error) {
	physical, err := rootedpath.ResolveReferentPathBounded(path, physicalwork.MaximumPhysicalPathDepth, budget)
	if err != nil {
		return capturePath{}, err
	}
	physical, err = mutation.CanonicalDirectoryEntryPathBounded(physical, physicalwork.MaximumPhysicalPathDepth, budget)
	if err != nil {
		return capturePath{}, err
	}
	observed, err := mutation.ObserveDirectoryEntryAuthorityBounded(physical, physicalwork.MaximumPhysicalPathDepth, budget)
	if err != nil {
		return capturePath{}, err
	}
	if reference, present := observed.Reference(); present {
		physical = reference.Locator()
	}
	return capturePath{lookup: physical, observed: observed}, nil
}

func captureHostOutput(paths daempaths.Paths, document configuration.DocumentSpec, budget *physicalwork.Budget) (string, error) {
	root := paths.ManifestRoot
	if document.RootReference().Identity() != configuration.ProjectRootIdentity {
		locations, err := nativehostpath.CaptureLocations(paths.ManifestRoot)
		if err != nil {
			return "", err
		}
		location, err := locations.Resolve(document.RootReference())
		if err != nil {
			return "", err
		}
		root = location.Path()
	}
	root, err := rootedpath.ResolveRootSelectorBounded(root, physicalwork.MaximumPhysicalPathDepth, budget)
	if err != nil {
		return "", err
	}
	child, directory := document.Coordinate().DirectoryChild()
	if !directory {
		return "", fmt.Errorf("host configuration output requires a directory-child document")
	}
	return filepath.Join(root, filepath.FromSlash(child)), nil
}

func (path capturePath) mayOverlap(other capturePath) bool {
	return path.observed.MayOverlap(other.observed)
}

func (path capturePath) contains(other capturePath) bool {
	parent, present := path.observed.Exact()
	if !present {
		return false
	}
	if child, present := other.observed.Exact(); present {
		return parent.Contains(child)
	}
	missing, _ := other.observed.Provisional()
	return missing.ConfinedWithin(parent)
}

// sourceChild requires a stable direct-child name before applying a selector.
// Exact path semantics alone do not imply that the source directory exists.
func (path capturePath) sourceChild(other capturePath) (string, bool) {
	parent, exact := path.observed.Exact()
	if !exact || !path.contains(other) || path.observed.Equal(other.observed) {
		return "", false
	}
	if missing, present := other.observed.Provisional(); present {
		if _, stable := missing.StableRelativePrefix(parent); !stable {
			return "", false
		}
	}
	relative, err := filepath.Rel(parent.Key(), other.observed.NamespaceKey())
	if err != nil || relative == "." {
		return "", false
	}
	child := other.lookup
	for range len(strings.Split(relative, string(filepath.Separator))) - 1 {
		child = filepath.Dir(child)
	}
	return filepath.Base(child), true
}
