package extension

import (
	"encoding/json"
	"fmt"

	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	"github.com/isty2e/daem/internal/topology"
)

// Carrier is one canonical provider/carrier subject. Declaration-local host
// relations may share it when all structural identity facts are equal.
type Carrier struct {
	key     desiredextension.CarrierKey
	subject topology.SubjectID
}

type carrierIdentityPayload struct {
	Target     string                      `json:"target"`
	Scope      string                      `json:"scope"`
	SourceKind desiredextension.SourceKind `json:"source_kind"`
	SourceRef  string                      `json:"source_ref"`
}

// NewCarrier constructs one canonical carrier subject from structural desired
// facts. The subject does not claim installation, ownership, or readiness.
func NewCarrier(key desiredextension.CarrierKey) (Carrier, error) {
	if err := key.Validate(); err != nil {
		return Carrier{}, fmt.Errorf("carrier topology: %w", err)
	}
	namespace, err := namespaceFor(key.Carrier())
	if err != nil {
		return Carrier{}, err
	}
	subjectKey, err := canonicalCarrierSubjectKey(key)
	if err != nil {
		return Carrier{}, err
	}
	subject, err := topology.NewSubjectID(topology.SubjectCarrier, namespace, subjectKey)
	if err != nil {
		return Carrier{}, fmt.Errorf("carrier topology subject: %w", err)
	}
	return Carrier{key: key, subject: subject}, nil
}

func carrierFor(value desiredextension.Extension) (Carrier, error) {
	if err := value.Validate(); err != nil {
		return Carrier{}, fmt.Errorf("extension carrier: %w", err)
	}
	return NewCarrier(value.CarrierKey())
}

// Validate rejects a zero or forged Carrier.
func (carrier Carrier) Validate() error {
	expected, err := NewCarrier(carrier.key)
	if err != nil {
		return err
	}
	if carrier.subject != expected.subject {
		return fmt.Errorf("carrier topology subject %q does not match canonical identity %q", carrier.subject, expected.subject)
	}
	return nil
}

// SubjectID returns the canonical structural carrier identity.
func (carrier Carrier) SubjectID() topology.SubjectID { return carrier.subject }

// Family returns the host-native carrier family.
func (carrier Carrier) Family() desiredextension.Carrier { return carrier.key.Carrier() }

// Key returns the canonical desired carrier identity facts.
func (carrier Carrier) Key() desiredextension.CarrierKey { return carrier.key }

// Source returns the exact unresolved host-native provider reference.
func (carrier Carrier) Source() desiredextension.SourceRef { return carrier.key.Source() }

// RelationEvidence returns the passive relation-identity precision selected by
// this carrier's canonical source interpretation.
func (carrier Carrier) RelationEvidence() (RelationEvidenceClass, error) {
	if err := carrier.Validate(); err != nil {
		return "", err
	}
	source, err := InterpretCarrierSource(carrier.key)
	if err != nil {
		return "", err
	}
	return source.RelationEvidence(), nil
}

func canonicalCarrierSubjectKey(key desiredextension.CarrierKey) (string, error) {
	payload, err := json.Marshal(carrierIdentityPayload{
		Target:     string(key.Target()),
		Scope:      string(key.Scope()),
		SourceKind: key.Source().Kind(),
		SourceRef:  key.Source().Ref(),
	})
	if err != nil {
		return "", fmt.Errorf("encode carrier topology identity: %w", err)
	}
	return string(payload), nil
}
