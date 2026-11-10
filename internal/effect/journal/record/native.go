package record

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"time"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/pathauthority"
	"github.com/isty2e/daem/internal/assurance/stateauthority"

	"github.com/isty2e/daem/internal/effect/journal/recovery"
	ownershipmutation "github.com/isty2e/daem/internal/effect/mutation/ownership"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/output/ownership"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/profile/configuration"
)

func NewNativeCapture(operationID string, createdAt time.Time, owner stateauthority.Authority, manifest rootedpath.AuthorityProvenance, before, after durable.Snapshot, groups []SealedDocumentGroup) (Native, error) {
	if err := owner.Validate(); err != nil {
		return Native{}, err
	}
	records := make([]recoveryDocumentGroup, len(groups))
	for index, group := range groups {
		if !group.initialized || group.current != nil {
			return Native{}, fmt.Errorf("captured document group is required")
		}
		records[index] = group.record
	}
	record := nativeJournalRecord{
		Version: native16Version, Operation: OperationApply, OperationID: operationID, CreatedAt: createdAt.UTC().Format(time.RFC3339),
		StateAuthority:         recoveryStateAuthority{StatefileAuthority: *persistedPathAuthority(owner.StatefileAuthority()), ManifestPath: owner.ManifestPath()},
		ManifestRootProvenance: persistedNativeRoot(manifest), Entries: []recoveryEntry{}, ClaimTransitions: []recoveryClaimTransition{}, ProvisionalAcquires: []recoveryProvisionalAcquireIntent{}, RemovalIntents: []recoveryRemovalIntent{}, DocumentGroups: records,
	}
	return newNative16Value(record, owner, before, after)
}

func (journal Native) requireNativeValue() error {
	if !SupportsNativeVersion(journal.record.Version) {
		return fmt.Errorf("native journal record is required")
	}
	return nil
}

// Owner returns the operation owner, or its zero value for a zero native value.
func (journal Native) Owner() stateauthority.Authority {
	return journal.owner
}

// Before returns the complete Before snapshot, or its zero value for zero Native.
func (journal Native) Before() durable.Snapshot {
	return journal.before
}

// After returns the complete After snapshot, or its zero value for zero Native.
func (journal Native) After() durable.Snapshot {
	return journal.after
}

// OperationID returns the operation identity, or an empty string for a zero value.
func (journal Native) OperationID() string {
	return journal.record.OperationID
}

func (journal Native) HasDocumentGroups() (bool, error) {
	if err := journal.requireNativeValue(); err != nil {
		return false, err
	}
	return len(journal.record.DocumentGroups) != 0, nil
}

func (journal Native) RequireDocumentOnly() error {
	if err := journal.requireNativeValue(); err != nil {
		return err
	}
	if len(journal.record.DocumentGroups) == 0 || len(journal.record.Entries) != 0 || len(journal.record.ClaimTransitions) != 0 || len(journal.record.ProvisionalAcquires) != 0 || len(journal.record.RemovalIntents) != 0 {
		return fmt.Errorf("document-only staging requires document groups without ordinary entry artifacts")
	}
	return nil
}

func (journal Native) MatchesContext(owner stateauthority.Authority, before, after durable.Snapshot) bool {
	return journal.requireNativeValue() == nil && journal.owner.Equal(owner) && journal.before.Equal(before) && journal.after.Equal(after)
}

func (journal Native) ClaimTransitions() ([]ownershipmutation.ClaimTransition, error) {
	if err := journal.requireNativeValue(); err != nil {
		return nil, err
	}
	return canonicalClaimTransitions(journal.record.ClaimTransitions)
}

func (journal Native) ProvisionalAcquires() ([]ownership.ProvisionalAcquireIntent, error) {
	if err := journal.requireNativeValue(); err != nil {
		return nil, err
	}
	return canonicalProvisionalAcquireIntents(journal.record.ProvisionalAcquires)
}

func (journal Native) OrdinaryRemovalIntents() ([]recovery.RemovalIntent, error) {
	if err := journal.requireNativeValue(); err != nil {
		return nil, err
	}
	return canonicalRecoveryRemovalIntents(journal.record.RemovalIntents)
}

func (journal Native) RemovalIntents() ([]recovery.RemovalIntent, error) {
	intents, err := journal.OrdinaryRemovalIntents()
	if err != nil {
		return nil, err
	}
	documents, err := journal.Documents()
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

func (journal Native) RemovalDemands() (recovery.RemovalDemandSet, error) {
	ordinary, err := journal.OrdinaryRemovalIntents()
	if err != nil {
		return recovery.RemovalDemandSet{}, err
	}
	var demands []recovery.RemovalDemand
	for _, intent := range ordinary {
		demands = append(demands, intent.Demand())
	}
	documents, err := journal.Documents()
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

func (journal Native) DocumentAliases() ([][]aggregate.DocumentAddress, error) {
	documents, err := journal.Documents()
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

func (journal Native) ManifestRootReference() (output.RootReference, error) {
	if err := journal.requireNativeValue(); err != nil {
		return output.RootReference{}, err
	}
	return journal.namespace.manifestReference, nil
}

func (journal Native) RecordedRootAnchors() (map[output.RootReference]rootedpath.AuthorityProvenance, error) {
	if err := journal.requireNativeValue(); err != nil {
		return nil, err
	}
	return maps.Clone(journal.namespace.roots), nil
}

func (journal Native) RecordedPointAlternative(reference output.RootReference) (configuration.PointAlternative, bool) {
	alternative, point := journal.namespace.alternatives[reference]
	return alternative, point
}

func (journal Native) ProvisionalAcquireEntry(intent ownership.ProvisionalAcquireIntent) (recovery.Entry, error) {
	if err := journal.requireNativeValue(); err != nil {
		return recovery.Entry{}, err
	}
	index, err := promotedIntentEntryIndex(journal.record.Entries, intent)
	if err != nil {
		return recovery.Entry{}, err
	}
	return canonicalRecoveryEntry(journal.record.Entries[index])
}

func (journal Native) PromoteProvisionalAcquire(intent ownership.ProvisionalAcquireIntent, transition ownershipmutation.ClaimTransition) (Native, error) {
	if err := journal.requireNativeValue(); err != nil {
		return Native{}, err
	}
	material := ordinaryOwnershipFacts{
		OperationID: journal.OperationID(), Entries: journal.record.Entries,
		ClaimTransitions: journal.record.ClaimTransitions, ProvisionalAcquires: journal.record.ProvisionalAcquires,
	}
	promoted, err := material.promoteOwnership(intent, transition)
	if err != nil {
		return Native{}, err
	}
	candidate := journal
	candidate.record.Entries, candidate.record.ClaimTransitions, candidate.record.ProvisionalAcquires = promoted.Entries, promoted.ClaimTransitions, promoted.ProvisionalAcquires
	return candidate, nil
}

func (journal Native) PromoteDocument(ctx context.Context, index int, aliases []aggregate.DocumentAddress, exact pathauthority.Exact) (Native, bool, error) {
	document, err := journal.DocumentAt(index)
	if err != nil {
		return Native{}, false, err
	}
	if ctx == nil {
		return Native{}, false, fmt.Errorf("native document promotion context is required")
	}
	group := document.record
	pending, err := group.CapturedTarget.provisional()
	if err != nil {
		return Native{}, false, err
	}
	if err := pending.AdmitsExact(exact); err != nil {
		return Native{}, false, err
	}

	changed := false
	if group.ExclusiveClaims.Kind == "provisional" {
		group.ExclusiveClaims, err = group.ExclusiveClaims.promoted(aliases, journal.Owner(), journal.OperationID(), pending, exact, journal.namespace.claims)
		if err != nil {
			return Native{}, false, err
		}
		changed = true
	}
	groups, err := journal.documentBindings(ctx)
	if err != nil {
		return Native{}, false, err
	}
	bindings := groups[index]
	group.BindingIntents = slices.Clone(group.BindingIntents)
	for position, intent := range bindings {
		if _, ready := intent.KnownAfter(); ready {
			continue
		}
		promoted, err := intent.Promote(exact)
		if err != nil {
			return Native{}, false, err
		}
		group.BindingIntents[position], err = persistedDocumentBindingIntent(promoted)
		if err != nil {
			return Native{}, false, err
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

func (journal Native) Encode(ctx context.Context, codec durable.SnapshotCodec) ([]byte, error) {
	if err := journal.requireNativeValue(); err != nil {
		return nil, err
	}
	return journal.encode(ctx, codec)
}
