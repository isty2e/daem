package record

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"

	"github.com/isty2e/daem/internal/assurance/durable"
	journaldocument "github.com/isty2e/daem/internal/effect/journal/document"
	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/aggregate/compose"
	"github.com/isty2e/daem/internal/topology"
)

// Version envelopes own immutable interpretation. This body is the only mutable
// source for promotion, ordinary evidence and prepared image references.
type nativeRecordBody struct {
	OperationID         string                             `json:"operation_id"`
	Operation           string                             `json:"operation"`
	CreatedAt           string                             `json:"created_at"`
	StateAuthority      recoveryStateAuthority             `json:"state_authority"`
	Entries             []recoveryEntry                    `json:"entries"`
	StatefileBefore     json.RawMessage                    `json:"statefile_before"`
	StatefileAfter      json.RawMessage                    `json:"statefile_after"`
	ClaimTransitions    []recoveryClaimTransition          `json:"claim_transitions"`
	ProvisionalAcquires []recoveryProvisionalAcquireIntent `json:"provisional_acquire_intents"`
	RemovalIntents      []recoveryRemovalIntent            `json:"removal_intents"`
}

type nativeRuntimeRecord struct {
	nativeRecordBody
	Version                int
	ManifestRootProvenance recoveryRootProvenance
	DocumentGroups         []nativeDocumentRecord
}

type nativeDocumentBody struct {
	CapturedTarget   recoveryDocumentObservation     `json:"captured_target"`
	Extent           string                          `json:"extent"`
	SelectedSubjects []persistedSubjectRef           `json:"selected_subjects"`
	Original         recoveryDocumentImage           `json:"original"`
	Expected         recoveryDocumentImage           `json:"expected"`
	ExclusiveClaims  recoveryDocumentClaimSet        `json:"exclusive_claims"`
	BindingIntents   []recoveryDocumentBindingIntent `json:"binding_intents"`
	Removal          *recoveryDocumentRemoval        `json:"removal,omitempty"`
}

type nativeDocumentRecord struct {
	nativeDocumentBody
	namespace documentNamespaceFacts
	replay    selectedSequenceReplay
}

type native16Envelope struct {
	groups []native16DocumentEnvelope
}

type native16DocumentEnvelope struct {
	writer  recoveryDocumentWriter
	aliases []recoveryDocumentAlias
	order   *recoveryDocumentPiOrder
}

// The closed replay forms retain16's semantic adapter without manufacturing
// planning observations from17's positional witnesses.
type selectedSequenceReplay struct {
	legacy *recoveryDocumentPiOrder
	arrays []nativeSequenceWitness
}

type nativeSequenceWitness struct {
	Alias   recoveryDocumentAliasRef `json:"document"`
	Path    []string                 `json:"path"`
	Syntax  string                   `json:"syntax"`
	Removed []int                    `json:"removed"`
	Order   []int                    `json:"order"`
}

func (replay selectedSequenceReplay) reconstruct(ctx context.Context, input journaldocument.ComparisonInput, participants []compose.Participant) (journaldocument.SelectedCandidate, error) {
	if replay.legacy != nil {
		order, err := replay.legacy.Evidence.canonical(ctx, input.Before)
		if err != nil {
			return journaldocument.SelectedCandidate{}, err
		}
		return ReconstructNative16Selected(ctx, input, participants, &order)
	}
	arrays := make([]configdocument.JSONArrayEdit, 0, len(replay.arrays))
	for _, witness := range replay.arrays {
		edit, err := witness.reconstruct(ctx, input)
		if err != nil {
			return journaldocument.SelectedCandidate{}, err
		}
		arrays = append(arrays, edit)
	}
	return journaldocument.ReconstructSelected(ctx, input, participants, arrays)
}

func (replay selectedSequenceReplay) documents() ([]aggregate.DocumentAddress, error) {
	var references []recoveryDocumentAliasRef
	if replay.legacy != nil {
		references = append(references, replay.legacy.Alias)
	}
	for _, witness := range replay.arrays {
		references = append(references, witness.Alias)
	}
	result := make([]aggregate.DocumentAddress, 0, len(references))
	for _, reference := range references {
		address, err := reference.canonical()
		if err != nil {
			return nil, err
		}
		result = append(result, address)
	}
	return result, nil
}

func native16RecordBody(record nativeJournalRecord) nativeRecordBody {
	return nativeRecordBody{
		OperationID: record.OperationID, Operation: record.Operation, CreatedAt: record.CreatedAt,
		StateAuthority: record.StateAuthority, Entries: record.Entries,
		StatefileBefore: record.StatefileBefore, StatefileAfter: record.StatefileAfter,
		ClaimTransitions: record.ClaimTransitions, ProvisionalAcquires: record.ProvisionalAcquires, RemovalIntents: record.RemovalIntents,
	}
}

func native16GroupBody(group recoveryDocumentGroup) nativeDocumentBody {
	return nativeDocumentBody{
		CapturedTarget: group.CapturedTarget, Extent: group.Extent, SelectedSubjects: group.SelectedSubjects,
		Original: group.Original, Expected: group.Expected, ExclusiveClaims: group.ExclusiveClaims,
		BindingIntents: group.BindingIntents, Removal: group.Removal,
	}
}

func (journal Native) native16Record() nativeJournalRecord {
	body := journal.record
	groups := make([]recoveryDocumentGroup, len(body.DocumentGroups))
	for index, group := range body.DocumentGroups {
		envelope := journal.legacy.groups[index]
		groups[index] = assembleNative16Document(group.nativeDocumentBody, envelope)
	}
	return nativeJournalRecord{
		Version: body.Version, OperationID: body.OperationID, Operation: body.Operation, CreatedAt: body.CreatedAt,
		StateAuthority: body.StateAuthority, ManifestRootProvenance: body.ManifestRootProvenance,
		Entries: body.Entries, StatefileBefore: body.StatefileBefore, StatefileAfter: body.StatefileAfter,
		ClaimTransitions: body.ClaimTransitions, ProvisionalAcquires: body.ProvisionalAcquires,
		RemovalIntents: body.RemovalIntents, DocumentGroups: groups,
	}
}

func assembleNative16Document(group nativeDocumentBody, envelope native16DocumentEnvelope) recoveryDocumentGroup {
	return recoveryDocumentGroup{
		CapturedTarget: group.CapturedTarget, Writer: envelope.writer, Aliases: envelope.aliases,
		Extent: group.Extent, SelectedSubjects: group.SelectedSubjects, PiOrder: envelope.order,
		Original: group.Original, Expected: group.Expected, ExclusiveClaims: group.ExclusiveClaims,
		BindingIntents: group.BindingIntents, Removal: group.Removal,
	}
}

func (journal Native) encode(ctx context.Context, codec durable.SnapshotCodec) ([]byte, error) {
	if err := journal.requireNativeValue(); err != nil {
		return nil, err
	}
	if journal.legacy != nil {
		return marshalNativeJournalRecord(ctx, journal.native16Record(), journal.before, journal.after, codec)
	}
	if journal.current != nil {
		return journal.encodeNative17(ctx, codec)
	}
	return nil, fmt.Errorf("native journal has no admitted version envelope")
}

// Legacy test/capture admission also uses the runtime's canonical namespace.
func lowerNative16Record(record nativeJournalRecord, documents []documentNamespaceFacts) (nativeRuntimeRecord, *native16Envelope) {
	body := nativeRuntimeRecord{
		nativeRecordBody: native16RecordBody(record), Version: record.Version,
		ManifestRootProvenance: record.ManifestRootProvenance,
		DocumentGroups:         make([]nativeDocumentRecord, len(record.DocumentGroups)),
	}
	envelope := &native16Envelope{groups: make([]native16DocumentEnvelope, len(record.DocumentGroups))}
	for index, group := range record.DocumentGroups {
		body.DocumentGroups[index] = nativeDocumentRecord{
			nativeDocumentBody: native16GroupBody(group), namespace: documents[index],
			replay: selectedSequenceReplay{legacy: group.PiOrder},
		}
		envelope.groups[index] = native16DocumentEnvelope{writer: group.Writer, aliases: group.Aliases, order: group.PiOrder}
	}
	return body, envelope
}

func (document nativeDocumentRecord) piQualification() (PiQualification, bool, error) {
	if document.replay.legacy == nil {
		return PiQualification{}, false, nil
	}
	facts, err := document.replay.legacy.qualificationContext()
	return facts, err == nil, err
}

func (witness nativeSequenceWitness) reconstruct(ctx context.Context, input journaldocument.ComparisonInput) (configdocument.JSONArrayEdit, error) {
	for _, image := range []aggregate.Document{input.Before, input.Expected} {
		if err := journaldocument.ValidateImage(ctx, image, input.Limits); err != nil {
			return configdocument.JSONArrayEdit{}, err
		}
	}
	if !input.Before.Exists() || !input.Expected.Exists() {
		return configdocument.JSONArrayEdit{}, fmt.Errorf("selected array requires existing before and expected documents")
	}
	syntax, err := witness.syntax()
	if err != nil {
		return configdocument.JSONArrayEdit{}, err
	}
	path, err := output.NewKeyPath(witness.Path...)
	if err != nil {
		return configdocument.JSONArrayEdit{}, err
	}
	before, err := configdocument.Parse(ctx, input.Before.Content(), syntax, input.Limits)
	if err != nil {
		return configdocument.JSONArrayEdit{}, err
	}
	expected, err := configdocument.Parse(ctx, input.Expected.Content(), syntax, input.Limits)
	if err != nil {
		return configdocument.JSONArrayEdit{}, err
	}
	return configdocument.NewJSONArrayEdit(ctx, before, expected, path, witness.Removed, witness.Order)
}

func legacyDocumentAliasFacts(aliases []recoveryDocumentAlias) ([]documentAliasFacts, error) {
	result := make([]documentAliasFacts, 0, len(aliases))
	for _, alias := range aliases {
		address, err := alias.Address.canonical()
		if err != nil {
			return nil, err
		}
		result = append(result, documentAliasFacts{address: address, before: alias.ObservedBefore, selected: alias.SelectedObservation})
	}
	return result, nil
}

func (document nativeDocumentRecord) writerPath() (string, error) {
	if err := document.namespace.writer.Validate(); err != nil {
		return "", err
	}
	if child, directory := document.namespace.writer.Coordinate().DirectoryChild(); directory {
		return filepath.Join(document.namespace.writer.Provenance().PhysicalRoot(), filepath.FromSlash(child)), nil
	}
	return document.namespace.writerSpelling, nil
}

func (document nativeDocumentRecord) validate() error {
	if err := document.CapturedTarget.validate(); err != nil {
		return err
	}
	if len(document.namespace.aliases) == 0 || document.SelectedSubjects == nil || document.BindingIntents == nil {
		return fmt.Errorf("document requires complete namespace, subject and binding facts")
	}
	if document.Extent != "whole" && document.Extent != "selected" {
		return fmt.Errorf("unsupported document comparison extent")
	}
	if document.Extent == "whole" && (len(document.SelectedSubjects) != 0 || document.replay.legacy != nil || len(document.replay.arrays) != 0) {
		return fmt.Errorf("whole document cannot share selected writers")
	}
	subjects := make(map[topology.SubjectID]bool, len(document.SelectedSubjects))
	for _, ref := range document.SelectedSubjects {
		subject, err := ref.canonical()
		if err != nil || subjects[subject] {
			return fmt.Errorf("document selected subjects must be valid unique identities")
		}
		subjects[subject] = true
	}
	if err := document.Original.validate(); err != nil {
		return err
	}
	if err := document.Expected.validate(); err != nil {
		return err
	}
	if err := document.ExclusiveClaims.validateShape(); err != nil {
		return err
	}
	for _, binding := range document.BindingIntents {
		if _, err := binding.canonical(); err != nil {
			return err
		}
	}
	return document.validateRemoval()
}

func (document nativeDocumentRecord) validateRemoval() error {
	writer, err := document.writerPath()
	if err != nil {
		return err
	}
	return validateDocumentRemoval(document.nativeDocumentBody, document.namespace.aliases, writer)
}

func (document nativeDocumentRecord) validateRemovalImages(images LoadedImages) error {
	if err := document.validateRemoval(); err != nil {
		return err
	}
	return document.nativeDocumentBody.validateRemovalImages(images)
}
