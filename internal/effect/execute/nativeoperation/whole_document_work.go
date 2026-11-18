package nativeoperation

import (
	"fmt"

	effectpreparation "github.com/isty2e/daem/internal/effect/execute/preparation"
	journalrecord "github.com/isty2e/daem/internal/effect/journal/record"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/reconcile"
)

type WholeDocumentConflictKind string

const (
	DuplicateWholeDocumentWork   WholeDocumentConflictKind = "multiple whole-file operations"
	OverlappingWholeDocumentWork WholeDocumentConflictKind = "whole-file and selected operations"
	AbsentWholeRetirementAndKeys WholeDocumentConflictKind = "whole-file retirement and key operations"
	WholeDocumentRemovalAndKeys  WholeDocumentConflictKind = "whole-file removal and key operations"
)

type WholeDocumentConflict struct {
	Kind    WholeDocumentConflictKind
	address aggregate.DocumentAddress
}

func (conflict *WholeDocumentConflict) Error() string {
	return fmt.Sprintf("%s resolve to the same document at %q", conflict.Kind, conflict.address.AggregateRoot())
}

type wholeDocumentAction uint8

const (
	recordWholeDocument wholeDocumentAction = iota
	writeWholeDocument
	removeWholeDocument
	retireAbsentWholeDocument
)

func (action wholeDocumentAction) writesFile() bool {
	return action == writeWholeDocument || action == removeWholeDocument
}

type wholeDocumentGroup struct {
	index   int
	address aggregate.DocumentAddress
	action  wholeDocumentAction
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

	wholeGroups := make(map[int]wholeDocumentAction)
	registerWhole := func(address aggregate.DocumentAddress, action wholeDocumentAction) error {
		index, observed := work.indexByAlias[address]
		if !observed {
			return fmt.Errorf("native whole effect has no retained namespace")
		}
		if _, duplicate := wholeGroups[index]; duplicate {
			return &WholeDocumentConflict{Kind: DuplicateWholeDocumentWork, address: address}
		}
		wholeGroups[index] = action
		work.groups = append(work.groups, wholeDocumentGroup{index: index, address: address, action: action})
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
		if err := registerWhole(address, retireAbsentWholeDocument); err != nil {
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
		action := writeWholeDocument
		switch mutation.Kind() {
		case journalrecord.CaptureRecord:
			action = recordWholeDocument
		case journalrecord.CaptureRemove:
			action = removeWholeDocument
		}
		if err := registerWhole(address, action); err != nil {
			return wholeDocumentWork{}, err
		}
	}

	checkSelected := func(address aggregate.DocumentAddress, agentConfigKeys bool) error {
		index, observed := work.indexByAlias[address]
		action, whole := wholeGroups[index]
		if !observed || !whole {
			return nil
		}
		kind := OverlappingWholeDocumentWork
		if agentConfigKeys {
			switch action {
			case retireAbsentWholeDocument:
				kind = AbsentWholeRetirementAndKeys
			case removeWholeDocument:
				kind = WholeDocumentRemovalAndKeys
			}
		}
		return &WholeDocumentConflict{Kind: kind, address: address}
	}
	for _, effect := range preparation.AggregateEffects() {
		_, agentConfigKeys := aggregate.AgentConfigSyntaxForCodec(effect.CodecContractID())
		if err := checkSelected(effect.DocumentAddress(), agentConfigKeys); err != nil {
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
		if err := checkSelected(address, false); err != nil {
			return wholeDocumentWork{}, err
		}
	}
	for _, edit := range preparation.SequenceEdits() {
		if err := checkSelected(edit.DocumentAddress(), false); err != nil {
			return wholeDocumentWork{}, err
		}
	}
	return work, nil
}

func narrowingDocumentAddress(action reconcile.NativeNarrowingAction) (aggregate.DocumentAddress, error) {
	placement, _ := action.SourceContract().AgentConfigPlacement()
	return aggregate.NewDocumentAddress(placement.Target(), placement.Scope(), placement.Destination())
}
