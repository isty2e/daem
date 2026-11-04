package journal

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
type recoveryPlanningBasis struct {
	authority     recovery.Authority
	transitions   []ownershipmutation.ClaimTransition
	intents       []ownership.ProvisionalAcquireIntent
	selected      []int
	all, planning []recordedPathEntry
	manifest      rootedpath.AuthorityProvenance
	removalCount  int
	initialized   bool
}

func (journal recoveryJournal) recoveryPlanningBasis(operationDir, fingerprint string, selected *[]EntrySelection) (recoveryPlanningBasis, error) {
	transitions, err := canonicalClaimTransitions(journal.ClaimTransitions)
	if err != nil {
		return recoveryPlanningBasis{}, err
	}
	intents, err := canonicalProvisionalAcquireIntents(journal.ProvisionalAcquires)
	if err != nil {
		return recoveryPlanningBasis{}, err
	}
	var indexes []int
	if selected != nil {
		indexes, err = selectedRecoveryEntryIndexes(journal.Entries, *selected)
		if err != nil {
			return recoveryPlanningBasis{}, err
		}
	}
	authority, err := canonicalRecoveryAuthority(journal, operationDir, transitions, intents, fingerprint)
	if err != nil {
		return recoveryPlanningBasis{}, err
	}
	manifest, err := journal.ManifestRootProvenance.canonical()
	if err != nil {
		return recoveryPlanningBasis{}, err
	}
	all := recordedPathEntries(journal.Entries)
	planning := all
	if selected != nil {
		planning = make([]recordedPathEntry, len(indexes))
		for resultIndex, entryIndex := range indexes {
			planning[resultIndex] = all[entryIndex]
		}
	}
	return recoveryPlanningBasis{
		authority: authority, transitions: transitions, intents: intents,
		selected: indexes, all: all, planning: planning,
		manifest: manifest, removalCount: len(journal.RemovalIntents), initialized: true,
	}, nil
}

func (basis recoveryPlanningBasis) Validate() error {
	if !basis.initialized {
		return fmt.Errorf("recovery planning basis is required")
	}
	return nil
}

func (basis recoveryPlanningBasis) Authority() recovery.Authority { return basis.authority }
func (basis recoveryPlanningBasis) ClaimTransitions() []ownershipmutation.ClaimTransition {
	return slices.Clone(basis.transitions)
}

func (basis recoveryPlanningBasis) ProvisionalAcquires() []ownership.ProvisionalAcquireIntent {
	return slices.Clone(basis.intents)
}

func (basis recoveryPlanningBasis) SelectedIndexes() []int {
	return slices.Clone(basis.selected)
}

func (basis recoveryPlanningBasis) AllEntries() []recordedPathEntry {
	return slices.Clone(basis.all)
}

func (basis recoveryPlanningBasis) PlanningEntries() []recordedPathEntry {
	return slices.Clone(basis.planning)
}

func (basis recoveryPlanningBasis) ManifestProvenance() rootedpath.AuthorityProvenance {
	return basis.manifest
}
func (basis recoveryPlanningBasis) RemovalIntentCount() int { return basis.removalCount }
