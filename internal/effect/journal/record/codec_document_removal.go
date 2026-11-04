package record

import (
	"encoding/json"
	"fmt"
	"path/filepath"

	journaldocument "github.com/isty2e/daem/internal/effect/journal/document"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
)

type recoveryDocumentRemoval struct {
	Demands   []recoveryDocumentRemovalDemand   `json:"demands"`
	Namespace recoveryRemovalNamespaceAuthority `json:"namespace_authority"`
	States    []recoveryRemovalState            `json:"states"`
}

type recoveryDocumentRemovalDemand struct {
	Scope       string `json:"scope"`
	Destination string `json:"destination"`
}

func (record *recoveryDocumentRemoval) UnmarshalJSON(content []byte) error {
	if err := requireDocumentEvidenceFields(content, "demands", "namespace_authority", "states"); err != nil {
		return err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(content, &fields); err != nil {
		return err
	}
	if err := requireRecoveryJSONArrayMaximum(fields["demands"], "document removal demands", recovery.MaximumRemovalIntents); err != nil {
		return err
	}
	if err := requireRecoveryJSONArrayMaximum(fields["states"], "document removal states", recovery.MaximumRemovalStatesPerIntent); err != nil {
		return err
	}
	type wire recoveryDocumentRemoval
	var decoded wire
	if err := decodeRecoveryJSONStrict(content, &decoded); err != nil {
		return err
	}
	*record = recoveryDocumentRemoval(decoded)
	return nil
}

func (record *recoveryDocumentRemovalDemand) UnmarshalJSON(content []byte) error {
	if err := requireDocumentEvidenceFields(content, "scope", "destination"); err != nil {
		return err
	}
	type wire recoveryDocumentRemovalDemand
	var decoded wire
	if err := decodeRecoveryJSONStrict(content, &decoded); err != nil {
		return err
	}
	*record = recoveryDocumentRemovalDemand(decoded)
	return nil
}

func (record recoveryDocumentRemoval) canonical() (recovery.DocumentRemoval, error) {
	values := make([]recoveryRemovalIntent, 0, len(record.Demands))
	for _, demand := range record.Demands {
		values = append(values, recoveryRemovalIntent{Scope: demand.Scope, Destination: demand.Destination, States: record.States, Namespace: record.Namespace})
	}
	intents, err := canonicalRecoveryRemovalIntents(values)
	if err != nil {
		return recovery.DocumentRemoval{}, err
	}
	demands := make([]recovery.RemovalDemand, 0, len(intents))
	for _, intent := range intents {
		demands = append(demands, intent.Demand())
	}
	set, err := recovery.NewRemovalDemandSet(demands)
	if err != nil {
		return recovery.DocumentRemoval{}, err
	}
	ordered := set.Demands()
	for index, intent := range intents {
		if intent.Scope() != ordered[index].Scope() || intent.Destination() != ordered[index].Destination() {
			return recovery.DocumentRemoval{}, fmt.Errorf("document removal demands are not canonically ordered")
		}
	}
	if len(intents) == 0 {
		return recovery.DocumentRemoval{}, fmt.Errorf("document removal has no logical demands")
	}
	return recovery.NewDocumentRemoval(set, intents[0].Namespace())
}

func persistedDocumentRemoval(removal recovery.DocumentRemoval) (recoveryDocumentRemoval, error) {
	physical, err := removal.PhysicalIntent()
	if err != nil {
		return recoveryDocumentRemoval{}, err
	}
	values, err := persistedRecoveryRemovalIntents([]recovery.RemovalIntent{physical})
	if err != nil {
		return recoveryDocumentRemoval{}, err
	}
	result := recoveryDocumentRemoval{Namespace: values[0].Namespace, States: values[0].States, Demands: make([]recoveryDocumentRemovalDemand, 0, removal.Demands().Len())}
	for _, value := range removal.Demands().Demands() {
		result.Demands = append(result.Demands, recoveryDocumentRemovalDemand{Scope: string(value.Scope()), Destination: value.Destination().String()})
	}
	return result, nil
}

func (record recoveryDocumentGroup) validateRemoval() error {
	required := (record.Original.Kind == "absent") != (record.Expected.Kind == "absent")
	if !required {
		if record.Removal != nil {
			return fmt.Errorf("unchanged document root presence cannot carry removal authority")
		}
		return nil
	}
	if record.Removal == nil {
		return fmt.Errorf("document root transition requires complete removal demands")
	}
	if record.Extent == "selected" && record.Original.Kind == "file" {
		return fmt.Errorf("selected removal must preserve a pre-existing document root")
	}
	removal, err := record.Removal.canonical()
	if err != nil {
		return err
	}
	aliases := make(map[removalRelationKey]struct{}, len(record.Aliases))
	for _, alias := range record.Aliases {
		address, err := alias.Address.canonical()
		if err != nil {
			return err
		}
		aliases[removalRelationKey{scope: address.Scope(), destination: address.AggregateRoot()}] = struct{}{}
	}
	for _, demand := range removal.Demands().Demands() {
		if _, found := aliases[removalRelationKey{scope: demand.Scope(), destination: demand.Destination()}]; !found {
			return fmt.Errorf("document removal demand has no declared alias")
		}
	}
	parent := record.Removal.Namespace.ParentProvenance
	var actual string
	if parent != nil {
		actual = parent.PhysicalRoot
	} else if ancestor := record.Removal.Namespace.RetainedAncestorProvenance; ancestor != nil {
		actual = filepath.Join(ancestor.PhysicalRoot, filepath.FromSlash(record.Removal.Namespace.MissingSuffix))
	}
	want := filepath.Dir(filepath.Join(record.Writer.Root.PhysicalRoot, filepath.FromSlash(record.Writer.RelativePath)))
	if actual != want {
		return fmt.Errorf("document removal namespace does not belong to its physical writer")
	}
	return nil
}

func (record recoveryDocumentGroup) validateRemovalImages(images LoadedImages) error {
	if err := record.validateRemoval(); err != nil || record.Removal == nil {
		return err
	}
	states, err := journaldocument.RemovalStates(images.original.document, images.original.mode, images.expected.document, images.expected.mode)
	if err != nil {
		return err
	}
	removal, err := record.Removal.canonical()
	if err != nil {
		return err
	}
	for _, demand := range removal.Demands().Demands() {
		actual := demand.States()
		if len(actual) != len(states) {
			return fmt.Errorf("document removal states do not match its complete root transition")
		}
		for index, state := range states {
			if !state.Equal(actual[index]) {
				return fmt.Errorf("document removal states do not match actual image bytes and modes")
			}
		}
	}
	return nil
}
