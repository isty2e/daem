package generate

import (
	"sort"

	"github.com/isty2e/daem/internal/supply/source"
	"github.com/isty2e/daem/internal/supply/source/backend/localfs"
)

// ConsumedLocalPaths returns the deterministic local filesystem inputs read by
// Build.
func ConsumedLocalPaths(input Input) ([]string, error) {
	resolver, err := localfs.NewResolver(input.Paths.ManifestRoot)
	if err != nil {
		return nil, err
	}
	paths := make(map[string]struct{})
	addResolvedSource := func(sourceSpec source.Source) error {
		if sourceSpec.Kind() != source.SourceKindLocal {
			return nil
		}
		path, err := resolver.LocalInputAuthorityPath(sourceSpec)
		if err != nil {
			return err
		}
		paths[path] = struct{}{}
		return nil
	}

	for _, spec := range input.Environment.EntityArtifactSources() {
		if err := addResolvedSource(spec); err != nil {
			return nil, err
		}
	}
	for _, set := range input.Environment.SkillSets() {
		if err := addResolvedSource(set.Source()); err != nil {
			return nil, err
		}
	}
	result := make([]string, 0, len(paths))
	for path := range paths {
		result = append(result, path)
	}
	sort.Strings(result)
	return result, nil
}
