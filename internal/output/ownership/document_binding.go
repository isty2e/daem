package ownership

import (
	"fmt"
	"strings"

	"github.com/isty2e/daem/internal/assurance/pathauthority"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	"github.com/isty2e/daem/internal/output"
)

// DocumentBinding retains a logical document's exact physical namespace across
// runs. It is neither an exclusive claim nor a live filesystem capability.
type DocumentBinding struct {
	owner       stateauthority.Authority
	destination output.Destination
	physical    pathauthority.Exact
}

type documentBindingKey struct {
	owner       stateauthority.Key
	destination output.Destination
}

func NewDocumentBinding(owner stateauthority.Authority, destination output.Destination, physical pathauthority.Exact) (DocumentBinding, error) {
	binding := DocumentBinding{owner: owner, destination: destination, physical: physical}
	if err := binding.Validate(); err != nil {
		return DocumentBinding{}, err
	}
	return binding, nil
}

func (binding DocumentBinding) Validate() error {
	if err := binding.owner.Validate(); err != nil {
		return fmt.Errorf("document binding owner: %w", err)
	}
	if err := binding.destination.Validate(); err != nil {
		return fmt.Errorf("document binding destination: %w", err)
	}
	if err := binding.physical.Validate(); err != nil {
		return fmt.Errorf("document binding physical namespace: %w", err)
	}
	return nil
}

func (binding DocumentBinding) Owner() stateauthority.Authority { return binding.owner }
func (binding DocumentBinding) Destination() output.Destination { return binding.destination }
func (binding DocumentBinding) PathAuthority() pathauthority.Exact {
	return binding.physical
}

// Equal includes diagnostic provenance for exact persisted before/after checks;
// provenance does not participate in the owner/destination identity key.
func (binding DocumentBinding) Equal(other DocumentBinding) bool {
	return binding.Validate() == nil && other.Validate() == nil &&
		binding.owner.ExactEqual(other.owner) && binding.destination == other.destination &&
		binding.physical.Equal(other.physical)
}

func (binding DocumentBinding) key() documentBindingKey {
	return documentBindingKey{owner: binding.owner.Key(), destination: binding.destination}
}

func (binding DocumentBinding) less(other DocumentBinding) bool {
	if order := binding.owner.Key().Compare(other.owner.Key()); order != 0 {
		return order < 0
	}
	return strings.Compare(binding.destination.String(), other.destination.String()) < 0
}

// DocumentBindingValue distinguishes absent identity evidence from a present
// exact record. Neither variant grants host mutation authority.
type DocumentBindingValue struct{ binding *DocumentBinding }

func NoDocumentBinding() DocumentBindingValue { return DocumentBindingValue{} }

func PresentDocumentBinding(binding DocumentBinding) (DocumentBindingValue, error) {
	if err := binding.Validate(); err != nil {
		return DocumentBindingValue{}, err
	}
	return DocumentBindingValue{binding: &binding}, nil
}

func (value DocumentBindingValue) Get() (DocumentBinding, bool) {
	if value.binding == nil {
		return DocumentBinding{}, false
	}
	return *value.binding, true
}

func (value DocumentBindingValue) Equal(other DocumentBindingValue) bool {
	if value.binding == nil || other.binding == nil {
		return value.binding == nil && other.binding == nil
	}
	return value.binding.Equal(*other.binding)
}
