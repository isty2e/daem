package qualification

import (
	"context"
	"fmt"
	"slices"

	relationhost "github.com/isty2e/daem/internal/assurance/observe/relation/host"
	journalnamespace "github.com/isty2e/daem/internal/effect/journal/namespace"
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/effect/physicalwork"
)

func QualifySelectedContext(ctx context.Context, edit relationhost.SelectedDocumentEdit, manifest journalnamespace.RootGrant, namespace journalnamespace.DocumentNamespace, budget rootedpath.PhysicalTraversalBudget) error {
	if ctx == nil {
		return fmt.Errorf("selected context qualification requires context")
	}
	if err := edit.Validate(); err != nil {
		return err
	}
	if !slices.Contains(namespace.Addresses(), edit.DocumentAddress()) {
		return fmt.Errorf("selected document edit has no qualified namespace alias")
	}
	for _, root := range edit.DefinitionRoots() {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := manifest.ValidateSelection(root, budget); err != nil {
			return err
		}
	}
	referent, err := rootedpath.ResolveReferentPathBounded(edit.SelectedPath(), physicalwork.MaximumPhysicalPathDepth, budget)
	if err != nil {
		return err
	}
	observation, err := mutation.ObserveDirectoryEntryAuthorityBounded(referent, physicalwork.MaximumPhysicalPathDepth, budget)
	if err != nil {
		return err
	}
	if !observation.Equal(namespace.Observation()) {
		return fmt.Errorf("selected normalization context does not identify the qualified document referent")
	}
	return ctx.Err()
}
