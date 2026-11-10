package record

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/profile/configuration"
)

// RootCapture is retained identity evidence, not a root handle or selector.
type RootCapture struct {
	row native17Root
}

func NewRootCapture(reference output.RootReference, provenance rootedpath.AuthorityProvenance, alternative configuration.PointAlternative) (RootCapture, error) {
	point, err := configuration.NativeRootIsPoint(reference)
	if err != nil {
		return RootCapture{}, err
	}
	if err := provenance.Validate(); err != nil {
		return RootCapture{}, err
	}
	row := native17Root{Reference: persistedRootReference(reference), Kind: "directory", Provenance: persistedNativeRoot(provenance)}
	if point {
		if configuration.NativeRootIsOwnedPoint(reference) {
			if alternative != "" {
				return RootCapture{}, fmt.Errorf("owned point capture cannot carry a current chooser alternative")
			}
		} else {
			if err := alternative.Validate(); err != nil {
				return RootCapture{}, err
			}
		}
		row.Kind, row.Alternative = "point", string(alternative)
	} else if alternative != "" {
		return RootCapture{}, fmt.Errorf("directory capture cannot carry a point alternative")
	}
	return RootCapture{row: row}, nil
}

type SequenceWitness struct {
	value nativeSequenceWitness
}

func NewSequenceWitness(document aggregate.DocumentAddress, edit configdocument.JSONArrayEdit) (SequenceWitness, error) {
	witness := nativeSequenceWitness{
		Alias: persistedDocumentAlias(document), Path: append([]string{}, edit.ArrayPath().Segments()...),
		Syntax: edit.Syntax().String() + "-v1", Removed: append([]int{}, edit.RemovedSlots()...), Order: append([]int{}, edit.SurvivorOrder()...),
	}
	if err := witness.validate([]documentAliasFacts{{address: document}}); err != nil {
		return SequenceWitness{}, err
	}
	return SequenceWitness{value: witness}, nil
}

func (draft DocumentGroupDraft) WithSequences(selected []SequenceWitness) (DocumentGroupDraft, error) {
	if draft.phase != documentGroupMetadataComplete || draft.current == nil {
		return DocumentGroupDraft{}, fmt.Errorf("native17 sequences require complete bound document metadata")
	}
	envelope := *draft.current
	envelope.Sequences = make([]nativeSequenceWitness, len(selected))
	for index, witness := range selected {
		if err := witness.value.validate(draft.namespace.aliases); err != nil {
			return DocumentGroupDraft{}, err
		}
		for _, earlier := range envelope.Sequences[:index] {
			left, _ := output.NewKeyPath(earlier.Path...)
			right, _ := output.NewKeyPath(witness.value.Path...)
			if left.Overlaps(right) {
				return DocumentGroupDraft{}, fmt.Errorf("selected sequences overlap in one physical document")
			}
		}
		envelope.Sequences[index] = witness.value
	}
	draft.current, draft.phase = &envelope, documentGroupOrderRecorded
	return draft, nil
}

func NewNative17Capture(ctx context.Context, operationID string, createdAt time.Time, owner stateauthority.Authority, manifest output.RootReference, roots []RootCapture, before, after durable.Snapshot, groups []SealedDocumentGroup, codec durable.SnapshotCodec) (Native, error) {
	if ctx == nil {
		return Native{}, fmt.Errorf("native17 capture context is required")
	}
	if err := owner.Validate(); err != nil {
		return Native{}, err
	}
	byReference := make(map[output.RootReference]native17Root, len(roots))
	for _, captured := range roots {
		reference, err := captured.row.Reference.canonical()
		if err != nil {
			return Native{}, err
		}
		if old, duplicate := byReference[reference]; duplicate && old != captured.row {
			return Native{}, fmt.Errorf("captured root reference has conflicting selection facts")
		}
		byReference[reference] = captured.row
	}
	beforeContent, afterContent, err := EncodeSnapshots(before, after, codec)
	if err != nil {
		return Native{}, err
	}
	used := map[output.RootReference]bool{manifest: true}
	records := make([]native17Document, len(groups))
	for index, group := range groups {
		if err := ctx.Err(); err != nil {
			return Native{}, err
		}
		if !group.initialized || group.current == nil {
			return Native{}, fmt.Errorf("native17 requires every complete group in its bound capture form")
		}
		writer, bound := group.namespace.writer.RootReference()
		root, present := byReference[writer]
		if !bound || !present || root.Provenance != persistedNativeRoot(group.namespace.writer.Provenance()) {
			return Native{}, fmt.Errorf("captured writer root differs from the qualified group")
		}
		used[writer] = true
		for _, alias := range group.namespace.aliases {
			reference := alias.location.RootReference()
			root, present := byReference[reference]
			if !present || root.Provenance != persistedNativeRoot(alias.root) {
				return Native{}, fmt.Errorf("captured alias root differs from the qualified group")
			}
			used[reference] = true
		}
		records[index] = *group.current
	}
	references := make([]output.RootReference, 0, len(used))
	for reference := range used {
		if _, present := byReference[reference]; !present {
			return Native{}, fmt.Errorf("required captured root reference is missing")
		}
		references = append(references, reference)
	}
	slices.SortFunc(references, output.RootReference.Compare)
	rows := make([]native17Root, 0, len(references))
	for _, reference := range references {
		rows = append(rows, byReference[reference])
	}
	record := native17JournalRecord{
		Version: native17Version, Purpose: "host-mutation", Manifest: persistedRootReference(manifest), Roots: rows, DocumentGroups: records,
		nativeRecordBody: nativeRecordBody{
			OperationID: operationID, Operation: OperationApply, CreatedAt: createdAt.UTC().Format(time.RFC3339),
			StateAuthority: recoveryStateAuthority{StatefileAuthority: *persistedPathAuthority(owner.StatefileAuthority()), ManifestPath: owner.ManifestPath()},
			Entries:        []recoveryEntry{}, ClaimTransitions: []recoveryClaimTransition{}, ProvisionalAcquires: []recoveryProvisionalAcquireIntent{}, RemovalIntents: []recoveryRemovalIntent{}, StatefileBefore: beforeContent, StatefileAfter: afterContent,
		},
	}
	return record.reconstruct(ctx, codec)
}
