package journal

import (
	"cmp"
	"fmt"
	"slices"

	"github.com/isty2e/daem/internal/effect/journal/recovery"
)

func (record nativeJournalRecord) validateRemovalCoverage() error {
	physical, err := canonicalRecoveryRemovalIntents(record.RemovalIntents)
	if err != nil {
		return err
	}
	var demands []recovery.RemovalDemand
	for _, intent := range physical {
		demands = append(demands, intent.Demand())
	}
	for _, group := range record.DocumentGroups {
		if group.Removal == nil {
			continue
		}
		removal, err := group.Removal.canonical()
		if err != nil {
			return err
		}
		if removal.Demands().Len() > recovery.MaximumRemovalIntents-len(demands) {
			return fmt.Errorf("complete native/ordinary removal relations exceed the operation maximum")
		}
		demands = append(demands, removal.Demands().Demands()...)
		intent, err := removal.PhysicalIntent()
		if err != nil {
			return err
		}
		physical = append(physical, intent)
	}
	if _, err := recovery.NewRemovalDemandSet(demands); err != nil {
		return fmt.Errorf("complete native/ordinary removal relations: %w", err)
	}
	slices.SortFunc(physical, func(left, right recovery.RemovalIntent) int {
		if ordering := cmp.Compare(left.Scope(), right.Scope()); ordering != 0 {
			return ordering
		}
		return cmp.Compare(left.Destination().String(), right.Destination().String())
	})
	return validateRecoveryRemovalIntents(physical)
}
