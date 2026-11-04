package reconcile

import (
	"fmt"
	"sort"

	"github.com/isty2e/daem/internal/topology"
)

// NativeNarrowings returns ownership transitions, not per-output projection rows.
func (result Result) NativeNarrowings() []NativeNarrowingAction {
	return append([]NativeNarrowingAction(nil), result.nativeNarrowings...)
}

func canonicalNativeNarrowings(actions []NativeNarrowingAction, paths []ManagedPathDecision, aggregates []AggregateDecision) ([]NativeNarrowingAction, error) {
	if len(actions) == 0 {
		return nil, nil
	}

	canonical := append([]NativeNarrowingAction(nil), actions...)
	whole := make(map[topology.SubjectID]struct{}, len(canonical))
	keys := make(map[topology.SubjectID]struct{})
	for _, action := range canonical {
		if err := action.Validate(); err != nil {
			return nil, fmt.Errorf("native narrowing: %w", err)
		}
		subject := action.Baseline().Subject()
		if _, duplicate := whole[subject]; duplicate {
			return nil, fmt.Errorf("native narrowing repeats a whole baseline")
		}
		whole[subject] = struct{}{}
		for _, item := range action.DesiredContributions() {
			if _, duplicate := keys[item.SubjectID()]; duplicate {
				return nil, fmt.Errorf("native narrowing repeats a desired key subject")
			}
			keys[item.SubjectID()] = struct{}{}
		}
	}

	for _, path := range paths {
		_, consumes := whole[path.Subject()]
		previous, present := path.PreviousState()
		_, consumesPrevious := whole[previous.Subject()]
		if consumes || present && consumesPrevious {
			return nil, fmt.Errorf("managed path decision also consumes narrowed whole ownership")
		}
	}
	for _, aggregate := range aggregates {
		for _, subject := range aggregate.Subjects() {
			if _, overlaps := keys[subject]; overlaps {
				return nil, fmt.Errorf("aggregate decision also owns a native narrowing key subject")
			}
		}
	}

	sort.Slice(canonical, func(left, right int) bool {
		return topology.CompareSubjectID(canonical[left].Baseline().Subject(), canonical[right].Baseline().Subject()) < 0
	})
	return canonical, nil
}
