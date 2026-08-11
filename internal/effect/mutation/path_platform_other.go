//go:build !darwin && !windows

package mutation

import (
	"path/filepath"

	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
)

func platformCanonicalPath(selection pathSelection, _ PathEffect) (canonicalPath, error) {
	path := selectedAccessPath(selection)
	witness, err := newPathSemanticsWitness("exact-v1", nil)
	if err != nil {
		return canonicalPath{}, err
	}
	return canonicalPath{keyPath: filepath.Clean(path), accessPath: path, witness: witness}, nil
}

func platformCanonicalPathBounded(
	selection pathSelection,
	effect PathEffect,
	_ int,
	_ rootedpath.PhysicalTraversalBudget,
) (canonicalPath, error) {
	return platformCanonicalPath(selection, effect)
}
