package ownership

import (
	"fmt"
	"sort"

	"github.com/isty2e/daem/internal/assurance/stateauthority"
	"github.com/isty2e/daem/internal/output"
)

func canonicalDocumentBindings(bindings []DocumentBinding) ([]DocumentBinding, map[documentBindingKey]int, error) {
	canonical := append([]DocumentBinding(nil), bindings...)
	for index, binding := range canonical {
		if err := binding.Validate(); err != nil {
			return nil, nil, fmt.Errorf("document binding[%d]: %w", index, err)
		}
	}
	sort.Slice(canonical, func(left, right int) bool { return canonical[left].less(canonical[right]) })
	indices := make(map[documentBindingKey]int, len(canonical))
	for index, binding := range canonical {
		key := binding.key()
		if _, exists := indices[key]; exists {
			return nil, nil, fmt.Errorf("document bindings repeat an owner/destination key")
		}
		indices[key] = index
	}
	return canonical, indices, nil
}

func (registry Registry) DocumentBindings() []DocumentBinding {
	return append([]DocumentBinding(nil), registry.bindings...)
}

// DocumentBinding looks up identity evidence by authoritative state key, not
// by the manifest path retained only for diagnostic provenance.
func (registry Registry) DocumentBinding(owner stateauthority.Key, destination output.Destination) (DocumentBinding, bool) {
	index, present := registry.bindingIndices[documentBindingKey{owner: owner, destination: destination}]
	if !present {
		return DocumentBinding{}, false
	}
	return registry.bindings[index], true
}

// ApplyDocumentBinding is a pure exact-before transition. The effect boundary
// must separately authorize, journal and publish any resulting metadata change.
func (registry Registry) ApplyDocumentBinding(owner stateauthority.Key, destination output.Destination, expected, replacement DocumentBindingValue) (Registry, error) {
	if !registry.initialized {
		return Registry{}, fmt.Errorf("ownership registry is required")
	}
	if err := owner.Validate(); err != nil {
		return Registry{}, err
	}
	if err := destination.Validate(); err != nil {
		return Registry{}, err
	}
	key := documentBindingKey{owner: owner, destination: destination}
	for _, value := range []DocumentBindingValue{expected, replacement} {
		if binding, present := value.Get(); present {
			if err := binding.Validate(); err != nil {
				return Registry{}, err
			}
			if binding.key() != key {
				return Registry{}, fmt.Errorf("document binding transition changes its identity key")
			}
		}
	}
	actual := NoDocumentBinding()
	if binding, present := registry.DocumentBinding(owner, destination); present {
		actual, _ = PresentDocumentBinding(binding)
	}
	if !actual.Equal(expected) {
		return Registry{}, fmt.Errorf("document binding changed since observation")
	}
	bindings := make([]DocumentBinding, 0, len(registry.bindings)+1)
	for _, binding := range registry.bindings {
		if binding.key() != key {
			bindings = append(bindings, binding)
		}
	}
	if binding, present := replacement.Get(); present {
		bindings = append(bindings, binding)
	}
	return NewRegistry(registry.claims, bindings...)
}
