package recoveryoperation

import (
	"context"
	"fmt"

	"github.com/isty2e/daem/internal/assurance/pathauthority"
	journaldocument "github.com/isty2e/daem/internal/effect/journal/document"
	journalrecord "github.com/isty2e/daem/internal/effect/journal/record"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/output/ownership"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/profile/configuration"
)

func journalHasOwnedSource(journal journalrecord.Native) (bool, error) {
	documents, err := journal.Documents()
	if err != nil {
		return false, err
	}
	for _, document := range documents {
		for _, alias := range document.Aliases() {
			if binding, bound := alias.Binding(); bound && configuration.NativeRootIsOwnedPoint(binding.RootReference()) {
				return true, nil
			}
		}
	}
	return false, nil
}

func selectOwnedSource(ctx context.Context, journal journalrecord.Native, records journalrecord.LoadedDocuments, registry ownership.Registry, resolver func(output.Destination) (string, error), after bool) (string, pathauthority.Exact, error) {
	indexes, err := journaldocument.IndexSnapshotsForClaims(ctx, journal.Before(), journal.After(), journaldocument.NativeClaims)
	if err != nil {
		return "", pathauthority.Exact{}, err
	}
	phase := indexes.Before()
	if after {
		phase = indexes.After()
	}
	var expected pathauthority.Exact
	var destination output.Destination
	for index := range records.Len() {
		loaded, present := records.At(index)
		if !present {
			return "", pathauthority.Exact{}, fmt.Errorf("owned source requires complete recorded documents")
		}
		for _, route := range loaded.Group().Routes() {
			if !route.OwnedSource() {
				continue
			}
			if expected.Validate() == nil {
				return "", pathauthority.Exact{}, fmt.Errorf("owned recovery selects more than one physical source")
			}
			destination = route.Address().AggregateRoot()
			claims := loaded.Claims().Before()
			if after {
				claims = loaded.Claims().After()
			}
			footprints, err := phase.Footprints(ctx, []aggregate.DocumentAddress{route.Address()})
			if err != nil {
				return "", pathauthority.Exact{}, err
			}
			matched := 0
			for _, claim := range claims {
				if _, member := footprints[claim.Address().Footprint()]; !member {
					continue
				}
				if matched == 0 {
					expected = claim.Address().PathAuthority()
				}
				matched++
				actual, found := registry.Exact(claim.Address())
				if !found || !actual.Equal(claim) || !claim.Owner().ExactEqual(journal.Owner()) || !claim.Address().PathAuthority().Equal(expected) {
					return "", pathauthority.Exact{}, ownership.ErrClaimMissing
				}
			}
			if matched == 0 || matched != len(footprints) {
				return "", pathauthority.Exact{}, ownership.ErrClaimMissing
			}
		}
	}
	if err := expected.Validate(); err != nil {
		return "", pathauthority.Exact{}, fmt.Errorf("owned recovery has no exact source claims")
	}
	var before ownership.DocumentBindingValue
	foundIntent := false
	for index := range records.Len() {
		loaded, _ := records.At(index)
		for _, intent := range loaded.Group().BindingIntents() {
			if intent.Destination() == destination {
				if foundIntent {
					return "", pathauthority.Exact{}, fmt.Errorf("owned source repeats its binding intent")
				}
				before, foundIntent = intent.Before(), true
			}
		}
	}
	actual, bound := registry.DocumentBinding(journal.Owner().Key(), destination)
	if !foundIntent && bound {
		return "", pathauthority.Exact{}, fmt.Errorf("bound source requires its recorded retained binding intent")
	}
	if retained, had := before.Get(); had {
		if !bound || !actual.Equal(retained) || !actual.PathAuthority().Equal(expected) {
			return "", pathauthority.Exact{}, fmt.Errorf("owned source differs from its current retained binding")
		}
		return actual.PathAuthority().Key(), expected, nil
	}
	if bound || resolver == nil {
		return "", pathauthority.Exact{}, fmt.Errorf("legacy source requires an unbound independently selected literal destination")
	}
	path, err := resolver(destination)
	return path, expected, err
}
