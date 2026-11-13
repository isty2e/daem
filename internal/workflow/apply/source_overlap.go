package apply

import (
	"fmt"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/isty2e/daem/internal/desired"
	journalobservation "github.com/isty2e/daem/internal/effect/journal/observation"
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/output"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/supply/source"
	localfs "github.com/isty2e/daem/internal/supply/source/backend/localfs"
	"github.com/isty2e/daem/internal/topology"
	topologyprojection "github.com/isty2e/daem/internal/topology/projection"
)

func rejectLocalSourceMutationOverlap(planned commandPlan) error {
	authorityPaths, err := localEntityArtifactSourceAuthorityPaths(
		planned.context.Paths,
		planned.context.RuntimeEnvironment,
	)
	if err != nil {
		return err
	}
	sources := make([]mutation.DirectoryEntryAuthorityObservation, 0, len(authorityPaths))
	for _, authorityPath := range authorityPaths {
		physical, err := rootedpath.ResolveReferentPathBounded(authorityPath, physicalwork.MaximumPhysicalPathDepth, planned.context.physicalBudget)
		if err != nil {
			return err
		}
		canonical, err := mutation.ObserveDirectoryEntryAuthorityBounded(physical, physicalwork.MaximumPhysicalPathDepth, planned.context.physicalBudget)
		if err != nil {
			return err
		}
		sources = append(sources, canonical)
	}

	paths, err := hostMutationPaths(planned)
	if err != nil {
		return err
	}
	for _, path := range paths {
		canonical, err := mutation.ObserveDirectoryEntryAuthorityBounded(path, physicalwork.MaximumPhysicalPathDepth, planned.context.physicalBudget)
		if err != nil {
			return err
		}
		for _, sourcePath := range sources {
			if sourcePath.MayOverlap(canonical) {
				return fmt.Errorf(
					"a local source overlaps managed mutation destination %q",
					path,
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

func resolveManagedMutationPath(planned commandPlan, subject topology.SubjectID, destination output.Destination) (string, error) {
	if aggregate.IsHostConfigDocumentSubject(subject) {
		request, _, err := nativeWholeAuthorityRequestFor(planned.assessment.NativeApproval, subject, destination)
		return request.Path, err
	}
	return destinationResolver(planned.context.Paths).Resolve(destination, planned.context.physicalBudget)
}

func nativeWholeAuthorityRequestFor(evidence []journalobservation.ApprovalEvidence, subject topology.SubjectID, destination output.Destination) (mutation.PhysicalAuthorityRequest, mutation.DirectoryEntryAuthorityObservation, error) {
	identity, native := topologyprojection.EntityID(subject)
	if !native || !aggregate.IsHostConfigDocumentSubject(subject) {
		return mutation.PhysicalAuthorityRequest{}, mutation.DirectoryEntryAuthorityObservation{}, fmt.Errorf("native whole-file authority requires its native subject")
	}
	for _, group := range evidence {
		for _, alias := range group.Aliases {
			expected, err := aggregate.HostConfigDocumentSubject(identity, alias.Target(), alias.Scope())
			if err == nil && expected == subject && alias.AggregateRoot() == destination {
				request, err := group.AuthorityRequest(alias)
				return request, group.Authority, err
			}
		}
	}
	return mutation.PhysicalAuthorityRequest{}, mutation.DirectoryEntryAuthorityObservation{}, fmt.Errorf("native whole-file authority has no selected namespace")
}

func hostMutationPaths(planned commandPlan) ([]string, error) {
	planResult := planned.assessment.Reconciliation
	paths := make([]string, 0)
	for _, decision := range planResult.MutatingManagedPaths() {
		if !decision.MutatesHost() {
			continue
		}
		path, err := resolveManagedMutationPath(planned, decision.Subject(), decision.Destination())
		if err != nil {
			return nil, err
		}
		paths = append(paths, path)
		if previous, present := decision.PreviousState(); present && previous.Destination() != decision.Destination() {
			path, err := resolveManagedMutationPath(planned, previous.Subject(), previous.Destination())
			if err != nil {
				return nil, err
			}
			paths = append(paths, path)
		}
	}
	for _, decision := range planResult.MutatingAggregates() {
		if decision.MutatesHost() {
			address := decision.DocumentAddress()
			var path string
			var err error
			qualified := false
			for _, group := range planned.assessment.NativeApproval {
				if slices.Contains(group.Aliases, address) {
					request, requestErr := group.AuthorityRequest(address)
					path, err = request.Path, requestErr
					qualified = true
					break
				}
			}
			if !qualified {
				if _, native := aggregate.HostConfigSyntaxForCodec(decision.CodecContractID()); native {
					return nil, fmt.Errorf("native aggregate mutation has no selected namespace")
				}
				path, err = destinationResolver(planned.context.Paths).Resolve(address.AggregateRoot(), planned.context.physicalBudget)
			}
			if err != nil {
				return nil, err
			}
			paths = append(paths, path)
		}
	}
	return paths, nil
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
