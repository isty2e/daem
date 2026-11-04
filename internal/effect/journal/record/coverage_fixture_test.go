package record

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
	for {
		demand, present, err := session.next(ctx)
		if err != nil {
			return nil, err
		}
		if !present {
			return session.finish()
		}
		if resolver == nil {
			return nil, fmt.Errorf("destination resolver is required")
		}
		path, err := resolver(demand.Destination())
		if err != nil {
			return nil, err
		}
		observation, err := mutation.ObserveDirectoryEntryAuthority(path)
		if err != nil {
			return nil, err
		}
		if err := session.bind(observation); err != nil {
			return nil, err
		}
	}
}
