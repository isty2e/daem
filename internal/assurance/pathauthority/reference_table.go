package pathauthority

import (
	"fmt"
	"slices"
)

// ReferenceTable is complete recorded evidence for a supplied identity set,
// not a live filesystem capability or locator selection policy.
type ReferenceTable struct {
	values map[Exact]Reference
}

// EmptyReferenceTable is initialized complete evidence for no identities.
func EmptyReferenceTable() ReferenceTable {
	return ReferenceTable{values: make(map[Exact]Reference)}
}

// NewReferenceTable requires exact membership and merges identical references.
func NewReferenceTable(identities []Exact, references []Reference) (ReferenceTable, error) {
	required := make(map[Exact]struct{}, len(identities))
	for _, identity := range identities {
		if err := identity.Validate(); err != nil {
			return ReferenceTable{}, err
		}
		required[identity] = struct{}{}
	}
	values := make(map[Exact]Reference, len(references))
	for _, reference := range references {
		if err := reference.Validate(); err != nil {
			return ReferenceTable{}, err
		}
		identity := reference.Identity()
		if _, present := required[identity]; !present {
			return ReferenceTable{}, fmt.Errorf("path reference %q has no required identity", identity.Key())
		}
		if previous, present := values[identity]; present && !previous.Equal(reference) {
			return ReferenceTable{}, fmt.Errorf("path identity %q has conflicting recorded locators", identity.Key())
		}
		values[identity] = reference
	}
	for identity := range required {
		if _, present := values[identity]; !present {
			return ReferenceTable{}, fmt.Errorf("path identity %q requires a recorded locator", identity.Key())
		}
	}
	return ReferenceTable{values: values}, nil
}

func (table ReferenceTable) Validate() error {
	if table.values == nil {
		return fmt.Errorf("path reference table is uninitialized")
	}
	return nil
}

// Converge retains surviving evidence and admits additions through the complete
// table contract. It neither replaces recorded locators nor qualifies them.
func (table ReferenceTable) Converge(identities []Exact, additions []Reference) (ReferenceTable, error) {
	if err := table.Validate(); err != nil {
		return ReferenceTable{}, err
	}
	references := append([]Reference(nil), additions...)
	for _, identity := range identities {
		if reference, present := table.Reference(identity); present {
			references = append(references, reference)
		}
	}
	return NewReferenceTable(identities, references)
}

func (table ReferenceTable) Reference(identity Exact) (Reference, bool) {
	reference, present := table.values[identity]
	return reference, present
}

func (table ReferenceTable) References() []Reference {
	references := make([]Reference, 0, len(table.values))
	for _, reference := range table.values {
		references = append(references, reference)
	}
	slices.SortFunc(references, func(left, right Reference) int { return left.Identity().Compare(right.Identity()) })
	return references
}
