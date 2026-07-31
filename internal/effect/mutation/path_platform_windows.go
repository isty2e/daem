//go:build windows

package mutation

import (
	"path/filepath"
	"strings"
)

func platformCanonicalPath(selection pathSelection, _ PathEffect) (canonicalPath, error) {
	path := selectedAccessPath(selection)
	witness, err := newPathSemanticsWitness("windows-fold-v1", nil)
	if err != nil {
		return canonicalPath{}, err
	}
	return canonicalPath{
		keyPath:    strings.ToLower(filepath.Clean(path)),
		accessPath: path,
		witness:    witness,
	}, nil
}
