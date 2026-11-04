package ownership

import (
	"cmp"
	"fmt"
	"slices"

	"github.com/isty2e/daem/internal/assurance/pathauthority"
)

// DocumentScope retains qualified comparison namespaces, not filesystem rights.
type DocumentScope struct {
	exact       []pathauthority.Exact
	missing     []pathauthority.Provisional
	initialized bool
}

func NewDocumentScope(exact []pathauthority.Exact, missing []pathauthority.Provisional) (DocumentScope, error) {
	for _, namespace := range exact {
		if err := namespace.Validate(); err != nil {
			return DocumentScope{}, err
		}
	}
	for _, namespace := range missing {
		if err := namespace.Validate(); err != nil {
			return DocumentScope{}, err
		}
	}
	exact = slices.Clone(exact)
	missing = slices.Clone(missing)
	slices.SortFunc(exact, pathauthority.Exact.Compare)
	slices.SortFunc(missing, compareMissingScope)
	return DocumentScope{
		exact: slices.Compact(exact), missing: slices.Compact(missing), initialized: true,
	}, nil
}

func compareMissingScope(left, right pathauthority.Provisional) int {
	return cmp.Or(cmp.Compare(left.CandidateKey(), right.CandidateKey()), cmp.Compare(left.CandidateWitness(), right.CandidateWitness()), left.Namespace().Compare(right.Namespace()))
}

func (scope DocumentScope) Equal(other DocumentScope) bool {
	return scope.initialized && other.initialized && slices.Equal(scope.exact, other.exact) && slices.Equal(scope.missing, other.missing)
}

func (scope DocumentScope) contains(physical pathauthority.Exact) bool {
	_, intersects := scope.intersection(physical)
	return intersects
}

func (scope DocumentScope) intersection(physical pathauthority.Exact) (pathauthority.Exact, bool) {
	for _, namespace := range scope.exact {
		if protectionPathsOverlap(namespace, physical) {
			return namespace, true
		}
	}
	for _, namespace := range scope.missing {
		// Missing normalization-sensitive names retain namespace exclusion;
		// their candidate spelling cannot establish disjoint exact authority.
		if protectionPathsOverlap(namespace.Namespace(), physical) {
			return physical, true
		}
	}
	return pathauthority.Exact{}, false
}

// RequireWritable checks entire physical documents, regardless of key or owner.
func (scope DocumentScope) RequireWritable(registry Registry) error {
	if !scope.initialized || !registry.initialized {
		return fmt.Errorf("qualified document scope and Registry are required")
	}
	return scope.RequireProtectionClear(registry.protections)
}

// RequireProtectionClear accepts authenticated safety rows without requiring
// unrelated claim-path qualification. It grants no permission to clear a row.
func (scope DocumentScope) RequireProtectionClear(protections []PendingProtection) error {
	if !scope.initialized {
		return fmt.Errorf("qualified document scope is required")
	}
	for _, protection := range protections {
		if err := protection.Validate(); err != nil {
			return err
		}
		for _, document := range protection.documents {
			if path, intersects := scope.intersection(document.PathAuthority()); intersects {
				return &ProtectionPendingError{Protection: protection, Path: path}
			}
		}
	}
	return nil
}

// DocumentRegistryView contains only facts relevant to a frozen document scope.
// It is never a complete Registry or a publication candidate.
type DocumentRegistryView struct {
	scope       DocumentScope
	claims      []Claim
	bindings    []DocumentBinding
	protections []PendingProtection
}

// RequireProtectionClear queries retained facts, not their freshness. Reuse at
// an effect gate requires the caller's unchanged document-lease coverage and a
// grammar that cannot change this protection basis.
func (view DocumentRegistryView) RequireProtectionClear() error {
	return view.scope.RequireProtectionClear(view.protections)
}

func (registry Registry) Documents(scope DocumentScope) (DocumentRegistryView, error) {
	if !registry.initialized || !scope.initialized {
		return DocumentRegistryView{}, fmt.Errorf("Registry projection requires a qualified document scope")
	}
	view := DocumentRegistryView{scope: scope}
	for _, claim := range registry.claims {
		if scope.contains(claim.Address().PathAuthority()) {
			view.claims = append(view.claims, claim)
		}
	}
	for _, binding := range registry.bindings {
		if scope.contains(binding.PathAuthority()) {
			view.bindings = append(view.bindings, binding)
		}
	}
	for _, protection := range registry.protections {
		for _, document := range protection.documents {
			if scope.contains(document.PathAuthority()) {
				view.protections = append(view.protections, protection)
				break
			}
		}
	}
	return view, nil
}

func (view DocumentRegistryView) Equal(other DocumentRegistryView) bool {
	return view.scope.Equal(other.scope) && slices.EqualFunc(view.claims, other.claims, Claim.Equal) &&
		slices.EqualFunc(view.bindings, other.bindings, DocumentBinding.Equal) &&
		slices.EqualFunc(view.protections, other.protections, PendingProtection.Equal)
}

func (scope DocumentScope) Matches(before, current Registry) (bool, error) {
	left, err := before.Documents(scope)
	if err != nil {
		return false, err
	}
	right, err := current.Documents(scope)
	if err != nil {
		return false, err
	}
	return left.Equal(right), nil
}
