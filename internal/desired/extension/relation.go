package extension

import (
	"fmt"
	"strings"

	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/target"
)

// Spec is constructor input for one canonical Extension.
type Spec struct {
	Name    string
	Carrier Carrier
	Target  target.Target
	Scope   target.Scope
	Source  SourceRef
}

// Extension is one immutable desired host-carrier relation.
type Extension struct {
	id         entity.ID
	carrierKey CarrierKey
}

// New constructs and validates a canonical extension relation.
func New(spec Spec) (Extension, error) {
	if err := validateStableToken(spec.Name); err != nil {
		return Extension{}, fmt.Errorf("extension id: %w", err)
	}
	id, err := entity.New(entity.KindExtension, spec.Name)
	if err != nil {
		return Extension{}, err
	}
	carrierKey, err := NewCarrierKey(spec.Carrier, spec.Target, spec.Scope, spec.Source)
	if err != nil {
		return Extension{}, err
	}
	return Extension{id: id, carrierKey: carrierKey}, nil
}

func validateStableToken(value string) error {
	if value == "" || strings.TrimSpace(value) != value {
		return fmt.Errorf("stable token is required")
	}
	for index := 0; index < len(value); index++ {
		character := value[index]
		asciiAlnum := (character >= 'A' && character <= 'Z') ||
			(character >= 'a' && character <= 'z') ||
			(character >= '0' && character <= '9')
		if index == 0 && !asciiAlnum {
			return fmt.Errorf("must start with an ASCII letter or digit")
		}
		if asciiAlnum || character == '.' || character == '_' || character == '-' {
			continue
		}
		return fmt.Errorf("must be a stable token")
	}
	return nil
}

// Validate rejects a zero or invalid Extension value.
func (extension Extension) Validate() error {
	if extension.id.Kind() != entity.KindExtension {
		return fmt.Errorf("extension has entity kind %q", extension.id.Kind())
	}
	_, err := New(Spec{
		Name: extension.id.Name(), Carrier: extension.carrierKey.Carrier(), Target: extension.carrierKey.Target(),
		Scope: extension.carrierKey.Scope(), Source: extension.carrierKey.Source(),
	})
	return err
}

// ID returns the authored desired identity.
func (extension Extension) ID() entity.ID { return extension.id }

// Carrier returns the host-native relation family.
func (extension Extension) Carrier() Carrier { return extension.carrierKey.Carrier() }

// Target returns the relation target.
func (extension Extension) Target() target.Target { return extension.carrierKey.Target() }

// Scope returns the relation scope.
func (extension Extension) Scope() target.Scope { return extension.carrierKey.Scope() }

// Source returns the canonical host-native source reference carried by this
// relation. A selected boundary may have resolved context-dependent spelling.
func (extension Extension) Source() SourceRef { return extension.carrierKey.Source() }

// CarrierKey returns carrier identity independent of declaration ID.
func (extension Extension) CarrierKey() CarrierKey { return extension.carrierKey }

// WithSource returns a revalidated extension with one context-resolved source
// while preserving declaration identity, carrier, target, and scope.
func (extension Extension) WithSource(source SourceRef) (Extension, error) {
	return New(Spec{
		Name:    extension.id.Name(),
		Carrier: extension.carrierKey.Carrier(),
		Target:  extension.carrierKey.Target(),
		Scope:   extension.carrierKey.Scope(),
		Source:  source,
	})
}
