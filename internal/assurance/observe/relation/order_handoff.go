package relation

import (
	"fmt"

	hostrelation "github.com/isty2e/daem/internal/realization/relation"
)

type OrderHandoffSpec struct {
	Before CorrelationKey
	After  CorrelationKey
}

// OrderHandoffs carries declaration handoffs, not row equality or removal
// authority. Native normalizers still select exact sources in their own context.
type OrderHandoffs struct {
	pairs map[CorrelationKey]map[CorrelationKey]struct{}
}

func NewOrderHandoffs(input []OrderHandoffSpec) (OrderHandoffs, error) {
	events := OrderHandoffs{pairs: make(map[CorrelationKey]map[CorrelationKey]struct{})}
	for _, pair := range input {
		if err := pair.Before.Validate(); err != nil {
			return OrderHandoffs{}, err
		}
		if err := pair.After.Validate(); err != nil {
			return OrderHandoffs{}, err
		}
		if pair.Before == pair.After || pair.Before.Subject().Namespace() != pair.After.Subject().Namespace() {
			return OrderHandoffs{}, fmt.Errorf("order handoff requires distinct expectations in the same carrier family")
		}
		after := events.pairs[pair.Before]
		if after == nil {
			after = make(map[CorrelationKey]struct{})
			events.pairs[pair.Before] = after
		}
		if _, repeated := after[pair.After]; repeated {
			return OrderHandoffs{}, fmt.Errorf("order handoff pair is repeated")
		}
		after[pair.After] = struct{}{}
	}
	return events, nil
}

func (events OrderHandoffs) AllowsRetirement(key CorrelationKey) bool {
	_, present := events.pairs[key]
	return present
}

func (events OrderHandoffs) AllowsPair(before, after CorrelationKey) bool {
	_, present := events.pairs[before][after]
	return present
}

func (events OrderHandoffs) Empty() bool { return len(events.pairs) == 0 }

func (events OrderHandoffs) ValidateDesired(keys []CorrelationKey) error {
	desired := make(map[CorrelationKey]struct{}, len(keys))
	for _, key := range keys {
		if err := key.Validate(); err != nil {
			return err
		}
		desired[key] = struct{}{}
	}
	for before, after := range events.pairs {
		if _, retained := desired[before]; retained {
			return fmt.Errorf("order handoff cannot retire an unchanged desired expectation")
		}
		for key := range after {
			if _, present := desired[key]; !present {
				return fmt.Errorf("order handoff acquisition is outside its exact desired selection")
			}
		}
	}
	return nil
}

func (events OrderHandoffs) validateConstraint(constraint hostrelation.RelationOrderConstraint) error {
	for _, after := range events.pairs {
		for key := range after {
			present := false
			for _, member := range constraint.Members() {
				present = present || member.Subject() == key.Subject()
			}
			if !present {
				return fmt.Errorf("order handoff acquisition is outside its desired class")
			}
		}
	}
	return nil
}

func (events OrderHandoffs) Equal(other OrderHandoffs) bool {
	if len(events.pairs) != len(other.pairs) {
		return false
	}
	for before, after := range events.pairs {
		matched := other.pairs[before]
		if len(after) != len(matched) {
			return false
		}
		for key := range after {
			if _, present := matched[key]; !present {
				return false
			}
		}
	}
	return true
}

func (events OrderHandoffs) WithoutRetiring(retired []CorrelationKey) OrderHandoffs {
	removed := make(map[CorrelationKey]struct{}, len(retired))
	for _, key := range retired {
		removed[key] = struct{}{}
	}
	remaining := OrderHandoffs{pairs: make(map[CorrelationKey]map[CorrelationKey]struct{}, len(events.pairs))}
	for key, after := range events.pairs {
		if _, remove := removed[key]; !remove {
			remaining.pairs[key] = after
		}
	}
	return remaining
}
