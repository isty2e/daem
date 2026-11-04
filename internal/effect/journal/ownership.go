package journal

import (
	"context"
	"fmt"

	"github.com/isty2e/daem/internal/effect/mutation"
	ownershipmutation "github.com/isty2e/daem/internal/effect/mutation/ownership"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/output/ownership"
)

func bindRecoveryClaimCoverage(ctx context.Context, entries []recoveryEntry, transitions []ownershipmutation.ClaimTransition, intents []ownership.ProvisionalAcquireIntent, resolver func(output.Destination) (string, error)) ([]recoveryEntry, error) {
	session, err := newClaimCoverageSession(entries, transitions, intents)
	if err != nil {
		return nil, err
	}
	if err := captureClaimAuthorities(ctx, resolver, session.next, session.bind); err != nil {
		return nil, err
	}
	return session.finish()
}

func captureClaimAuthorities(
	ctx context.Context,
	resolver func(output.Destination) (string, error),
	next func(context.Context) (claimAuthorityDemand, bool, error),
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
		physical, err := resolver(demand.destination)
		if err != nil {
			return fmt.Errorf("recovery entries[%d] resolve ownership path: %w", demand.index, err)
		}
		observation, err := mutation.ObserveDirectoryEntryAuthority(physical)
		if err != nil {
			return fmt.Errorf("recovery entries[%d] canonicalize ownership path: %w", demand.index, err)
		}
		if err := bind(observation); err != nil {
			return err
		}
	}
	return nil
}
