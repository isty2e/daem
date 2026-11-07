package configuration

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/isty2e/daem/internal/desired/hostconfig"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/target"
)

var (
	ErrReservedKey  = errors.New("native key overlaps a dedicated resource")
	ErrReservedPath = errors.New("native path overlaps a dedicated resource")
)

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

type DedicatedReservationsInput struct {
	Target     target.Target
	Scope      target.Scope
	MCP        []output.KeyPath
	Hooks      []output.KeyPath
	Extensions []output.KeyPath

	MCPDocuments  []output.Destination
	HookDocuments []output.Destination
	Resources     []output.Destination
}

// DedicatedReservations is a composition of owner-provided namespaces, not
// declaration state or proof of physical ownership.
type DedicatedReservations struct {
	target target.Target
	scope  target.Scope
	keys   [len(dedicatedSlots)][]output.KeyPath
	paths  []string
}

func NewDedicatedReservations(input DedicatedReservationsInput) (DedicatedReservations, error) {
	if _, err := target.ParseTarget(string(input.Target)); err != nil {
		return DedicatedReservations{}, err
	}
	if _, err := target.ParseScope(string(input.Scope)); err != nil {
		return DedicatedReservations{}, err
	}

	result := DedicatedReservations{target: input.Target, scope: input.Scope}
	for index, keys := range [][]output.KeyPath{input.MCP, input.Hooks, input.Extensions} {
		for _, key := range keys {
			if key.IsRoot() {
				return DedicatedReservations{}, fmt.Errorf("%s coverage cannot be the document root", dedicatedSlots[index].name)
			}
		}
		result.keys[index] = append([]output.KeyPath(nil), keys...)
	}

	layout, err := selectLayout(input.Target, input.Scope)
	if err != nil {
		return DedicatedReservations{}, err
	}
	for _, group := range []struct {
		slot    dedicatedSlotSet
		aliases []output.Destination
	}{
		{mcpEntries, input.MCPDocuments},
		{hookSets, input.HookDocuments},
		{0, input.Resources},
	} {
		paths, err := result.reservedPaths(layout, group.slot, group.aliases)
		if err != nil {
			return DedicatedReservations{}, err
		}
		result.paths = append(result.paths, paths...)
	}
	slices.Sort(result.paths)
	result.paths = slices.Compact(result.paths)

	return result, nil
}

func (facts DedicatedReservations) reservedPaths(layout nativeLayout, slot dedicatedSlotSet, aliases []output.Destination) ([]string, error) {
	result := make([]string, 0, len(aliases))
	for _, alias := range aliases {
		if err := alias.ValidateScope(facts.scope); err != nil {
			return nil, err
		}
		nativePath, inside := layout.reservationPathFor(alias)
		if !inside {
			continue
		}
		rule, err := documentRuleFor(facts.target, facts.scope, nativePath, false)
		if err != nil {
			return nil, err
		}
		if slot != 0 && rule.slots&slot != 0 {
			continue
		}
		result = append(result, nativePath)
	}
	return result, nil
}

func (facts DedicatedReservations) RequireUnreservedPath(nativePath string) error {
	if _, err := target.ParseTarget(string(facts.target)); err != nil {
		return err
	}
	if _, err := target.ParseScope(string(facts.scope)); err != nil {
		return err
	}
	if err := hostconfig.ValidateRelativePath(nativePath); err != nil {
		return err
	}
	for _, reserved := range facts.paths {
		if nativePath == reserved || strings.HasPrefix(nativePath, reserved+"/") || strings.HasPrefix(reserved, nativePath+"/") {
			return fmt.Errorf("%w: %q intersects %q; use its dedicated resource", ErrReservedPath, nativePath, reserved)
		}
	}
	return nil
}

// ValidateDocument checks coverage for a constructor-checked immutable document;
// it does not reconstruct the document's canonical shape.
func (facts DedicatedReservations) ValidateDocument(spec DocumentSpec) error {
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
func (facts DedicatedReservations) RequireUnreservedKey(spec DocumentSpec, key output.KeyPath) error {
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
