package journal

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
func (indexes documentSnapshotIndexes) validateNativeBindingMembership(ctx context.Context, owner stateauthority.Authority, aliases []aggregate.DocumentAddress, intents []ownership.DocumentBindingIntent) error {
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
	addresses, err := documentAliasSet(ctx, aliases)
	if err != nil {
		return err
	}
	destinations := make(map[output.Destination]struct{}, len(addresses))
	for address := range addresses {
		destinations[address.AggregateRoot()] = struct{}{}
	}
	before, err := indexes.before.nativeDestinations(ctx)
	if err != nil {
		return err
	}
	after, err := indexes.after.nativeDestinations(ctx)
	if err != nil {
		return err
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
		if !nativeBefore && !nativeAfter {
			return fmt.Errorf("document binding intent[%d] has no native state membership", index)
		}
		_, bindingBefore := intent.Before().Get()
		if bindingBefore != nativeBefore {
			return fmt.Errorf("document binding intent[%d] before does not match native membership", index)
		}
		if value, known := intent.KnownAfter(); known {
			_, bindingAfter := value.Get()
			if bindingAfter != nativeAfter {
				return fmt.Errorf("document binding intent[%d] after does not match native membership", index)
			}
		} else if nativeBefore || !nativeAfter {
			return fmt.Errorf("document binding intent[%d] provisional acquisition does not match native membership", index)
		}
	}
	for destination := range destinations {
		if err := ctx.Err(); err != nil {
			return err
		}
		_, nativeBefore := before[destination]
		_, nativeAfter := after[destination]
		if _, present := seen[destination]; !present && (nativeBefore || nativeAfter) {
			return fmt.Errorf("native document membership requires its binding intent")
		}
	}
	return ctx.Err()
}

func (index documentSnapshotIndex) nativeDestinations(ctx context.Context) (map[output.Destination]struct{}, error) {
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
