package configuration

import (
	"errors"
	"fmt"

	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/target"
)

var ErrReservedKey = errors.New("native key overlaps a dedicated resource")

type dedicatedSlotSet uint8

const (
	mcpEntries dedicatedSlotSet = 1 << iota
	hookSets
	extensionMembership
)

var dedicatedSlots = [...]struct {
	flag dedicatedSlotSet
	name string
}{
	{mcpEntries, "MCP entries"},
	{hookSets, "hook sets"},
	{extensionMembership, "extension membership"},
}

type DedicatedKeysInput struct {
	Target     target.Target
	Scope      target.Scope
	MCP        []output.KeyPath
	Hooks      []output.KeyPath
	Extensions []output.KeyPath
}

// DedicatedKeys is a composition of owner-provided namespaces, not
// declaration state or proof of physical ownership.
type DedicatedKeys struct {
	target target.Target
	scope  target.Scope
	keys   [len(dedicatedSlots)][]output.KeyPath
}

func NewDedicatedKeys(input DedicatedKeysInput) (DedicatedKeys, error) {
	if _, err := target.ParseTarget(string(input.Target)); err != nil {
		return DedicatedKeys{}, err
	}
	if _, err := target.ParseScope(string(input.Scope)); err != nil {
		return DedicatedKeys{}, err
	}

	result := DedicatedKeys{target: input.Target, scope: input.Scope}
	for index, keys := range [][]output.KeyPath{input.MCP, input.Hooks, input.Extensions} {
		for _, key := range keys {
			if key.IsRoot() {
				return DedicatedKeys{}, fmt.Errorf("%s coverage cannot be the document root", dedicatedSlots[index].name)
			}
		}
		result.keys[index] = append([]output.KeyPath(nil), keys...)
	}

	return result, nil
}

// ValidateDocument checks coverage for a constructor-checked immutable document;
// it does not reconstruct the document's canonical shape.
func (facts DedicatedKeys) ValidateDocument(spec DocumentSpec) error {
	if err := spec.root.Validate(); err != nil {
		return err
	}

	if facts.target != spec.Target() || facts.scope != spec.Scope() {
		return fmt.Errorf("dedicated key coverage does not match the selected document target/scope")
	}

	for index, slot := range dedicatedSlots {
		if spec.slots&slot.flag != 0 && len(facts.keys[index]) == 0 {
			return fmt.Errorf("%s coverage is required for %q", slot.name, spec.Destination().String())
		}
	}
	return nil
}

// RequireUnreservedKey checks only static dedicated namespaces. Referent,
// installed-claim, path and invocation admission remain independent obligations.
func (facts DedicatedKeys) RequireUnreservedKey(spec DocumentSpec, key output.KeyPath) error {
	if err := facts.ValidateDocument(spec); err != nil {
		return err
	}
	if key.IsRoot() {
		return fmt.Errorf("key ownership requires a non-root native key")
	}
	for index, slot := range dedicatedSlots {
		if spec.slots&slot.flag == 0 {
			continue
		}
		for _, reserved := range facts.keys[index] {
			if reserved.Overlaps(key) {
				return fmt.Errorf("%w: %q belongs to %s at %q", ErrReservedKey, key.String(), slot.name, reserved.String())
			}
		}
	}
	return nil
}
