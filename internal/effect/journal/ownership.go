package journal

import (
	"context"
	"fmt"

	journalrecord "github.com/isty2e/daem/internal/effect/journal/record"

	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/output"
)

func captureClaimAuthorities(
	ctx context.Context,
	resolver func(output.Destination) (string, error),
	next func(context.Context) (journalrecord.ClaimAuthorityDemand, bool, error),
	bind func(mutation.DirectoryEntryAuthorityObservation) error,
) error {
	for {
		demand, present, err := next(ctx)
		if err != nil {
			return err
		}
		if !present {
			break
		}
		if resolver == nil {
			return fmt.Errorf("ownership coverage destination resolver is required")
		}
		physical, err := resolver(demand.Destination())
		if err != nil {
			return fmt.Errorf("recovery entries[%d] resolve ownership path: %w", demand.Index(), err)
		}
		observation, err := mutation.ObserveDirectoryEntryAuthority(physical)
		if err != nil {
			return fmt.Errorf("recovery entries[%d] canonicalize ownership path: %w", demand.Index(), err)
		}
		if err := bind(observation); err != nil {
			return err
		}
	}
	return nil
}
