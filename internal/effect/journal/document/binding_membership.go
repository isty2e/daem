package document

import (
	"context"
	"fmt"

	"github.com/isty2e/daem/internal/assurance/stateauthority"
	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/output/ownership"
	"github.com/isty2e/daem/internal/realization/aggregate"
	topologyprojection "github.com/isty2e/daem/internal/topology/projection"
)

// This operation-wide join checks state membership, not physical path binding
// or registry phase authority. Untouched destinations need no synthetic group.
func (indexes SnapshotIndexes) ValidateBoundBindingMembership(ctx context.Context, owner stateauthority.Authority, routes []AliasRoute, intents []ownership.DocumentBindingIntent) error {
	aliases, err := AliasRouteAddresses(ctx, routes)
	if err != nil {
		return err
	}
	return indexes.validateNativeBindingMembership(ctx, owner, aliases, intents, routes...)
}

func (indexes SnapshotIndexes) validateNativeBindingMembership(ctx context.Context, owner stateauthority.Authority, aliases []aggregate.DocumentAddress, intents []ownership.DocumentBindingIntent, routes ...AliasRoute) error {
	if ctx == nil {
		return fmt.Errorf("native binding membership context is required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := owner.Validate(); err != nil {
		return err
	}
	if len(aliases) == 0 {
		if len(intents) != 0 {
			return fmt.Errorf("document binding intents require declared aliases")
		}
		return nil
	}
	addresses, err := AliasSet(ctx, aliases)
	if err != nil {
		return err
	}
	destinations := make(map[output.Destination]struct{}, len(addresses))
	for address := range addresses {
		destinations[address.AggregateRoot()] = struct{}{}
	}
	before, err := indexes.before.NativeDestinations(ctx)
	if err != nil {
		return err
	}
	after, err := indexes.after.NativeDestinations(ctx)
	if err != nil {
		return err
	}
	mcp := indexes.mcpBindingMembership(routes)
	for _, carrier := range mcp {
		if carrier.source && !carrier.before {
			return fmt.Errorf("owned MCP source requires before MCP state")
		}
	}

	seen := make(map[output.Destination]struct{}, len(intents))
	for index, intent := range intents {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := intent.Validate(); err != nil {
			return fmt.Errorf("document binding intent[%d]: %w", index, err)
		}
		if !owner.Equal(intent.Owner()) {
			return fmt.Errorf("document binding intent[%d] has a different operation owner", index)
		}
		destination := intent.Destination()
		if _, declared := destinations[destination]; !declared {
			return fmt.Errorf("document binding intent[%d] has no declared alias", index)
		}
		if _, duplicate := seen[destination]; duplicate {
			return fmt.Errorf("document binding intent[%d] repeats an owner/destination key", index)
		}
		seen[destination] = struct{}{}
		_, nativeBefore := before[destination]
		_, nativeAfter := after[destination]
		_, bindingBefore := intent.Before().Get()
		if carrier, present := mcp[destination]; present && (carrier.before || carrier.after) {
			if carrier.before && !bindingBefore && !carrier.source {
				return fmt.Errorf("legacy MCP binding acquisition requires its owned-source route")
			}
			nativeBefore = nativeBefore || carrier.before && bindingBefore
			nativeAfter = nativeAfter || carrier.after
		}
		if !nativeBefore && !nativeAfter {
			return fmt.Errorf("document binding intent[%d] has no native state membership", index)
		}
		if bindingBefore != nativeBefore {
			return fmt.Errorf("document binding intent[%d] before does not match native membership", index)
		}
		if value, known := intent.KnownAfter(); known {
			_, bindingAfter := value.Get()
			if bindingAfter != nativeAfter {
				return fmt.Errorf("document binding intent[%d] after does not match native membership", index)
			}
		} else if !nativeAfter {
			return fmt.Errorf("document binding intent[%d] provisional acquisition does not match native membership", index)
		}
	}
	for destination := range destinations {
		if err := ctx.Err(); err != nil {
			return err
		}
		_, nativeBefore := before[destination]
		_, nativeAfter := after[destination]
		carrier := mcp[destination]
		needsCarrierBinding := carrier.source && carrier.current && (carrier.before || carrier.after)
		if _, present := seen[destination]; !present && (nativeBefore || nativeAfter || needsCarrierBinding) {
			return fmt.Errorf("native document membership requires its binding intent")
		}
	}
	return ctx.Err()
}

type mcpBindingPhases struct {
	before, after   bool
	source, current bool
}

func (indexes SnapshotIndexes) mcpBindingMembership(routes []AliasRoute) map[output.Destination]mcpBindingPhases {
	result := make(map[output.Destination]mcpBindingPhases)
	for _, route := range routes {
		if !route.OwnedSource() && !route.CurrentCarrier() {
			continue
		}
		destination := route.Address().AggregateRoot()
		phases := result[destination]
		phases.source = phases.source || route.OwnedSource()
		phases.current = phases.current || route.CurrentCarrier()
		for side, index := range []SnapshotIndex{indexes.before, indexes.after} {
			for _, row := range index.aggregates {
				contract := row.Contribution().Contract()
				if contract.Address().Document() != route.Address() {
					continue
				}
				if _, mcp := aggregate.MCPPlacementForCodec(aggregate.MCPPlacementID(contract.Address().PlacementID()), contract.CodecContractID()); mcp {
					phases.before = phases.before || side == 0
					phases.after = phases.after || side == 1
				}
			}
		}
		result[destination] = phases
	}
	return result
}

func (index SnapshotIndex) NativeDestinations(ctx context.Context) (map[output.Destination]struct{}, error) {
	destinations := make(map[output.Destination]struct{})
	for _, row := range index.paths {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		id, entityBacked := topologyprojection.EntityID(row.Subject())
		if entityBacked && id.Kind() == entity.KindHostConfig {
			destinations[row.Destination()] = struct{}{}
		}
	}
	for _, row := range index.aggregates {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		contribution := row.Contribution()
		if _, native := aggregate.HostConfigSyntaxForCodec(contribution.CodecContractID()); native {
			destinations[contribution.AggregateRoot()] = struct{}{}
		}
	}
	return destinations, nil
}
