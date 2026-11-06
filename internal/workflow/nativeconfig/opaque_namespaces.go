package nativeconfig

import (
	"context"
	"errors"
	"fmt"

	relationobserve "github.com/isty2e/daem/internal/assurance/observe/relation"
	"github.com/isty2e/daem/internal/assurance/pathauthority"
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/effect/physicalwork"
)

var ErrOpaqueNamespaceOverlap = errors.New("opaque plugin namespace overlaps selected settings without an admitted rewrite contract")

// RequireDisjointOpaqueNamespaces rejects unresolved cross-owner rewrites before
// effects. It retains provisional exclusion and supplies no write authority.
func RequireDisjointOpaqueNamespaces(ctx context.Context, native []mutation.DirectoryEntryAuthorityObservation, paths []relationobserve.AuthorityPath, budget rootedpath.PhysicalTraversalBudget) error {
	if ctx == nil || budget == nil {
		return fmt.Errorf("opaque namespace comparison requires context and allocation")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	for _, observed := range native {
		if err := observed.Validate(); err != nil {
			return err
		}
	}
	for _, path := range paths {
		if err := ctx.Err(); err != nil {
			return err
		}
		layout, err := rootedpath.CaptureReferentLayoutBounded(path.Path(), physicalwork.MaximumPhysicalPathDepth, budget)
		if err != nil {
			return err
		}
		observed, err := mutation.ObserveDirectoryEntryAuthorityBounded(layout.CanonicalPath(), physicalwork.MaximumPhysicalPathDepth, budget)
		if err != nil {
			return err
		}
		host := opaqueComparisonNamespace(observed)
		for _, observed := range native {
			selected := opaqueComparisonNamespace(observed)
			if host.Contains(selected) || selected.Contains(host) {
				return ErrOpaqueNamespaceOverlap
			}
		}
	}
	return nil
}

func opaqueComparisonNamespace(observed mutation.DirectoryEntryAuthorityObservation) pathauthority.Exact {
	if exact, present := observed.Exact(); present {
		return exact
	}
	provisional, _ := observed.Provisional()
	return provisional.Namespace()
}
