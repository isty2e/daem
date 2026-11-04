package journal

import (
	"fmt"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	ownershipmutation "github.com/isty2e/daem/internal/effect/mutation/ownership"
	"github.com/isty2e/daem/internal/output/ownership"
)

func (journal nativeJournalSnapshots) requireNativeValue() error {
	if journal.record.Version != nativeRecoveryJournalVersion {
		return fmt.Errorf("native journal record is required")
	}
	return nil
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
