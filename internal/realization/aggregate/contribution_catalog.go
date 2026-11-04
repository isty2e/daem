package aggregate

import (
	"fmt"
	"reflect"
)

// ContributionValidator admits canonical desired values without requiring a
// host document or the capability to read, render or restore one.
type ContributionValidator interface {
	ContractID() CodecContractID
	ValidateContributions(ContributionSet) error
}

// ContributionCatalog is an immutable exact-ID index of value admission ports.
type ContributionCatalog struct {
	byContract map[CodecContractID]ContributionValidator
}

func NewContributionCatalog(validators []ContributionValidator) (ContributionCatalog, error) {
	if len(validators) == 0 {
		return ContributionCatalog{}, fmt.Errorf("aggregate contribution catalog is required")
	}
	byContract := make(map[CodecContractID]ContributionValidator, len(validators))
	for index, validator := range validators {
		if nilContributionValidator(validator) {
			return ContributionCatalog{}, fmt.Errorf("aggregate contribution catalog[%d] is nil", index)
		}
		id := validator.ContractID()
		if id == "" {
			return ContributionCatalog{}, fmt.Errorf("aggregate contribution catalog[%d] has an empty contract identity", index)
		}
		if _, duplicate := byContract[id]; duplicate {
			return ContributionCatalog{}, fmt.Errorf("aggregate codec contract %q is registered more than once", id)
		}
		byContract[id] = validator
	}
	return ContributionCatalog{byContract: byContract}, nil
}

// Validate checks that the immutable catalog was constructed successfully.
func (catalog ContributionCatalog) Validate() error {
	if len(catalog.byContract) == 0 {
		return fmt.Errorf("aggregate contribution catalog is required")
	}
	return nil
}

func (catalog ContributionCatalog) Lookup(id CodecContractID) (ContributionValidator, bool) {
	validator, ok := catalog.byContract[id]
	if id == "" || !ok || nilContributionValidator(validator) || validator.ContractID() != id {
		return nil, false
	}
	return validator, true
}

// ValidateContributionSet checks complete set structure, subject correlation
// and exact value admission; document budgets and operations are not consulted.
func (catalog ContributionCatalog) ValidateContributionSet(set ContributionSet) error {
	return validateContributionSet(set, catalog.Lookup)
}

func validateContributionSet(set ContributionSet, lookup func(CodecContractID) (ContributionValidator, bool)) error {
	canonical, err := NewContributionSet(set.Contributions())
	if err != nil {
		return err
	}
	for index, item := range canonical.Contributions() {
		if err := ValidateSubjectContract(item.SubjectID(), item.Contribution().Contract()); err != nil {
			return fmt.Errorf("aggregate contribution set[%d]: %w", index, err)
		}
	}
	validator, ok := lookup(canonical.Contract().CodecContractID())
	if !ok {
		return fmt.Errorf("aggregate codec contract %q is not admitted", canonical.Contract().CodecContractID())
	}
	return validator.ValidateContributions(canonical)
}

func nilContributionValidator(validator ContributionValidator) bool {
	if validator == nil {
		return true
	}
	value := reflect.ValueOf(validator)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}
