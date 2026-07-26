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
	if err := validateContributionIdentityComponent("kind", spec.Kind); err != nil {
		return Contribution{}, err
	}
	if err := validateContributionIdentityComponent("key", spec.Key); err != nil {
		return Contribution{}, err
	}
	payload, err := json.Marshal(contributionIdentityPayload{
		Provider: provider.SubjectID().String(),
		Kind:     spec.Kind,
		Key:      spec.Key,
	})
	if err != nil {
		return Contribution{}, fmt.Errorf("encode provider contribution identity: %w", err)
	}
	subject, err := topology.NewSubjectID(
		topology.SubjectContribution,
		provider.SubjectID().Namespace()+".contribution",
		string(payload),
	)
	if err != nil {
		return Contribution{}, fmt.Errorf("provider contribution subject: %w", err)
	}
	return Contribution{provider: provider, kind: spec.Kind, key: spec.Key, subject: subject}, nil
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

// SubjectID returns the canonical provider-scoped contribution identity.
func (contribution Contribution) SubjectID() topology.SubjectID { return contribution.subject }

// Kind returns the provider-bundled contribution kind.
func (contribution Contribution) Kind() string { return contribution.kind }

// Key returns the contribution key inside the provider namespace.
func (contribution Contribution) Key() string { return contribution.key }

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
