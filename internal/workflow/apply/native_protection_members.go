package apply

import (
	"context"
	"fmt"

	"github.com/isty2e/daem/internal/effect/execute"
	"github.com/isty2e/daem/internal/effect/journal"
	"github.com/isty2e/daem/internal/realization/aggregate"
)

func nativeProtectionMembers(ctx context.Context, observed journal.NativeDocumentObservationBatch, prefix execute.NativeApplyStatePreparation, members []journal.NativeDocumentMember) ([]journal.NativeDocumentMember, error) {
	if ctx == nil {
		return nil, fmt.Errorf("protection member context is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := prefix.Validate(); err != nil {
		return nil, err
	}
	if _, err := observed.RegistryScope(); err != nil {
		return nil, err
	}
	if !observed.BeforeState().Equal(prefix.BeforeState()) {
		return nil, fmt.Errorf("protection members differ from the positive prefix baseline")
	}
	presence := make(map[aggregate.DocumentAddress]bool)
	for _, effect := range prefix.AggregateEffects() {
		presence[effect.DocumentAddress()] = effect.Rendered().Document().Exists()
	}
	var result []journal.NativeDocumentMember
	for _, member := range members {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		aliases := member.Namespace().Addresses()
		original, _, found := observed.Document(aliases[0])
		if !found {
			return nil, fmt.Errorf("protection member has no retained original document")
		}
		expectedPresent := original.Exists()
		hasPrefix := false
		for _, alias := range aliases {
			if exists, changed := presence[alias]; changed {
				if hasPrefix && expectedPresent != exists {
					return nil, fmt.Errorf("positive prefix aliases disagree on document presence")
				}
				expectedPresent, hasPrefix = exists, true
			}
		}
		if expectedPresent {
			result = append(result, member)
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return result, nil
}
