package apply

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/isty2e/daem/internal/desired"
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/output"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/supply/source"
	localfs "github.com/isty2e/daem/internal/supply/source/backend/localfs"
)

func rejectLocalSourceMutationOverlap(planned commandPlan) error {
	authorityPaths, err := localEntityArtifactSourceAuthorityPaths(
		planned.context.Paths,
		planned.context.RuntimeEnvironment,
	)
	if err != nil {
		return err
	}
	sources := make([]string, 0, len(authorityPaths))
	for _, authorityPath := range authorityPaths {
		canonical, err := mutation.CanonicalDirectoryEntryKey(authorityPath)
		if err != nil {
			return err
		}
		sources = append(sources, canonical)
	}

	resolveDestination := destinationResolver(planned.context.Paths).Resolve
	for _, destination := range hostMutationDestinations(planned.assessment.Reconciliation) {
		path, err := resolveDestination(destination)
		if err != nil {
			return err
		}
		canonical, err := mutation.CanonicalDirectoryEntryKey(path)
		if err != nil {
			return err
		}
		for _, sourcePath := range sources {
			if pathsOverlap(sourcePath, canonical) {
				return fmt.Errorf(
					"local source %q overlaps managed mutation destination %q",
					sourcePath,
					canonical,
				)
			}
		}
	}
	return nil
}

func localEntityArtifactSourceAuthorityPaths(
	paths daempaths.Paths,
	environment desired.Environment,
) ([]string, error) {
	resolver, err := localfs.NewResolver(paths.ManifestRoot)
	if err != nil {
		return nil, err
	}
	seen := make(map[string]struct{})
	for _, sourceSpec := range environment.EntityArtifactSources() {
		if sourceSpec.Kind() != source.SourceKindLocal {
			continue
		}
		path, err := resolver.LocalInputAuthorityPath(sourceSpec)
		if err != nil {
			return nil, err
		}
		seen[path] = struct{}{}
	}
	result := make([]string, 0, len(seen))
	for path := range seen {
		result = append(result, path)
	}
	sort.Strings(result)
	return result, nil
}

func hostMutationDestinations(planResult reconcile.Result) []output.Destination {
	destinations := make([]output.Destination, 0)
	for _, decision := range planResult.MutatingManagedPaths() {
		if !decision.MutatesHost() {
			continue
		}
		destinations = append(destinations, decision.Destination())
		if previous, present := decision.PreviousState(); present && previous.Destination() != decision.Destination() {
			destinations = append(destinations, previous.Destination())
		}
	}
	for _, decision := range planResult.MutatingAggregates() {
		if decision.MutatesHost() {
			destinations = append(destinations, output.Destination(decision.DocumentAddress().AggregateRoot()))
		}
	}
	return destinations
}

func pathsOverlap(left string, right string) bool {
	return pathContains(left, right) || pathContains(right, left)
}

func pathContains(parent string, child string) bool {
	relative, err := filepath.Rel(parent, child)
	if err != nil {
		return false
	}
	return relative == "." || (relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)))
}
