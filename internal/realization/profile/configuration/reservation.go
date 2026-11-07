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

// ProtectionRequirements records which read-only root selectors a checked
// reservation facet needs. Its zero value means the requirements are unknown.
type ProtectionRequirements struct {
	known bool
	roots []output.RootReference
}

// NoProtectionRequirements is an explicit fact from a reservation facet, not an
// inference from missing selectors or an empty lookup result.
func NoProtectionRequirements() ProtectionRequirements {
	return ProtectionRequirements{known: true}
}

func RequireProtectionRoots(roots []output.RootReference) (ProtectionRequirements, error) {
	if len(roots) == 0 {
		return ProtectionRequirements{}, fmt.Errorf("required protection roots must not be empty")
	}
	selected := slices.Clone(roots)
	for _, reference := range selected {
		if err := reference.Validate(); err != nil {
			return ProtectionRequirements{}, fmt.Errorf("required protection root: %w", err)
		}
	}

	slices.SortFunc(selected, output.RootReference.Compare)
	return ProtectionRequirements{known: true, roots: slices.Compact(selected)}, nil
}

func (requirements ProtectionRequirements) References() ([]output.RootReference, error) {
	if !requirements.known {
		return nil, fmt.Errorf("physical reservation protection requirements are unknown")
	}
	return slices.Clone(requirements.roots), nil
}

func ExclusionProtectionRequirements(spec DocumentSpec) (ProtectionRequirements, error) {
	if err := spec.Validate(); err != nil {
		return ProtectionRequirements{}, err
	}
	facets, known := nativePathPolicies[spec.Target()]
	if !known {
		return ProtectionRequirements{}, fmt.Errorf("physical native exclusion facets for %s have not been audited", spec.Target())
	}
	layout, err := selectLayout(spec.Target(), target.ScopeGlobal)
	if err != nil {
		return ProtectionRequirements{}, err
	}
	return facets.requirementsForRoot(layout.root)
}

func protectionForFacets(facets nativePathFacets) (ProtectionRequirements, error) {
	if len(facets) == 0 {
		return ProtectionRequirements{}, fmt.Errorf("native exclusion facets are unknown")
	}
	references := make([]output.RootReference, 0, len(facets))
	for _, facet := range facets {
		reference, err := facet.root.reference()
		if err != nil {
			return ProtectionRequirements{}, fmt.Errorf("native exclusion facet root: %w", err)
		}
		if !facet.hasReservations() {
			continue
		}
		references = append(references, reference)
	}

	if len(references) == 0 {
		return NoProtectionRequirements(), nil
	}
	return RequireProtectionRoots(references)
}

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
		if nativePathsOverlap(nativePath, reserved) {
			return fmt.Errorf("%w: %q intersects %q; use its dedicated resource", ErrReservedPath, nativePath, reserved)
		}
	}
	return nil
}

func nativePathsOverlap(left, right string) bool {
	return left == right || strings.HasPrefix(left, right+"/") || strings.HasPrefix(right, left+"/")
}

func requireNativePath(selected target.Target, scope target.Scope, nativePath string) error {
	facets, known := nativePathPolicies[selected]
	if !known {
		return fmt.Errorf("host configuration target %q is unsupported until its native path policy is reviewed", selected)
	}
	if nativePathsOverlap(nativePath, ".git") {
		return fmt.Errorf("%w: %q belongs to repository metadata", ErrReserved, nativePath)
	}
	layout, err := selectLayout(selected, target.ScopeGlobal)
	if err != nil {
		return err
	}
	return requireNativeRootPath(facets, layout.root, scope, nativePath)
}

func (facets nativePathFacets) requireRoot(root rootFact) error {
	if _, err := root.reference(); err != nil {
		return err
	}
	for _, facet := range facets {
		if facet.root == root {
			return nil
		}
	}
	return fmt.Errorf("native exclusion facet for root %q (%s) has not been audited", root.identity, root.locator)
}

func (facets nativePathFacets) requirementsForRoot(root rootFact) (ProtectionRequirements, error) {
	if err := facets.requireRoot(root); err != nil {
		return ProtectionRequirements{}, err
	}
	return protectionForFacets(facets)
}

func requireNativeRootPath(facets nativePathFacets, root rootFact, scope target.Scope, nativePath string) error {
	if err := facets.requireRoot(root); err != nil {
		return err
	}
	for _, facet := range facets {
		if facet.root != root {
			continue
		}
		if err := facet.requirePath(scope, nativePath); err != nil {
			return err
		}
	}
	return nil
}

func (facet nativePathFacet) hasReservations() bool {
	return len(facet.paths) != 0 || facet.moduleDirectory != "" || facet.runtimeFiles
}

func (facet nativePathFacet) requirePath(scope target.Scope, nativePath string) error {
	for _, reservation := range facet.paths {
		if reservation.globalOnly && scope != target.ScopeGlobal {
			continue
		}
		if nativePathsOverlap(nativePath, reservation.path) {
			return fmt.Errorf("%w: %q belongs to %s", ErrReserved, nativePath, reservation.owner)
		}
	}
	if facet.moduleDirectory != "" && moduleEntrypointPath(nativePath, facet.moduleDirectory) {
		return fmt.Errorf("%w: %q belongs to extension loading", ErrReserved, nativePath)
	}
	if facet.runtimeFiles && scope == target.ScopeGlobal && versionedRuntimePath(nativePath) {
		return fmt.Errorf("%w: %q belongs to runtime state", ErrReserved, nativePath)
	}
	return nil
}

func moduleEntrypointPath(nativePath, directory string) bool {
	parts := strings.Split(nativePath, "/")
	if parts[0] != directory {
		return false
	}
	if len(parts) == 1 {
		return true
	}
	name := parts[1]
	if name == "index.ts" || name == "index.js" || name == "package.json" ||
		name == ".gitignore" || name == ".ignore" || name == ".fdignore" {
		return true
	}
	if len(parts) == 2 {
		return strings.HasSuffix(name, ".ts") || strings.HasSuffix(name, ".js")
	}
	return parts[2] == "index.ts" || parts[2] == "index.js" || parts[2] == "package.json"
}

func versionedRuntimePath(nativePath string) bool {
	parts := strings.Split(nativePath, "/")
	name := parts[0]
	if strings.HasSuffix(name, "-wal") {
		name = strings.TrimSuffix(name, "-wal")
	} else {
		name = strings.TrimSuffix(name, "-shm")
	}
	for _, prefix := range []string{"state_", "logs_", "goals_", "memories_", "thread_history_"} {
		if !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, ".sqlite") {
			continue
		}
		version := strings.TrimSuffix(strings.TrimPrefix(name, prefix), ".sqlite")
		if version != "" && strings.IndexFunc(version, func(r rune) bool { return r < '0' || r > '9' }) < 0 {
			return true
		}
	}
	if len(parts) < 2 || parts[0] != "app-server-daemon" {
		return false
	}
	for _, stem := range []string{"app-server", "app-server-updater"} {
		for _, suffix := range []string{".pid", ".pid.lock", ".pid.tmp", ".stderr.log"} {
			if parts[1] == stem+suffix {
				return true
			}
		}
	}
	return false
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
