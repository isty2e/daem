package observe

import (
	"cmp"
	"fmt"
	"slices"

	"github.com/isty2e/daem/internal/assurance/stateauthority"
	"github.com/isty2e/daem/internal/output/ownership"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/topology"
)

type NativeDocumentParticipantsInput struct {
	Owner           stateauthority.Authority
	Aliases         []aggregate.DocumentAddress
	BeforeSubjects  []topology.SubjectID
	CurrentBindings []ownership.DocumentBinding
}

// NativeDocumentParticipants retains supplied group membership and stored
// identity records. Physical qualification and freshness belong to its producer;
// neither this value nor a binding grants access or exclusive ownership.
type NativeDocumentParticipants struct {
	owner       stateauthority.Authority
	aliases     []aggregate.DocumentAddress
	subjects    []topology.SubjectID
	bindings    []ownership.DocumentBinding
	initialized bool
}

func NewNativeDocumentParticipants(input NativeDocumentParticipantsInput) (NativeDocumentParticipants, error) {
	if err := input.Owner.Validate(); err != nil {
		return NativeDocumentParticipants{}, err
	}
	aliases := slices.Clone(input.Aliases)
	if len(aliases) == 0 {
		return NativeDocumentParticipants{}, fmt.Errorf("native participants require observed aliases")
	}
	seen := make(map[aggregate.DocumentAddress]struct{}, len(aliases))
	for _, alias := range aliases {
		if err := alias.Validate(); err != nil {
			return NativeDocumentParticipants{}, err
		}
		if _, duplicate := seen[alias]; duplicate {
			return NativeDocumentParticipants{}, fmt.Errorf("native participants repeat an observed alias")
		}
		seen[alias] = struct{}{}
	}
	slices.SortFunc(aliases, func(left, right aggregate.DocumentAddress) int {
		for _, order := range []int{cmp.Compare(left.Target(), right.Target()), cmp.Compare(left.Scope(), right.Scope()), cmp.Compare(left.AggregateRoot().String(), right.AggregateRoot().String())} {
			if order != 0 {
				return order
			}
		}
		return 0
	})
	subjects := slices.Clone(input.BeforeSubjects)
	slices.SortFunc(subjects, topology.CompareSubjectID)
	for index, subject := range subjects {
		if err := subject.Validate(); err != nil {
			return NativeDocumentParticipants{}, err
		}
		if index != 0 && subjects[index-1] == subject {
			return NativeDocumentParticipants{}, fmt.Errorf("native participants repeat a before subject")
		}
	}
	bindings, err := ownership.NewRegistry(nil, input.CurrentBindings...)
	if err != nil {
		return NativeDocumentParticipants{}, err
	}
	return NativeDocumentParticipants{owner: input.Owner, aliases: aliases, subjects: subjects, bindings: bindings.DocumentBindings(), initialized: true}, nil
}

func (participants NativeDocumentParticipants) Validate() error {
	if !participants.initialized {
		return fmt.Errorf("native document participants are required")
	}
	return nil
}

func (participants NativeDocumentParticipants) Owner() stateauthority.Authority {
	return participants.owner
}

func (participants NativeDocumentParticipants) Aliases() []aggregate.DocumentAddress {
	return slices.Clone(participants.aliases)
}

func (participants NativeDocumentParticipants) BeforeSubjects() []topology.SubjectID {
	return slices.Clone(participants.subjects)
}

func (participants NativeDocumentParticipants) CurrentBindings() []ownership.DocumentBinding {
	return slices.Clone(participants.bindings)
}
