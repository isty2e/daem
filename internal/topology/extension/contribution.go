package extension

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/isty2e/daem/internal/topology"
)

// ContributionSpec identifies one provider-scoped contribution. Kind and key
// are structural identity components, not standalone managed resource kinds.
type ContributionSpec struct {
	Kind string
	Key  string
}

// Contribution is one provider-scoped structural capability subject.
type Contribution struct {
	provider Carrier
	kind     string
	key      string
	subject  topology.SubjectID
}

// ContributionReference is the lossless foreign-key view of one canonical
// provider-scoped contribution identity. It carries no provider installation,
// source, version, activation, or readiness facts.
type ContributionReference struct {
	provider topology.SubjectID
	kind     string
	key      string
	subject  topology.SubjectID
}

// ContributionModel is one immutable provider and its declared structural
// contributions. It carries no current installation or activation evidence.
type ContributionModel struct {
	contributions []Contribution
}

type contributionIdentityPayload struct {
	Provider string `json:"provider"`
	Kind     string `json:"kind"`
	Key      string `json:"key"`
}

// NewContribution constructs one canonical provider-scoped contribution.
func NewContribution(provider Carrier, spec ContributionSpec) (Contribution, error) {
	if err := provider.Validate(); err != nil {
		return Contribution{}, fmt.Errorf("contribution provider: %w", err)
	}
	reference, err := NewContributionReference(provider.SubjectID(), spec)
	if err != nil {
		return Contribution{}, err
	}
	return Contribution{
		provider: provider,
		kind:     reference.kind,
		key:      reference.key,
		subject:  reference.subject,
	}, nil
}

// NewContributionReference constructs one canonical contribution foreign key
// from an already canonical carrier SubjectID.
func NewContributionReference(
	provider topology.SubjectID,
	spec ContributionSpec,
) (ContributionReference, error) {
	if err := provider.Validate(); err != nil {
		return ContributionReference{}, fmt.Errorf("contribution provider subject: %w", err)
	}
	if provider.Kind() != topology.SubjectCarrier {
		return ContributionReference{}, fmt.Errorf(
			"contribution provider subject kind must be %q, got %q",
			topology.SubjectCarrier,
			provider.Kind(),
		)
	}
	if err := validateContributionIdentityComponent("kind", spec.Kind); err != nil {
		return ContributionReference{}, err
	}
	if err := validateContributionIdentityComponent("key", spec.Key); err != nil {
		return ContributionReference{}, err
	}
	payload, err := json.Marshal(contributionIdentityPayload{
		Provider: provider.String(),
		Kind:     spec.Kind,
		Key:      spec.Key,
	})
	if err != nil {
		return ContributionReference{}, fmt.Errorf("encode provider contribution identity: %w", err)
	}
	subject, err := topology.NewSubjectID(
		topology.SubjectContribution,
		provider.Namespace()+".contribution",
		string(payload),
	)
	if err != nil {
		return ContributionReference{}, fmt.Errorf("provider contribution subject: %w", err)
	}
	return ContributionReference{
		provider: provider,
		kind:     spec.Kind,
		key:      spec.Key,
		subject:  subject,
	}, nil
}

// ParseContributionReference reconstructs and validates one canonical
// contribution SubjectID without reconstructing provider-owned carrier facts.
func ParseContributionReference(subject topology.SubjectID) (ContributionReference, error) {
	if err := subject.Validate(); err != nil {
		return ContributionReference{}, fmt.Errorf("provider contribution subject: %w", err)
	}
	if subject.Kind() != topology.SubjectContribution {
		return ContributionReference{}, fmt.Errorf(
			"provider contribution subject kind must be %q, got %q",
			topology.SubjectContribution,
			subject.Kind(),
		)
	}

	var payload contributionIdentityPayload
	if err := json.Unmarshal([]byte(subject.Key()), &payload); err != nil {
		return ContributionReference{}, fmt.Errorf("decode provider contribution identity: %w", err)
	}
	provider, err := topology.ParseSubjectID(payload.Provider)
	if err != nil {
		return ContributionReference{}, fmt.Errorf("provider contribution identity: %w", err)
	}
	reference, err := NewContributionReference(provider, ContributionSpec{
		Kind: payload.Kind,
		Key:  payload.Key,
	})
	if err != nil {
		return ContributionReference{}, err
	}
	if reference.subject != subject {
		return ContributionReference{}, fmt.Errorf(
			"provider contribution subject %q is not canonical; want %q",
			subject,
			reference.subject,
		)
	}
	return reference, nil
}

// ProviderContributions constructs one validated provider/contribution graph.
// Duplicate provider-scoped identities are rejected instead of silently merged.
func ProviderContributions(provider Carrier, specs []ContributionSpec) (ContributionModel, error) {
	if err := provider.Validate(); err != nil {
		return ContributionModel{}, fmt.Errorf("contribution provider: %w", err)
	}
	contributions := make([]Contribution, 0, len(specs))
	seen := make(map[topology.SubjectID]struct{}, len(specs))
	for index, spec := range specs {
		contribution, err := NewContribution(provider, spec)
		if err != nil {
			return ContributionModel{}, fmt.Errorf("provider contribution[%d]: %w", index, err)
		}
		if _, duplicate := seen[contribution.SubjectID()]; duplicate {
			return ContributionModel{}, fmt.Errorf(
				"duplicate provider contribution identity %q",
				contribution.SubjectID(),
			)
		}
		seen[contribution.SubjectID()] = struct{}{}
		contributions = append(contributions, contribution)
	}
	sort.Slice(contributions, func(left int, right int) bool {
		return topology.CompareSubjectID(
			contributions[left].SubjectID(),
			contributions[right].SubjectID(),
		) < 0
	})
	return ContributionModel{
		contributions: contributions,
	}, nil
}

// Validate rejects a zero or forged Contribution.
func (contribution Contribution) Validate() error {
	expected, err := NewContribution(contribution.provider, ContributionSpec{
		Kind: contribution.kind,
		Key:  contribution.key,
	})
	if err != nil {
		return err
	}
	if contribution.subject != expected.subject {
		return fmt.Errorf(
			"provider contribution subject %q does not match canonical identity %q",
			contribution.subject,
			expected.subject,
		)
	}
	return nil
}

// Provider returns the exact structural carrier that provides this contribution.
func (contribution Contribution) Provider() Carrier { return contribution.provider }

// SubjectID returns the canonical provider-scoped contribution identity.
func (contribution Contribution) SubjectID() topology.SubjectID { return contribution.subject }

// Reference returns the lossless foreign-key view of this contribution.
func (contribution Contribution) Reference() ContributionReference {
	return ContributionReference{
		provider: contribution.provider.SubjectID(),
		kind:     contribution.kind,
		key:      contribution.key,
		subject:  contribution.subject,
	}
}

// Kind returns the provider-bundled contribution kind.
func (contribution Contribution) Kind() string { return contribution.kind }

// Key returns the contribution key inside the provider namespace.
func (contribution Contribution) Key() string { return contribution.key }

// Validate rejects a zero, malformed, or non-canonical contribution reference.
func (reference ContributionReference) Validate() error {
	parsed, err := ParseContributionReference(reference.subject)
	if err != nil {
		return err
	}
	if reference != parsed {
		return fmt.Errorf("provider contribution reference does not match canonical identity")
	}
	return nil
}

// SubjectID returns the canonical contribution foreign key.
func (reference ContributionReference) SubjectID() topology.SubjectID { return reference.subject }

// ProviderSubjectID returns the exact carrier subject named by this reference.
func (reference ContributionReference) ProviderSubjectID() topology.SubjectID {
	return reference.provider
}

// Kind returns the provider-bundled contribution kind.
func (reference ContributionReference) Kind() string { return reference.kind }

// Key returns the contribution key inside the provider namespace.
func (reference ContributionReference) Key() string { return reference.key }

// Equal reports whether two references identify the same provider contribution.
func (reference ContributionReference) Equal(other ContributionReference) bool {
	return reference == other
}

// Contributions returns a deterministic defensive copy.
func (model ContributionModel) Contributions() []Contribution {
	return append([]Contribution(nil), model.contributions...)
}

func validateContributionIdentityComponent(label string, value string) error {
	if value == "" || strings.TrimSpace(value) != value {
		return fmt.Errorf("provider contribution %s must be non-empty and trimmed", label)
	}
	if !utf8.ValidString(value) {
		return fmt.Errorf("provider contribution %s must be valid UTF-8", label)
	}
	if strings.IndexFunc(value, func(character rune) bool {
		return unicode.IsControl(character) || unicode.Is(unicode.Bidi_Control, character)
	}) >= 0 {
		return fmt.Errorf("provider contribution %s must not contain control or bidirectional formatting characters", label)
	}
	return nil
}
