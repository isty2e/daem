package pathauthority

// Reference retains access spelling alongside a comparison identity. It is
// metadata, not filesystem authority, and is deliberately not comparable.
type Reference struct {
	_        [0]func()
	identity Exact
	locator  string
}

// NewReference checks syntax without establishing filesystem equality or rights.
func NewReference(identity Exact, locator string) (Reference, error) {
	reference := Reference{identity: identity, locator: locator}
	if err := reference.Validate(); err != nil {
		return Reference{}, err
	}
	return reference, nil
}

func (reference Reference) Validate() error {
	if err := reference.identity.Validate(); err != nil {
		return err
	}
	return validateAbsoluteCleanPath("path reference locator", reference.locator)
}

func (reference Reference) Identity() Exact { return reference.identity }

func (reference Reference) Locator() string { return reference.locator }

// Equal compares recorded identity and locator strings, not filesystem equivalence.
func (reference Reference) Equal(other Reference) bool {
	return reference.identity.Equal(other.identity) && reference.locator == other.locator
}

func (reference Reference) IsZero() bool {
	return reference.identity.IsZero() && reference.locator == ""
}
