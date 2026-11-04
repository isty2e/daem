package record

import (
	"fmt"
	"slices"

	"github.com/isty2e/daem/internal/effect/journal/recovery"
	ownershipmutation "github.com/isty2e/daem/internal/effect/mutation/ownership"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/output/ownership"
)

// Planning facts preserve the complete journal authority even when observations
// are narrowed to selected entries. They do not supply live filesystem rights.
type PlanningBasis struct {
	authority     recovery.Authority
	transitions   []ownershipmutation.ClaimTransition
	intents       []ownership.ProvisionalAcquireIntent
	selected      []int
	all, planning []PathEntry
	manifest      rootedpath.AuthorityProvenance
	removalCount  int
	initialized   bool
}

func (journal recoveryJournal) PlanningBasis(operationDir, fingerprint string, selected *[]EntrySelection) (PlanningBasis, error) {
	transitions, err := canonicalClaimTransitions(journal.ClaimTransitions)
	if err != nil {
		return PlanningBasis{}, err
	}
	intents, err := canonicalProvisionalAcquireIntents(journal.ProvisionalAcquires)
	if err != nil {
		return PlanningBasis{}, err
	}
	var indexes []int
	if selected != nil {
		indexes, err = selectedRecoveryEntryIndexes(journal.Entries, *selected)
		if err != nil {
			return PlanningBasis{}, err
		}
	}
	authority, err := canonicalRecoveryAuthority(journal, operationDir, transitions, intents, fingerprint)
	if err != nil {
		return PlanningBasis{}, err
	}
	manifest, err := journal.ManifestRootProvenance.canonical()
	if err != nil {
		return PlanningBasis{}, err
	}
	all := recordedPathEntries(journal.Entries)
	planning := all
	if selected != nil {
		planning = make([]PathEntry, len(indexes))
		for resultIndex, entryIndex := range indexes {
			planning[resultIndex] = all[entryIndex]
		}
	}
	return PlanningBasis{
		authority: authority, transitions: transitions, intents: intents,
		selected: indexes, all: all, planning: planning,
		manifest: manifest, removalCount: len(journal.RemovalIntents), initialized: true,
	}, nil
}

func (basis PlanningBasis) Validate() error {
	if !basis.initialized {
		return fmt.Errorf("recovery planning basis is required")
	}
	return nil
}

func (basis PlanningBasis) Authority() recovery.Authority { return basis.authority }
func (basis PlanningBasis) ClaimTransitions() []ownershipmutation.ClaimTransition {
	return slices.Clone(basis.transitions)
}

func (basis PlanningBasis) ProvisionalAcquires() []ownership.ProvisionalAcquireIntent {
	return slices.Clone(basis.intents)
}

func (basis PlanningBasis) SelectedIndexes() []int {
	return slices.Clone(basis.selected)
}

func (basis PlanningBasis) AllEntries() []PathEntry {
	return slices.Clone(basis.all)
}

func (basis PlanningBasis) PlanningEntries() []PathEntry {
	return slices.Clone(basis.planning)
}

func (basis PlanningBasis) ManifestProvenance() rootedpath.AuthorityProvenance {
	return basis.manifest
}
func (basis PlanningBasis) RemovalIntentCount() int { return basis.removalCount }
