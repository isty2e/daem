package qualification

import (
	"fmt"
	"slices"

	"github.com/isty2e/daem/internal/assurance/pathauthority"
	"github.com/isty2e/daem/internal/output/ownership"
	"github.com/isty2e/daem/internal/realization/aggregate"
)

func (facts AssessmentFacts) DocumentWriteIndexes() ([]int, error) {
	records := facts.documents
	if len(facts.demand.live) != len(facts.stages) || facts.demand.count != records.Len() {
		return nil, fmt.Errorf("native recovery document facts are incomplete")
	}
	var indexes []int
	for index, recordIndex := range facts.demand.live {
		loaded, present := records.At(recordIndex)
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
	if len(assessment.facts.demand.live) != assessment.qualified.Len() {
		return ownership.DocumentScope{}, fmt.Errorf("live native namespace qualification is required before freezing scope")
	}
	indexes, err := assessment.facts.DocumentWriteIndexes()
	if err != nil {
		return ownership.DocumentScope{}, err
	}
	var exact []pathauthority.Exact
	var missing []pathauthority.Provisional
	for _, index := range indexes {
		namespace, present := assessment.qualified.NamespaceAt(index)
		if !present {
			return ownership.DocumentScope{}, fmt.Errorf("live native namespace qualification is required before freezing scope")
		}
		if physical, known := namespace.Observation().Exact(); known {
			exact = append(exact, physical)
		} else if candidate, known := namespace.Observation().Provisional(); known {
			missing = append(missing, candidate)
		} else {
			return ownership.DocumentScope{}, fmt.Errorf("native Registry scope has no namespace observation")
		}
	}
	for recordIndex := range assessment.facts.documents.Len() {
		if slices.Contains(assessment.facts.demand.live, recordIndex) || slices.Contains(assessment.facts.demand.retired, recordIndex) {
			continue
		}
		loaded, _ := assessment.facts.documents.At(recordIndex)
		for _, claim := range loaded.Claims().Before() {
			exact = append(exact, claim.Address().PathAuthority())
		}
	}
	for recordIndex := range assessment.facts.documents.Len() {
		loaded, _ := assessment.facts.documents.At(recordIndex)
		for _, intent := range loaded.Group().BindingIntents() {
			after, ready := intent.KnownAfter()
			actual := ownership.NoDocumentBinding()
			if binding, present := assessment.facts.registry.DocumentBinding(intent.Owner().Key(), intent.Destination()); present {
				actual, _ = ownership.PresentDocumentBinding(binding)
			}
			if ready && actual.Equal(after) {
				continue
			}
			for _, value := range []ownership.DocumentBindingValue{intent.Before(), after} {
				if binding, present := value.Get(); present {
					exact = append(exact, binding.PathAuthority())
				}
			}
		}
	}
	return ownership.NewDocumentScope(exact, missing)
}

func (facts AssessmentFacts) DocumentWriteAliases() ([][]aggregate.DocumentAddress, error) {
	indexes, err := facts.DocumentWriteIndexes()
	if err != nil {
		return nil, err
	}
	aliases := make([][]aggregate.DocumentAddress, 0, len(indexes))
	for _, index := range indexes {
		loaded, present := facts.documents.At(facts.demand.live[index])
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
