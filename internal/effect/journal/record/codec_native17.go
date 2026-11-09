package record

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/isty2e/daem/internal/assurance/durable"
	journaldocument "github.com/isty2e/daem/internal/effect/journal/document"
	journalnamespace "github.com/isty2e/daem/internal/effect/journal/namespace"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/profile/configuration"
)

const native17Version = 17

type nativeRootReference struct {
	Identity string `json:"identity"`
	Locator  string `json:"locator"`
}

type native17Root struct {
	Reference   nativeRootReference    `json:"reference"`
	Kind        string                 `json:"kind"`
	Alternative string                 `json:"alternative,omitempty"`
	Provenance  recoveryRootProvenance `json:"provenance"`
}

type nativeCoordinate struct {
	Kind  string `json:"kind"`
	Child string `json:"child,omitempty"`
}

type native17Writer struct {
	Root       nativeRootReference `json:"root"`
	Coordinate nativeCoordinate    `json:"coordinate"`
	Spelling   string              `json:"spelling,omitempty"`
}

type native17Alias struct {
	Address             recoveryDocumentAliasRef    `json:"address"`
	Root                nativeRootReference         `json:"root"`
	Coordinate          nativeCoordinate            `json:"coordinate"`
	Policy              string                      `json:"policy"`
	SelectedObservation recoveryDocumentObservation `json:"selected_observation"`
	ObservedBefore      recoveryDocumentObservation `json:"observed_before"`
}

type native17DocumentEnvelope struct {
	Writer    native17Writer          `json:"writer"`
	Aliases   []native17Alias         `json:"aliases"`
	Sequences []nativeSequenceWitness `json:"sequences"`
}

type native17Document struct {
	nativeDocumentBody
	native17DocumentEnvelope
}

type native17Envelope struct {
	Manifest nativeRootReference        `json:"manifest_root"`
	Roots    []native17Root             `json:"roots"`
	Groups   []native17DocumentEnvelope `json:"-"`
}

type native17JournalRecord struct {
	Version int    `json:"version"`
	Purpose string `json:"purpose"`
	nativeRecordBody
	Manifest       nativeRootReference `json:"manifest_root"`
	Roots          []native17Root      `json:"roots"`
	DocumentGroups []native17Document  `json:"document_groups"`
}

func (reference nativeRootReference) canonical() (output.RootReference, error) {
	return output.NewRootReference(reference.Identity, reference.Locator)
}

func persistedRootReference(reference output.RootReference) nativeRootReference {
	return nativeRootReference{Identity: reference.Identity(), Locator: reference.LocatorPolicy()}
}

func (coordinate nativeCoordinate) canonical() (configuration.RootCoordinate, error) {
	switch coordinate.Kind {
	case "directory-child":
		return configuration.NewDirectoryChild(coordinate.Child)
	case "point-self":
		if coordinate.Child == "" {
			return configuration.PointSelf(), nil
		}
	}
	return configuration.RootCoordinate{}, fmt.Errorf("recorded coordinate must be a directory child or point self")
}

func persistedCoordinate(coordinate configuration.RootCoordinate) nativeCoordinate {
	if child, directory := coordinate.DirectoryChild(); directory {
		return nativeCoordinate{Kind: "directory-child", Child: child}
	}
	return nativeCoordinate{Kind: "point-self"}
}

func decodeNative17(ctx context.Context, content []byte, codec durable.SnapshotCodec) (Native, error) {
	if err := requireRecoveryJSONFields(content, "native17 journal", "version", "purpose", "operation_id", "operation", "created_at", "state_authority", "entries", "statefile_before", "statefile_after", "claim_transitions", "provisional_acquire_intents", "removal_intents", "manifest_root", "roots", "document_groups"); err != nil {
		return Native{}, err
	}
	var record native17JournalRecord
	if err := decodeRecoveryJSONStrict(content, &record); err != nil {
		return Native{}, err
	}
	return record.reconstruct(ctx, codec)
}

func (record native17JournalRecord) reconstruct(ctx context.Context, codec durable.SnapshotCodec) (Native, error) {
	if ctx == nil || codec == nil {
		return Native{}, fmt.Errorf("native17 requires context and Snapshot codec")
	}
	if err := ctx.Err(); err != nil {
		return Native{}, err
	}
	if record.Version != native17Version || record.Purpose != "host-mutation" || record.Operation != OperationApply || !IsSafeOperationID(record.OperationID) {
		return Native{}, fmt.Errorf("native17 requires its closed host-mutation purpose and operation identity")
	}
	if record.Roots == nil || record.DocumentGroups == nil || record.Entries == nil || record.ClaimTransitions == nil || record.ProvisionalAcquires == nil || record.RemovalIntents == nil {
		return Native{}, fmt.Errorf("native17 requires explicit complete root, group and ordinary metadata arrays")
	}
	if _, err := time.Parse(time.RFC3339, record.CreatedAt); err != nil {
		return Native{}, err
	}
	owner, err := record.StateAuthority.canonical()
	if err != nil {
		return Native{}, err
	}
	before, err := codec.Decode(record.StatefileBefore)
	if err != nil {
		return Native{}, err
	}
	if err := ctx.Err(); err != nil {
		return Native{}, err
	}
	after, err := codec.Decode(record.StatefileAfter)
	if err != nil {
		return Native{}, err
	}
	if _, _, err := EncodeSnapshots(before, after, codec); err != nil {
		return Native{}, err
	}
	namespace, groups, err := record.lower(ctx)
	if err != nil {
		return Native{}, err
	}
	manifest := persistedNativeRoot(namespace.roots[namespace.manifestReference])
	body := nativeRuntimeRecord{nativeRecordBody: record.nativeRecordBody, Version: record.Version, ManifestRootProvenance: manifest, DocumentGroups: groups}
	if len(groups) == 0 && len(body.Entries) == 0 && before.Equal(after) {
		return Native{}, fmt.Errorf("native17 has no host or state work")
	}
	if err := validateRecoveryEntries(body.Entries); err != nil {
		return Native{}, err
	}
	if err := validateNativeOrdinaryEntries(body.Entries); err != nil {
		return Native{}, err
	}
	if err := body.validateWriterSpellingPartition(ctx); err != nil {
		return Native{}, err
	}
	if err := body.validateRemovalCoverage(); err != nil {
		return Native{}, err
	}
	legacy := recoveryJournal{OperationID: body.OperationID, Entries: body.Entries, StatefileBefore: before, StatefileAfter: after, ClaimTransitions: body.ClaimTransitions, ProvisionalAcquires: body.ProvisionalAcquires, RemovalIntents: body.RemovalIntents}
	if len(body.Entries) != 0 || len(groups) == 0 {
		if err := validateRecoveryJournalRelationships(legacy); err != nil {
			return Native{}, err
		}
	} else if len(body.ClaimTransitions)+len(body.ProvisionalAcquires)+len(body.RemovalIntents) != 0 {
		return Native{}, fmt.Errorf("ordinary metadata requires ordinary entries")
	}
	envelope := &native17Envelope{Manifest: record.Manifest, Roots: record.Roots, Groups: make([]native17DocumentEnvelope, len(record.DocumentGroups))}
	for index, group := range record.DocumentGroups {
		envelope.Groups[index] = group.native17DocumentEnvelope
	}
	if err := ctx.Err(); err != nil {
		return Native{}, err
	}
	return Native{record: body, current: envelope, namespace: namespace, owner: owner, before: before, after: after}, nil
}

func (record native17JournalRecord) lower(ctx context.Context) (nativeNamespaceFacts, []nativeDocumentRecord, error) {
	work := len(record.Roots) + len(record.DocumentGroups) + len(record.Entries) + len(record.ClaimTransitions) + len(record.ProvisionalAcquires) + len(record.RemovalIntents)
	if err := validateRecoveryOwnershipWorkBudget(work, 0, 0); err != nil {
		return nativeNamespaceFacts{}, nil, err
	}
	manifest, err := record.Manifest.canonical()
	if err != nil || manifest.Identity() != configuration.ProjectRootIdentity {
		return nativeNamespaceFacts{}, nil, fmt.Errorf("native17 manifest reference is invalid")
	}
	roots := make(map[output.RootReference]native17Root, len(record.Roots))
	facts := nativeNamespaceFacts{
		claims: journaldocument.NativeClaims, manifestReference: manifest,
		roots:        make(map[output.RootReference]rootedpath.AuthorityProvenance),
		alternatives: make(map[output.RootReference]configuration.PointAlternative),
	}
	used := map[output.RootReference]bool{manifest: true}
	identities := make(map[string]bool, len(record.Roots))
	var previous output.RootReference
	for index, row := range record.Roots {
		if err := ctx.Err(); err != nil {
			return nativeNamespaceFacts{}, nil, err
		}
		reference, err := row.Reference.canonical()
		if err != nil || index != 0 && previous.Compare(reference) >= 0 || identities[reference.Identity()] {
			return nativeNamespaceFacts{}, nil, fmt.Errorf("native17 roots must have unique canonical references")
		}
		identities[reference.Identity()] = true
		point, err := configuration.NativeRootIsPoint(reference)
		if err != nil || point != (row.Kind == "point") {
			return nativeNamespaceFacts{}, nil, fmt.Errorf("recorded root kind differs from its retained locator revision")
		}
		if reference == manifest {
			if reference.LocatorPolicy() != "selected-project-v1" || row.Kind != "directory" {
				return nativeNamespaceFacts{}, nil, fmt.Errorf("manifest must retain its directory locator")
			}
		} else if err := configuration.AdmitNativeAliasRootReference(reference); err != nil {
			return nativeNamespaceFacts{}, nil, err
		}
		switch row.Kind {
		case "directory":
			if row.Alternative != "" {
				return nativeNamespaceFacts{}, nil, fmt.Errorf("directory root cannot select a point alternative")
			}
		case "point":
			if err := configuration.PointAlternative(row.Alternative).Validate(); err != nil {
				return nativeNamespaceFacts{}, nil, err
			}
			facts.alternatives[reference] = configuration.PointAlternative(row.Alternative)
		default:
			return nativeNamespaceFacts{}, nil, fmt.Errorf("native17 root selection kind is invalid")
		}
		provenance, err := row.Provenance.canonical()
		if err != nil {
			return nativeNamespaceFacts{}, nil, err
		}
		roots[reference], facts.roots[reference], previous = row, provenance, reference
	}
	if _, present := roots[manifest]; !present {
		return nativeNamespaceFacts{}, nil, fmt.Errorf("native17 manifest root is missing")
	}
	groups := make([]nativeDocumentRecord, len(record.DocumentGroups))
	for index, group := range record.DocumentGroups {
		if err := ctx.Err(); err != nil {
			return nativeNamespaceFacts{}, nil, err
		}
		writerReference, err := group.Writer.Root.canonical()
		if err != nil {
			return nativeNamespaceFacts{}, nil, err
		}
		writerRoot, present := roots[writerReference]
		coordinate, coordinateErr := group.Writer.Coordinate.canonical()
		if !present || coordinateErr != nil || (coordinate.Kind() == configuration.CoordinatePointSelf) != (writerRoot.Kind == "point") {
			return nativeNamespaceFacts{}, nil, fmt.Errorf("native17 writer does not match a recorded root kind")
		}
		pin, err := journalnamespace.NewBoundWriterPin(writerReference, facts.roots[writerReference], coordinate)
		if err != nil {
			return nativeNamespaceFacts{}, nil, err
		}
		if writerRoot.Kind == "point" {
			if err := validateWriterSpelling(group.Writer.Spelling, writerRoot.Provenance); err != nil {
				return nativeNamespaceFacts{}, nil, err
			}
		} else if group.Writer.Spelling != "" {
			return nativeNamespaceFacts{}, nil, fmt.Errorf("directory writer cannot carry point spelling evidence")
		}
		namespace := documentNamespaceFacts{writer: pin, writerSpelling: group.Writer.Spelling, aliases: make([]documentAliasFacts, 0, len(group.Aliases))}
		ownPointAlias := false
		for position, alias := range group.Aliases {
			address, err := alias.Address.canonical()
			if err != nil || position != 0 && compareDocumentAliasRefs(group.Aliases[position-1].Address, alias.Address) >= 0 {
				return nativeNamespaceFacts{}, nil, fmt.Errorf("native17 aliases must be unique canonical addresses")
			}
			reference, err := alias.Root.canonical()
			if err != nil {
				return nativeNamespaceFacts{}, nil, err
			}
			root, present := roots[reference]
			coordinate, err := alias.Coordinate.canonical()
			if !present || err != nil || (coordinate.Kind() == configuration.CoordinatePointSelf) != (root.Kind == "point") {
				return nativeNamespaceFacts{}, nil, fmt.Errorf("native17 alias root and coordinate kind differ")
			}
			binding, err := configuration.AdmitRecordedAliasBinding(configuration.AliasInput{Target: address.Target(), Scope: address.Scope(), Destination: address.AggregateRoot()}, reference, coordinate, alias.Policy)
			if err != nil {
				return nativeNamespaceFacts{}, nil, err
			}
			if err := alias.SelectedObservation.validate(); err != nil {
				return nativeNamespaceFacts{}, nil, err
			}
			if !alias.ObservedBefore.equal(group.CapturedTarget) {
				return nativeNamespaceFacts{}, nil, fmt.Errorf("native17 alias before differs from its captured document")
			}
			namespace.aliases = append(namespace.aliases, documentAliasFacts{address: address, location: binding.Location(), binding: binding, root: facts.roots[reference], selected: alias.SelectedObservation, before: alias.ObservedBefore})
			used[reference] = true
			ownPointAlias = ownPointAlias || reference == writerReference && root.Kind == "point"
		}
		if writerRoot.Kind == "point" && !ownPointAlias {
			return nativeNamespaceFacts{}, nil, fmt.Errorf("point writer has no own point-self alias")
		}
		used[writerReference] = true
		groups[index] = nativeDocumentRecord{nativeDocumentBody: group.nativeDocumentBody, namespace: namespace, replay: selectedSequenceReplay{arrays: group.Sequences}}
		if group.Sequences == nil {
			return nativeNamespaceFacts{}, nil, fmt.Errorf("native17 requires an explicit complete sequence set")
		}
		for position, witness := range group.Sequences {
			if err := ctx.Err(); err != nil {
				return nativeNamespaceFacts{}, nil, err
			}
			if err := witness.validate(namespace.aliases); err != nil {
				return nativeNamespaceFacts{}, nil, err
			}
			path, _ := output.NewKeyPath(witness.Path...)
			for _, earlier := range group.Sequences[:position] {
				other, _ := output.NewKeyPath(earlier.Path...)
				if path.Overlaps(other) {
					return nativeNamespaceFacts{}, nil, fmt.Errorf("native17 sequence witnesses overlap")
				}
			}
			work += len(witness.Path) + len(witness.Removed) + len(witness.Order)
			if err := validateRecoveryOwnershipWorkBudget(work, 0, 0); err != nil {
				return nativeNamespaceFacts{}, nil, err
			}
		}
		if err := groups[index].validate(); err != nil {
			return nativeNamespaceFacts{}, nil, err
		}
		work += 1 + len(group.Aliases) + len(group.SelectedSubjects) + len(group.BindingIntents)
		if group.Removal != nil {
			work += 1 + len(group.Removal.Demands) + len(group.Removal.States)
		}
		for _, claims := range []*[]recoveryClaimValue{group.ExclusiveClaims.Before, group.ExclusiveClaims.After} {
			if claims != nil {
				work += len(*claims)
			}
		}
		if group.ExclusiveClaims.Acquires != nil {
			work += len(*group.ExclusiveClaims.Acquires)
		}
		if err := validateRecoveryOwnershipWorkBudget(work, 0, 0); err != nil {
			return nativeNamespaceFacts{}, nil, err
		}
	}
	if len(used) != len(roots) {
		return nativeNamespaceFacts{}, nil, fmt.Errorf("native17 root table must exactly close over manifest, aliases and writers")
	}
	return facts, groups, nil
}

func validateWriterSpelling(spelling string, root recoveryRootProvenance) error {
	if err := validateRecoveryResolvedGlobalPath(spelling); err != nil {
		return err
	}
	relative, err := filepath.Rel(root.PhysicalRoot, spelling)
	if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return fmt.Errorf("point writer spelling must be strictly below its recorded witness")
	}
	return nil
}

func (witness nativeSequenceWitness) validate(aliases []documentAliasFacts) error {
	address, err := witness.Alias.canonical()
	if err != nil {
		return err
	}
	found := false
	for _, alias := range aliases {
		found = found || alias.address == address
	}
	if !found || witness.Path == nil || witness.Removed == nil || witness.Order == nil {
		return fmt.Errorf("selected sequence requires its document alias and explicit positional evidence")
	}
	if _, err := output.NewKeyPath(witness.Path...); err != nil {
		return err
	}
	previous := -1
	for _, removed := range witness.Removed {
		if removed <= previous {
			return fmt.Errorf("removed slots must be sorted unique original positions")
		}
		previous = removed
	}
	seen := make([]bool, len(witness.Order))
	for _, slot := range witness.Order {
		if slot < 0 || slot >= len(seen) || seen[slot] {
			return fmt.Errorf("survivor order must be bijective")
		}
		seen[slot] = true
	}
	switch witness.Syntax {
	case "json-v1", "jsonc-v1", "json-line-comments-v1", "toml-v1":
		return nil
	default:
		return fmt.Errorf("selected array syntax contract is unsupported")
	}
}

func (journal Native) encodeNative17(ctx context.Context, codec durable.SnapshotCodec) ([]byte, error) {
	before, after, err := EncodeSnapshots(journal.before, journal.after, codec)
	if err != nil {
		return nil, err
	}
	body := journal.record.nativeRecordBody
	body.StatefileBefore, body.StatefileAfter = before, after
	record := native17JournalRecord{Version: native17Version, Purpose: "host-mutation", nativeRecordBody: body, Manifest: journal.current.Manifest, Roots: journal.current.Roots, DocumentGroups: make([]native17Document, len(journal.record.DocumentGroups))}
	for index, group := range journal.record.DocumentGroups {
		record.DocumentGroups[index] = native17Document{nativeDocumentBody: group.nativeDocumentBody, native17DocumentEnvelope: journal.current.Groups[index]}
	}
	if _, err := record.reconstruct(ctx, codec); err != nil {
		return nil, err
	}
	content, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return nil, err
	}
	if int64(len(content)) >= MaximumBytes {
		return nil, fmt.Errorf("native17 exceeds the journal byte envelope")
	}
	return append(content, '\n'), nil
}
