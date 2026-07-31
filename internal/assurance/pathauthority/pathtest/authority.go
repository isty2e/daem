// Package pathtest constructs synthetic exact path authority for tests that
// exercise higher-level models without a filesystem observation boundary.
package pathtest

import (
	"path/filepath"
	"strings"

	"github.com/isty2e/daem/internal/assurance/pathauthority"
)

// Exact returns one synthetic exact-v1 authority or panics when key is not an
// absolute clean path. Tests of path observation must use effect/mutation.
func Exact(key string) pathauthority.Exact {
	return withWitness(key, "exact-v1:")
}

// DarwinCaseSensitive returns a synthetic authority whose every path
// component was observed as case-sensitive.
func DarwinCaseSensitive(key string) pathauthority.Exact {
	volume := filepath.VolumeName(key)
	relative := strings.TrimPrefix(key, volume+string(filepath.Separator))
	componentCount := 0
	if relative != "" {
		componentCount = len(strings.Split(relative, string(filepath.Separator)))
	}
	return withWitness(key, "darwin-case-v1:"+strings.Repeat("s", componentCount))
}

func withWitness(key string, witness string) pathauthority.Exact {
	authority, err := pathauthority.NewExact(key, witness)
	if err != nil {
		panic(err)
	}
	return authority
}
