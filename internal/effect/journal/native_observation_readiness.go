package journal

import (
	"context"
	"errors"
	"fmt"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/pathauthority"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/output/ownership"
)

// BeforeState retains the supplied complete baseline, not a freshness proof.
func (batch NativeDocumentObservationBatch) BeforeState() durable.Snapshot {
	return batch.before
}

// ValidateBefore checks persisted ownership, not source freshness or a write grant.
func (batch NativeDocumentObservationBatch) ValidateBefore(ctx context.Context) error {
	if ctx == nil || !batch.initialized {
		return fmt.Errorf("native observation batch and context are required")
	}
	if err := errors.Join(ctx.Err(), batch.budget.RequireCapturePending()); err != nil {
		return err
	}
	before, err := indexDocumentSnapshot(ctx, batch.before)
	if err != nil {
		return err
	}
	native, err := before.nativeDestinations(ctx)
	if err != nil {
		return err
	}
	owned := make(map[string][]ownership.Claim)
	for _, claim := range batch.registry.Claims() {
		if err := ctx.Err(); err != nil {
			return err
		}
		if claim.OwnedBy(batch.owner) {
			owned[claim.Address().Path()] = append(owned[claim.Address().Path()], claim)
		}
	}
	input := nativeCaptureOwnershipInput{owner: batch.owner}
	for _, namespace := range batch.namespaces {
		footprints, err := before.documentFootprints(ctx, namespace.Addresses())
		if err != nil {
			return err
		}
		if _, _, err := nativeDocumentClaimBaseline(namespace, owned, footprints); err != nil {
			return err
		}
		if _, err := input.documentBindings(ctx, namespace, batch.registry, native, native); err != nil {
			return err
		}
	}
	return errors.Join(ctx.Err(), batch.budget.RequireCapturePending())
}

func nativeDocumentClaimBaseline(namespace NativeDocumentNamespace, owned map[string][]ownership.Claim, before map[output.Footprint]struct{}) (pathauthority.Exact, []ownership.Claim, error) {
	exact, known := namespace.observation.Exact()
	if !known {
		missing, provisional := namespace.observation.Provisional()
		if !provisional {
			return pathauthority.Exact{}, nil, fmt.Errorf("native history requires a current namespace observation")
		}
		previous := owned[missing.CandidateKey()]
		if len(before) == 0 && len(previous) == 0 {
			return pathauthority.Exact{}, nil, nil
		}
		if len(previous) == 0 || !missing.MatchesMissingExact(previous[0].Address().PathAuthority()) {
			return pathauthority.Exact{}, nil, fmt.Errorf("native history differs from the missing namespace")
		}
		exact = previous[0].Address().PathAuthority()
	}
	claims := owned[exact.Key()]
	if !sameDocumentClaimFootprints(before, claims) {
		return pathauthority.Exact{}, nil, fmt.Errorf("native capture before membership differs from current owned claims")
	}
	for _, claim := range claims {
		if !claim.Address().PathAuthority().Equal(exact) || claim.State() != ownership.ClaimActive {
			return pathauthority.Exact{}, nil, fmt.Errorf("native capture requires active claims at the captured namespace")
		}
	}
	return exact, claims, nil
}
