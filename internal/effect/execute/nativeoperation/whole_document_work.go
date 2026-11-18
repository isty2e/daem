package nativeoperation

import (
	"errors"
	"fmt"

	effectpreparation "github.com/isty2e/daem/internal/effect/execute/preparation"
	journalrecord "github.com/isty2e/daem/internal/effect/journal/record"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/reconcile"
)

var ErrOverlappingWholeDocumentWork = errors.New("whole-file and selected effects overlap")

type wholeDocumentGroup struct {
	index        int
	address      aggregate.DocumentAddress
	writes       bool
	retireAbsent bool
}

type wholeDocumentWork struct {
	ordinary     []effectpreparation.ManagedPathEffect
	whole        []effectpreparation.ManagedPathEffect
	mutations    []journalrecord.ManagedPathMutation
	groups       []wholeDocumentGroup
	indexByAlias map[aggregate.DocumentAddress]int
}

// ValidateWholeDocumentWork checks placement using the observed alias groups.
// It does not require live roots or supply publication authority.
func ValidateWholeDocumentWork(preparation effectpreparation.NativeApplyStatePreparation, aliases [][]aggregate.DocumentAddress) error {
	if err := preparation.Validate(); err != nil {
		return err
	}
	_, err := wholeDocumentWorkFor(preparation, aliases)
	return err
}

func wholeDocumentWorkFor(preparation effectpreparation.NativeApplyStatePreparation, aliases [][]aggregate.DocumentAddress) (wholeDocumentWork, error) {
	work := wholeDocumentWork{indexByAlias: make(map[aggregate.DocumentAddress]int)}
	for index, group := range aliases {
		for _, address := range group {
			work.indexByAlias[address] = index
		}
	}

	wholeGroups := make(map[int]struct{})
	registerWhole := func(address aggregate.DocumentAddress, writes, retireAbsent bool) error {
		index, observed := work.indexByAlias[address]
		if !observed {
			return fmt.Errorf("native whole effect has no retained namespace")
		}
		if _, duplicate := wholeGroups[index]; duplicate {
			return fmt.Errorf("native document has multiple whole effects")
		}
		wholeGroups[index] = struct{}{}
		work.groups = append(work.groups, wholeDocumentGroup{index: index, address: address, writes: writes, retireAbsent: retireAbsent})
		return nil
	}

	var captureEffects []effectpreparation.ManagedPathEffect
	for _, effect := range preparation.ManagedPathEffects() {
		if !aggregate.IsAgentConfigDocumentSubject(effect.Subject()) {
			work.ordinary = append(work.ordinary, effect)
			continue
		}
		work.whole = append(work.whole, effect)
		if effect.Kind() != effectpreparation.ManagedPathEffectRetireAbsent {
			captureEffects = append(captureEffects, effect)
			continue
		}
		previous, _ := effect.PreviousState()
		address, err := aggregate.NewDocumentAddress(previous.ConsumerTargets()[0], effect.Scope(), effect.Destination())
		if err != nil {
			return wholeDocumentWork{}, err
		}
		if err := registerWhole(address, false, true); err != nil {
			return wholeDocumentWork{}, err
		}
	}
	mutations, err := effectpreparation.ManagedPathJournalMutations(captureEffects)
	if err != nil {
		return wholeDocumentWork{}, err
	}
	work.mutations = mutations
	for _, mutation := range mutations {
		consumers := mutation.ConsumerTargets()
		if len(consumers) != 1 {
			return wholeDocumentWork{}, fmt.Errorf("native whole effect requires one consumer")
		}
		address, err := aggregate.NewDocumentAddress(consumers[0], mutation.Scope(), mutation.Destination())
		if err != nil {
			return wholeDocumentWork{}, err
		}
		if err := registerWhole(address, mutation.Kind() != journalrecord.CaptureRecord, false); err != nil {
			return wholeDocumentWork{}, err
		}
	}

	checkSelected := func(address aggregate.DocumentAddress) error {
		index, observed := work.indexByAlias[address]
		if _, whole := wholeGroups[index]; observed && whole {
			return fmt.Errorf("%w at %q", ErrOverlappingWholeDocumentWork, address.AggregateRoot())
		}
		return nil
	}
	for _, effect := range preparation.AggregateEffects() {
		if err := checkSelected(effect.DocumentAddress()); err != nil {
			return wholeDocumentWork{}, err
		}
	}
	for _, action := range preparation.Narrowings() {
		if _, present := action.KeyPlan(); !present {
			continue
		}
		address, err := narrowingDocumentAddress(action)
		if err != nil {
			return wholeDocumentWork{}, err
		}
		if err := checkSelected(address); err != nil {
			return wholeDocumentWork{}, err
		}
	}
	for _, edit := range preparation.SequenceEdits() {
		if err := checkSelected(edit.DocumentAddress()); err != nil {
			return wholeDocumentWork{}, err
		}
	}
	return work, nil
}

func narrowingDocumentAddress(action reconcile.NativeNarrowingAction) (aggregate.DocumentAddress, error) {
	placement, _ := action.SourceContract().AgentConfigPlacement()
	return aggregate.NewDocumentAddress(placement.Target(), placement.Scope(), placement.Destination())
}
