package journal

import (
	"context"
	"fmt"
	"slices"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/pathauthority"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	ownershipmutation "github.com/isty2e/daem/internal/effect/mutation/ownership"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/output/ownership"
	"github.com/isty2e/daem/internal/realization/aggregate"
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

func (journal nativeJournalSnapshots) DocumentAliases() ([][]aggregate.DocumentAddress, error) {
	documents, err := journal.recordedDocuments()
	if err != nil {
		return nil, err
	}
	result := make([][]aggregate.DocumentAddress, 0, len(documents))
	for _, document := range documents {
		// This context-free, pure projection retains the existing cancellation seams.
		addresses, err := document.Addresses(context.Background())
		if err != nil {
			return nil, err
		}
		result = append(result, addresses)
	}
	return result, nil
}

func (journal nativeJournalSnapshots) RecordedRootAnchors() (map[output.RootRole]rootedpath.AuthorityProvenance, error) {
	if err := journal.requireNativeValue(); err != nil {
		return nil, err
	}
	manifest, err := journal.record.ManifestRootProvenance.canonical()
	if err != nil {
		return nil, err
	}
	anchors := map[output.RootRole]rootedpath.AuthorityProvenance{output.RootProject: manifest}
	documents, err := journal.recordedDocuments()
	if err != nil {
		return nil, err
	}
	for _, document := range documents {
		for _, alias := range document.Aliases() {
			address, err := alias.Address()
			if err != nil {
				return nil, err
			}
			role := address.AggregateRoot().RootRole()
			provenance, err := alias.record.LogicalRoot.Provenance.canonical()
			if err != nil {
				return nil, err
			}
			if previous, known := anchors[role]; known && previous != provenance {
				return nil, fmt.Errorf("native role has inconsistent recorded root anchors")
			}
			anchors[role] = provenance
		}
	}
	return anchors, nil
}

func (journal nativeJournalSnapshots) ProvisionalAcquireEntry(intent ownership.ProvisionalAcquireIntent) (recovery.Entry, error) {
	if err := journal.requireNativeValue(); err != nil {
		return recovery.Entry{}, err
	}
	index, err := promotedIntentEntryIndex(journal.record.Entries, intent)
	if err != nil {
		return recovery.Entry{}, err
	}
	return canonicalRecoveryEntry(journal.record.Entries[index])
}

func (journal nativeJournalSnapshots) PromoteProvisionalAcquire(intent ownership.ProvisionalAcquireIntent, transition ownershipmutation.ClaimTransition) (nativeJournalSnapshots, error) {
	if err := journal.requireNativeValue(); err != nil {
		return nativeJournalSnapshots{}, err
	}
	material := ordinaryOwnershipFacts{
		OperationID: journal.OperationID(), Entries: journal.record.Entries,
		ClaimTransitions: journal.record.ClaimTransitions, ProvisionalAcquires: journal.record.ProvisionalAcquires,
	}
	promoted, err := material.promoteOwnership(intent, transition)
	if err != nil {
		return nativeJournalSnapshots{}, err
	}
	candidate := journal
	candidate.record.Entries, candidate.record.ClaimTransitions, candidate.record.ProvisionalAcquires = promoted.Entries, promoted.ClaimTransitions, promoted.ProvisionalAcquires
	return candidate, nil
}

func (journal nativeJournalSnapshots) PromoteDocument(ctx context.Context, index int, aliases []aggregate.DocumentAddress, exact pathauthority.Exact) (nativeJournalSnapshots, bool, error) {
	document, err := journal.recordedDocumentAt(index)
	if err != nil {
		return nativeJournalSnapshots{}, false, err
	}
	if ctx == nil {
		return nativeJournalSnapshots{}, false, fmt.Errorf("native document promotion context is required")
	}
	group := document.record
	pending, err := group.CapturedTarget.provisional()
	if err != nil {
		return nativeJournalSnapshots{}, false, err
	}
	if err := pending.AdmitsExact(exact); err != nil {
		return nativeJournalSnapshots{}, false, err
	}

	changed := false
	if group.ExclusiveClaims.Kind == "provisional" {
		group.ExclusiveClaims, err = group.ExclusiveClaims.promoted(aliases, journal.Owner(), journal.OperationID(), pending, exact)
		if err != nil {
			return nativeJournalSnapshots{}, false, err
		}
		changed = true
	}
	bindings, err := group.canonicalBindingIntents(ctx, journal.Owner())
	if err != nil {
		return nativeJournalSnapshots{}, false, err
	}
	group.BindingIntents = slices.Clone(group.BindingIntents)
	for position, intent := range bindings {
		if _, ready := intent.KnownAfter(); ready {
			continue
		}
		promoted, err := intent.Promote(exact)
		if err != nil {
			return nativeJournalSnapshots{}, false, err
		}
		group.BindingIntents[position], err = persistedDocumentBindingIntent(promoted)
		if err != nil {
			return nativeJournalSnapshots{}, false, err
		}
		changed = true
	}
	if !changed {
		return journal, false, nil
	}
	candidate := journal
	candidate.record.DocumentGroups = slices.Clone(candidate.record.DocumentGroups)
	candidate.record.DocumentGroups[index] = group
	return candidate, true, nil
}

func (journal nativeJournalSnapshots) Encode(ctx context.Context, codec durable.SnapshotCodec) ([]byte, error) {
	if err := journal.requireNativeValue(); err != nil {
		return nil, err
	}
	return marshalNativeJournalRecord(ctx, journal.record, journal.Before(), journal.After(), codec)
}
