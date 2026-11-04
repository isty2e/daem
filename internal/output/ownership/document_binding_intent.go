package ownership

import (
	"fmt"
	"strings"

	"github.com/isty2e/daem/internal/assurance/pathauthority"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	"github.com/isty2e/daem/internal/output"
)

// DocumentBindingIntent records exact before and exact/absent or provisional
// after identity evidence. It reserves no registry row or filesystem namespace.
type DocumentBindingIntent struct {
	owner       stateauthority.Authority
	destination output.Destination
	before      DocumentBindingValue
	after       DocumentBindingValue
	provisional pathauthority.Provisional
}

func NewExactDocumentBindingIntent(owner stateauthority.Authority, destination output.Destination, before, after DocumentBindingValue) (DocumentBindingIntent, error) {
	intent := DocumentBindingIntent{owner: owner, destination: destination, before: before, after: after}
	if err := intent.Validate(); err != nil {
		return DocumentBindingIntent{}, err
	}

	// Stored records, not caller aliases, own persisted diagnostic provenance.
	if binding, present := before.Get(); present {
		intent.owner = binding.Owner()
	} else if binding, present := after.Get(); present {
		intent.owner = binding.Owner()
	}
	return intent, nil
}

func NewProvisionalDocumentBindingIntent(owner stateauthority.Authority, destination output.Destination, candidate pathauthority.Provisional) (DocumentBindingIntent, error) {
	if err := candidate.Validate(); err != nil {
		return DocumentBindingIntent{}, err
	}
	intent := DocumentBindingIntent{owner: owner, destination: destination, provisional: candidate}
	if err := intent.Validate(); err != nil {
		return DocumentBindingIntent{}, err
	}
	return intent, nil
}

func (intent DocumentBindingIntent) Validate() error {
	if err := intent.owner.Validate(); err != nil {
		return fmt.Errorf("document binding intent owner: %w", err)
	}
	if err := intent.destination.Validate(); err != nil {
		return fmt.Errorf("document binding intent destination: %w", err)
	}
	for _, value := range []DocumentBindingValue{intent.before, intent.after} {
		if binding, present := value.Get(); present {
			if err := binding.Validate(); err != nil {
				return err
			}
			if binding.key() != intent.key() {
				return fmt.Errorf("document binding intent changes its owner or destination key")
			}
		}
	}

	before, hadBefore := intent.before.Get()
	after, hasAfter := intent.after.Get()
	if !intent.provisional.IsZero() {
		if hadBefore || hasAfter {
			return fmt.Errorf("provisional binding intent requires absent before and no exact after")
		}
		return intent.provisional.Validate()
	}
	if !hadBefore && !hasAfter {
		return fmt.Errorf("document binding intent cannot retain absence")
	}
	if hadBefore && hasAfter && !before.Equal(after) {
		return fmt.Errorf("document binding intent cannot change an existing exact record")
	}
	return nil
}

func (intent DocumentBindingIntent) Owner() stateauthority.Authority { return intent.owner }
func (intent DocumentBindingIntent) Destination() output.Destination { return intent.destination }
func (intent DocumentBindingIntent) Before() DocumentBindingValue    { return intent.before }

// KnownAfter distinguishes an exact/absent target from pending path evidence.
func (intent DocumentBindingIntent) KnownAfter() (DocumentBindingValue, bool) {
	if intent.Validate() != nil || !intent.provisional.IsZero() {
		return NoDocumentBinding(), false
	}
	return intent.after, true
}

func (intent DocumentBindingIntent) ProvisionalAfter() (pathauthority.Provisional, bool) {
	if intent.Validate() != nil || intent.provisional.IsZero() {
		return pathauthority.Provisional{}, false
	}
	return intent.provisional, true
}

func (intent DocumentBindingIntent) ChangesBinding() (bool, error) {
	if err := intent.Validate(); err != nil {
		return false, err
	}
	return !intent.provisional.IsZero() || !intent.before.Equal(intent.after), nil
}

// Promote checks comparison evidence only. Its caller must have freshly
// observed the selected path under the appropriate rooted namespace authority.
func (intent DocumentBindingIntent) Promote(exact pathauthority.Exact) (DocumentBindingIntent, error) {
	if err := intent.Validate(); err != nil {
		return DocumentBindingIntent{}, err
	}
	if intent.provisional.IsZero() {
		return DocumentBindingIntent{}, fmt.Errorf("document binding intent has no provisional target")
	}
	if err := intent.provisional.AdmitsExact(exact); err != nil {
		return DocumentBindingIntent{}, err
	}
	observed, err := NewDocumentBinding(intent.owner, intent.destination, exact)
	if err != nil {
		return DocumentBindingIntent{}, err
	}
	value, err := PresentDocumentBinding(observed)
	if err != nil {
		return DocumentBindingIntent{}, err
	}
	return NewExactDocumentBindingIntent(intent.owner, intent.destination, intent.before, value)
}

func (intent DocumentBindingIntent) Equal(other DocumentBindingIntent) bool {
	return intent.Validate() == nil && other.Validate() == nil &&
		intent.owner.ExactEqual(other.owner) && intent.destination == other.destination &&
		intent.before.Equal(other.before) && intent.after.Equal(other.after) &&
		(intent.provisional.IsZero() && other.provisional.IsZero() || intent.provisional.Equal(other.provisional))
}

func (intent DocumentBindingIntent) key() documentBindingKey {
	return documentBindingKey{owner: intent.owner.Key(), destination: intent.destination}
}

func (intent DocumentBindingIntent) less(other DocumentBindingIntent) bool {
	if order := intent.owner.Key().Compare(other.owner.Key()); order != 0 {
		return order < 0
	}
	return strings.Compare(intent.destination.String(), other.destination.String()) < 0
}
