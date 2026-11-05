package qualification

import (
	"fmt"
	"slices"

	journalnamespace "github.com/isty2e/daem/internal/effect/journal/namespace"
	"github.com/isty2e/daem/internal/output/ownership"
	"github.com/isty2e/daem/internal/realization/aggregate"
)

func (facts AssessmentFacts) documentWriteIndexes() ([]int, error) {
	records := facts.documents
	if records.Len() != len(facts.stages) {
		return nil, fmt.Errorf("native recovery document facts are incomplete")
	}
	var indexes []int
	for index := range records.Len() {
		loaded, present := records.At(index)
		if !present {
			return nil, fmt.Errorf("native recovery document facts are incomplete")
		}
		group := loaded.Group()
		comparison := group.Comparison()
		if err := comparison.Validate(); err != nil {
			return nil, err
		}
		changed := !comparison.BeforeDocument().Equal(comparison.ExpectedDocument()) ||
			!comparison.BeforeState().SameRootMetadata(comparison.ExpectedState()) || facts.stages[index].restoration.Changed()
		claims := loaded.Claims()
		changed = changed || !slices.EqualFunc(claims.Before(), claims.Prepared(), ownership.Claim.Equal) ||
			!slices.EqualFunc(claims.Prepared(), claims.After(), ownership.Claim.Equal)
		for _, intent := range group.BindingIntents() {
			bindingChanged, err := intent.ChangesBinding()
			if err != nil {
				return nil, err
			}
			changed = changed || bindingChanged
		}
		if changed {
			indexes = append(indexes, index)
		}
	}
	return indexes, nil
}

func (assessment Assessment) RegistryScope() (ownership.DocumentScope, error) {
	if assessment.facts.documents.Len() != assessment.qualified.Len() {
		return ownership.DocumentScope{}, fmt.Errorf("live native namespace qualification is required before freezing scope")
	}
	indexes, err := assessment.facts.documentWriteIndexes()
	if err != nil {
		return ownership.DocumentScope{}, err
	}
	namespaces := make([]journalnamespace.DocumentNamespace, 0, len(indexes))
	for _, index := range indexes {
		namespace, present := assessment.qualified.NamespaceAt(index)
		if !present {
			return ownership.DocumentScope{}, fmt.Errorf("live native namespace qualification is required before freezing scope")
		}
		namespaces = append(namespaces, namespace)
	}
	return journalnamespace.RegistryScope(namespaces)
}

func (facts AssessmentFacts) DocumentWriteAliases() ([][]aggregate.DocumentAddress, error) {
	indexes, err := facts.documentWriteIndexes()
	if err != nil {
		return nil, err
	}
	aliases := make([][]aggregate.DocumentAddress, 0, len(indexes))
	for _, index := range indexes {
		loaded, present := facts.documents.At(index)
		if !present {
			return nil, fmt.Errorf("native recovery document facts are incomplete")
		}
		aliases = append(aliases, loaded.Group().Aliases())
	}
	return aliases, nil
}

func (facts AssessmentFacts) ObservedDocuments(scope ownership.DocumentScope) (ownership.DocumentRegistryView, error) {
	return facts.registry.Documents(scope)
}
