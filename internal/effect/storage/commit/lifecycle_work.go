package commit

import (
	"strings"

	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
)

// MaximumRootedEntryRenamePathWork bounds parent opening, retained-chain seals
// and moved-entry verification. Rename reads no payload and traverses no tree.
func MaximumRootedEntryRenamePathWork(destination rootedpath.Destination) (int, error) {
	r, err := destination.RootValidationWork()
	if err != nil {
		return 0, err
	}
	w, err := destination.ParentChainValidationWork()
	if err != nil {
		return 0, err
	}
	// Parent opening and each seal may revalidate the root for every
	// intermediate handle. Eight complete passes cover opening and moved checks.
	parents, err := treePublicationWorkProduct(strings.Count(destination.Relative().Path(), "/"), r)
	if err != nil {
		return 0, err
	}
	chainWork, err := treePublicationWorkSum(w, parents)
	if err != nil {
		return 0, err
	}
	chains, err := treePublicationWorkProduct(8, chainWork)
	if err != nil {
		return 0, err
	}
	roots, err := treePublicationWorkProduct(8, r)
	if err != nil {
		return 0, err
	}
	return treePublicationWorkSum(chains, roots)
}
