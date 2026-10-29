package ownership

import (
	"fmt"
	"sort"
)

// DocumentBindingConvergence settles exact-ready intents, without a prepared
// registry phase or physical-path exclusion between distinct binding keys.
type DocumentBindingConvergence struct {
	intents     []DocumentBindingIntent
	initialized bool
}

func NewDocumentBindingConvergence(intents []DocumentBindingIntent) (DocumentBindingConvergence, error) {
	canonical := append([]DocumentBindingIntent(nil), intents...)
	for index, intent := range canonical {
		if err := intent.Validate(); err != nil {
			return DocumentBindingConvergence{}, fmt.Errorf("document binding convergence intent[%d]: %w", index, err)
		}
		if !intent.provisional.IsZero() {
			return DocumentBindingConvergence{}, fmt.Errorf("document binding convergence requires exact-ready targets")
		}
	}
	sort.Slice(canonical, func(left, right int) bool { return canonical[left].less(canonical[right]) })
	for index := 1; index < len(canonical); index++ {
		if canonical[index-1].key() == canonical[index].key() {
			return DocumentBindingConvergence{}, fmt.Errorf("document binding convergence repeats an owner/destination key")
		}
	}
	return DocumentBindingConvergence{intents: canonical, initialized: true}, nil
}

func (convergence DocumentBindingConvergence) Validate() error {
	if !convergence.initialized {
		return fmt.Errorf("document binding convergence is required")
	}
	return nil
}

func (convergence DocumentBindingConvergence) Intents() []DocumentBindingIntent {
	return append([]DocumentBindingIntent(nil), convergence.intents...)
}

// Apply accepts each exact before or target and returns no partial successor
// for a stale row. Unrelated bindings and all exclusive claims are retained.
func (convergence DocumentBindingConvergence) Apply(registry Registry) (Registry, bool, error) {
	if err := convergence.Validate(); err != nil {
		return Registry{}, false, err
	}
	if !registry.initialized {
		return Registry{}, false, fmt.Errorf("ownership registry is required")
	}

	replacements := make(map[documentBindingKey]DocumentBindingValue)
	for _, intent := range convergence.intents {
		actual := NoDocumentBinding()
		if binding, present := registry.DocumentBinding(intent.owner.Key(), intent.destination); present {
			actual, _ = PresentDocumentBinding(binding)
		}
		if actual.Equal(intent.after) {
			continue
		}
		if err := registry.requireBindingWritable(intent.before, intent.after); err != nil {
			return Registry{}, false, err
		}
		if !actual.Equal(intent.before) {
			return Registry{}, false, fmt.Errorf("document binding differs from both before and target records")
		}
		replacements[intent.key()] = intent.after
	}
	if len(replacements) == 0 {
		return registry, false, nil
	}

	bindings := make([]DocumentBinding, 0, len(registry.bindings)+len(replacements))
	for _, binding := range registry.bindings {
		if _, replaced := replacements[binding.key()]; !replaced {
			bindings = append(bindings, binding)
		}
	}
	for _, intent := range convergence.intents {
		if value, replaced := replacements[intent.key()]; replaced {
			if binding, present := value.Get(); present {
				bindings = append(bindings, binding)
			}
		}
	}
	next, err := registry.withFacts(registry.claims, bindings)
	if err != nil {
		return Registry{}, false, err
	}
	return next, true, nil
}
