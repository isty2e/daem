package hostrelation

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	"github.com/isty2e/daem/internal/topology"
)

const managedInstanceKeyVersion = "host-relation:v1:"

// SubjectKey is the host-visible key expected for one relation.
type SubjectKey string

// ManagedInstanceKey is daem's stable correlation key for one desired
// host-managed relation.
type ManagedInstanceKey string

// ExpectedRelation pairs the host-visible identity with daem's independent
// managed-instance correlation key. Neither key grants mutation authority.
type ExpectedRelation struct {
	subjectKey         SubjectKey
	managedInstanceKey ManagedInstanceKey
}

// NewSubjectKey validates one host-visible relation key.
func NewSubjectKey(value string) (SubjectKey, error) {
	if err := validateText("relation subject key", value); err != nil {
		return "", err
	}
	return SubjectKey(value), nil
}

// NewManagedInstanceKey validates one daem correlation key.
func NewManagedInstanceKey(value string) (ManagedInstanceKey, error) {
	if err := validateText("managed instance key", value); err != nil {
		return "", err
	}
	return ManagedInstanceKey(value), nil
}

// NewExpectedRelation validates and pairs independently reconstructed relation
// keys. Use Derive when canonical Desired and Topology facts are available.
func NewExpectedRelation(subjectKey SubjectKey, managedKey ManagedInstanceKey) (ExpectedRelation, error) {
	if _, err := NewSubjectKey(string(subjectKey)); err != nil {
		return ExpectedRelation{}, err
	}
	if _, err := NewManagedInstanceKey(string(managedKey)); err != nil {
		return ExpectedRelation{}, err
	}
	return ExpectedRelation{subjectKey: subjectKey, managedInstanceKey: managedKey}, nil
}

// Derive constructs the expected relation and deterministically derives its
// versioned managed-instance key from canonical Desired and Topology identity.
func Derive(
	carrier desiredextension.CarrierKey,
	subject topology.SubjectID,
	subjectKey SubjectKey,
) (ExpectedRelation, error) {
	if err := carrier.Validate(); err != nil {
		return ExpectedRelation{}, fmt.Errorf("expected host relation carrier: %w", err)
	}
	if err := subject.Validate(); err != nil {
		return ExpectedRelation{}, fmt.Errorf("expected host relation subject: %w", err)
	}
	if subject.Kind() != topology.SubjectHostRelation {
		return ExpectedRelation{}, fmt.Errorf(
			"expected host relation subject kind = %q, want %q",
			subject.Kind(),
			topology.SubjectHostRelation,
		)
	}
	validatedSubjectKey, err := NewSubjectKey(string(subjectKey))
	if err != nil {
		return ExpectedRelation{}, err
	}
	managedKey, err := deriveManagedInstanceKey(carrier, subject)
	if err != nil {
		return ExpectedRelation{}, err
	}
	return NewExpectedRelation(validatedSubjectKey, managedKey)
}

// Validate rejects zero or forged expected-relation values.
func (relation ExpectedRelation) Validate() error {
	_, err := NewExpectedRelation(relation.subjectKey, relation.managedInstanceKey)
	return err
}

// SubjectKey returns the expected host-visible identity.
func (relation ExpectedRelation) SubjectKey() SubjectKey {
	return relation.subjectKey
}

// ManagedInstanceKey returns daem's expected correlation identity.
func (relation ExpectedRelation) ManagedInstanceKey() ManagedInstanceKey {
	return relation.managedInstanceKey
}

// Equal compares both independent relation identities.
func (relation ExpectedRelation) Equal(other ExpectedRelation) bool {
	return relation.Validate() == nil &&
		other.Validate() == nil &&
		relation == other
}

type managedInstancePayload struct {
	Carrier          desiredextension.Carrier    `json:"carrier"`
	Target           string                      `json:"target"`
	Scope            string                      `json:"scope"`
	SourceKind       desiredextension.SourceKind `json:"source_kind"`
	SourceRef        string                      `json:"source_ref"`
	SubjectKind      topology.SubjectKind        `json:"subject_kind"`
	SubjectNamespace string                      `json:"subject_namespace"`
	SubjectKey       string                      `json:"subject_key"`
}

func deriveManagedInstanceKey(
	carrier desiredextension.CarrierKey,
	subject topology.SubjectID,
) (ManagedInstanceKey, error) {
	payload := managedInstancePayload{
		Carrier:          carrier.Carrier(),
		Target:           string(carrier.Target()),
		Scope:            string(carrier.Scope()),
		SourceKind:       carrier.Source().Kind(),
		SourceRef:        carrier.Source().Ref(),
		SubjectKind:      subject.Kind(),
		SubjectNamespace: subject.Namespace(),
		SubjectKey:       subject.Key(),
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("encode managed instance identity: %w", err)
	}
	return NewManagedInstanceKey(managedInstanceKeyVersion + string(data))
}

func validateText(label string, value string) error {
	if strings.TrimSpace(value) == "" || strings.TrimSpace(value) != value {
		return fmt.Errorf("%s must be non-empty and trimmed", label)
	}
	if !utf8.ValidString(value) {
		return fmt.Errorf("%s must be valid UTF-8", label)
	}
	if strings.IndexFunc(value, func(character rune) bool {
		return unicode.IsControl(character) || unicode.Is(unicode.Bidi_Control, character)
	}) >= 0 {
		return fmt.Errorf("%s must not contain control characters", label)
	}
	return nil
}
