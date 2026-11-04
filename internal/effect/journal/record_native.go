package journal

import (
	"fmt"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	ownershipmutation "github.com/isty2e/daem/internal/effect/mutation/ownership"
	"github.com/isty2e/daem/internal/output/ownership"
)

func (journal nativeJournalSnapshots) requireNativeValue() error {
	if journal.record.Version != nativeRecoveryJournalVersion {
		return fmt.Errorf("native journal record is required")
	}
	return nil
}

// Owner returns the operation owner, or its zero value for a zero native value.
func (journal nativeJournalSnapshots) Owner() stateauthority.Authority {
	return journal.owner
}

// Before returns the complete Before snapshot, or its zero value for zero Native.
func (journal nativeJournalSnapshots) Before() durable.Snapshot {
	return journal.before
}

// After returns the complete After snapshot, or its zero value for zero Native.
func (journal nativeJournalSnapshots) After() durable.Snapshot {
	return journal.after
}

// OperationID returns the operation identity, or an empty string for a zero value.
func (journal nativeJournalSnapshots) OperationID() string {
	return journal.record.OperationID
}

func (journal nativeJournalSnapshots) HasDocumentGroups() (bool, error) {
	if err := journal.requireNativeValue(); err != nil {
		return false, err
	}
	return len(journal.record.DocumentGroups) != 0, nil
}

func (journal nativeJournalSnapshots) RequireDocumentOnly() error {
	if err := journal.requireNativeValue(); err != nil {
		return err
	}
	if len(journal.record.DocumentGroups) == 0 || len(journal.record.Entries) != 0 || len(journal.record.ClaimTransitions) != 0 || len(journal.record.ProvisionalAcquires) != 0 || len(journal.record.RemovalIntents) != 0 {
		return fmt.Errorf("document-only staging requires document groups without ordinary entry artifacts")
	}
	return nil
}

func (journal nativeJournalSnapshots) MatchesContext(owner stateauthority.Authority, before, after durable.Snapshot) bool {
	return journal.requireNativeValue() == nil && journal.owner.Equal(owner) && journal.before.Equal(before) && journal.after.Equal(after)
}

func (journal nativeJournalSnapshots) ClaimTransitions() ([]ownershipmutation.ClaimTransition, error) {
	if err := journal.requireNativeValue(); err != nil {
		return nil, err
	}
	return canonicalClaimTransitions(journal.record.ClaimTransitions)
}

func (journal nativeJournalSnapshots) ProvisionalAcquires() ([]ownership.ProvisionalAcquireIntent, error) {
	if err := journal.requireNativeValue(); err != nil {
		return nil, err
	}
	return canonicalProvisionalAcquireIntents(journal.record.ProvisionalAcquires)
}

func (journal nativeJournalSnapshots) OrdinaryRemovalIntents() ([]recovery.RemovalIntent, error) {
	if err := journal.requireNativeValue(); err != nil {
		return nil, err
	}
	return canonicalRecoveryRemovalIntents(journal.record.RemovalIntents)
}

func (journal nativeJournalSnapshots) RemovalIntents() ([]recovery.RemovalIntent, error) {
	intents, err := journal.OrdinaryRemovalIntents()
	if err != nil {
		return nil, err
	}
	documents, err := journal.recordedDocuments()
	if err != nil {
		return nil, err
	}
	for _, document := range documents {
		removal, present, err := document.Removal()
		if err != nil {
			return nil, err
		}
		if !present {
			continue
		}
		intent, err := removal.PhysicalIntent()
		if err != nil {
			return nil, err
		}
		intents = append(intents, intent)
	}
	return intents, nil
}

func (journal nativeJournalSnapshots) RemovalDemands() (recovery.RemovalDemandSet, error) {
	ordinary, err := journal.OrdinaryRemovalIntents()
	if err != nil {
		return recovery.RemovalDemandSet{}, err
	}
	var demands []recovery.RemovalDemand
	for _, intent := range ordinary {
		demands = append(demands, intent.Demand())
	}
	documents, err := journal.recordedDocuments()
	if err != nil {
		return recovery.RemovalDemandSet{}, err
	}
	for _, document := range documents {
		removal, present, err := document.Removal()
		if err != nil {
			return recovery.RemovalDemandSet{}, err
		}
		if present {
			demands = append(demands, removal.Demands().Demands()...)
		}
	}
	return recovery.NewRemovalDemandSet(demands)
}
