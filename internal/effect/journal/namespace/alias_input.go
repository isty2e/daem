package namespace

import (
	"context"
	"fmt"

	journaldocument "github.com/isty2e/daem/internal/effect/journal/document"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/profile/configuration"
)

// RecordedAlias supplies location facts admitted by the record-version owner.
// Its location still requires an independently selected matching root.
type RecordedAlias struct {
	Address  aggregate.DocumentAddress
	Location configuration.AliasLocation
	Binding  configuration.AliasBinding
}

type aliasInput struct {
	address        aggregate.DocumentAddress
	legacyLocation configuration.AliasLocation
	binding        configuration.AliasBinding
}

func (input aliasInput) location() configuration.AliasLocation {
	if input.binding != (configuration.AliasBinding{}) {
		return input.binding.Location()
	}
	return input.legacyLocation
}

func (input aliasInput) validateBinding() error {
	address := input.address
	if input.binding == (configuration.AliasBinding{}) {
		return validateNativeDocumentRootRole(address, address.AggregateRoot().RootRole())
	}
	binding := input.binding
	_, err := configuration.AdmitRecordedAliasBinding(configuration.AliasInput{
		Target: address.Target(), Scope: address.Scope(), Destination: address.AggregateRoot(),
	}, binding.RootReference(), binding.Coordinate(), binding.PolicyID())
	return err
}

func currentAliasInputs(ctx context.Context, addresses []aggregate.DocumentAddress) ([]aliasInput, error) {
	if _, err := journaldocument.AliasSet(ctx, addresses); err != nil {
		return nil, err
	}
	inputs := make([]aliasInput, 0, len(addresses))
	for _, address := range addresses {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		binding, err := configuration.RootBindingForAlias(configuration.AliasInput{Target: address.Target(), Scope: address.Scope(), Destination: address.AggregateRoot()})
		if err != nil {
			return nil, err
		}
		inputs = append(inputs, aliasInput{address: address, binding: binding})
	}
	return inputs, nil
}

func recordedAliasInputs(ctx context.Context, aliases []RecordedAlias) ([]aliasInput, error) {
	inputs := make([]aliasInput, 0, len(aliases))
	for _, alias := range aliases {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if alias.Binding != (configuration.AliasBinding{}) {
			if alias.Location != (configuration.AliasLocation{}) || alias.Binding.Validate() != nil {
				return nil, fmt.Errorf("recorded alias must supply exactly one admitted binding")
			}
			inputs = append(inputs, aliasInput{address: alias.Address, binding: alias.Binding})
		} else {
			inputs = append(inputs, aliasInput{address: alias.Address, legacyLocation: alias.Location})
		}
	}
	return inputs, nil
}

func validateAliasInputs(ctx context.Context, inputs []aliasInput) error {
	addresses := make([]aggregate.DocumentAddress, 0, len(inputs))
	routes := make([]journaldocument.AliasRoute, 0, len(inputs))
	for _, input := range inputs {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := input.location().Validate(); err != nil {
			return err
		}
		if input.binding != (configuration.AliasBinding{}) && input.legacyLocation != (configuration.AliasLocation{}) {
			return fmt.Errorf("native alias supplies both a complete binding and an unversioned-policy location")
		}
		addresses = append(addresses, input.address)
		if input.binding != (configuration.AliasBinding{}) {
			route, err := journaldocument.NewAliasRoute(input.address, input.binding)
			if err != nil {
				return err
			}
			routes = append(routes, route)
		}
	}
	if len(routes) == len(inputs) && len(routes) != 0 {
		_, err := journaldocument.AliasRouteAddresses(ctx, routes)
		return err
	}
	_, err := journaldocument.AliasSet(ctx, addresses)
	return err
}
