package mcpcodec

import (
	"bytes"
	"fmt"

	"github.com/isty2e/daem/internal/realization/aggregate"
)

// MCPProjectionObservation is an immutable, projection-only view of selected
// entries in one host aggregate. It intentionally retains no unmanaged bytes.
type MCPProjectionObservation struct {
	parentPresent bool
	selected      map[string]struct{}
	canonical     map[string][]byte
}

func newMCPProjectionObservation(
	parentPresent bool,
	serverIDs []string,
	canonical map[string][]byte,
) (MCPProjectionObservation, error) {
	selected := make(map[string]struct{}, len(serverIDs))
	for _, serverID := range serverIDs {
		if err := validateServerID(serverID); err != nil {
			return MCPProjectionObservation{}, err
		}
		if _, duplicate := selected[serverID]; duplicate {
			return MCPProjectionObservation{}, fmt.Errorf("MCP projection observation repeats server id %q", serverID)
		}
		selected[serverID] = struct{}{}
	}
	if len(selected) == 0 {
		return MCPProjectionObservation{}, fmt.Errorf("MCP projection observation selection is empty")
	}

	owned := make(map[string][]byte, len(canonical))
	for serverID, content := range canonical {
		if _, ok := selected[serverID]; !ok {
			return MCPProjectionObservation{}, fmt.Errorf("MCP projection observation contains unselected server id %q", serverID)
		}
		if len(bytes.TrimSpace(content)) == 0 {
			return MCPProjectionObservation{}, fmt.Errorf("MCP projection observation %q has empty canonical bytes", serverID)
		}
		owned[serverID] = bytes.Clone(content)
	}
	return MCPProjectionObservation{
		parentPresent: parentPresent,
		selected:      selected,
		canonical:     owned,
	}, nil
}

// ParentPresent reports whether the managed aggregate parent existed.
func (observation MCPProjectionObservation) ParentPresent() bool {
	return observation.parentPresent
}

// CanonicalEntry returns an owned copy of one selected canonical entry.
func (observation MCPProjectionObservation) CanonicalEntry(serverID string) ([]byte, bool, error) {
	if _, ok := observation.selected[serverID]; !ok {
		return nil, false, fmt.Errorf("MCP projection observation did not select server id %q", serverID)
	}
	content, present := observation.canonical[serverID]
	return bytes.Clone(content), present, nil
}

// MCPProjectionCanonicalComparison reports canonical-byte equivalence for one placement entry.
type MCPProjectionCanonicalComparison struct {
	ContentPath string
	Present     bool
	Equivalent  bool
}

// MCPPlacementOperations binds one implemented placement to pure aggregate operations.
type MCPPlacementOperations struct {
	placement             aggregate.MCPPlacement
	foldMutations         func([]byte, []MCPProjectionMutation) ([]byte, error)
	restoreMutations      func([]byte, []MCPProjectionMutation, bool) ([]byte, bool, error)
	verifyMutations       func([]byte, []MCPProjectionMutation) error
	observeCanonical      func([]byte, []string) (MCPProjectionObservation, error)
	mergeCanonicalEntry   func([]byte, string, []byte) ([]byte, error)
	removeProjection      func([]byte, string) ([]byte, error)
	restoreRemove         func([]byte, string, bool) ([]byte, bool, error)
	extractCanonicalEntry func([]byte, string) ([]byte, bool, error)
	compareCanonicalEntry func([]byte, string, []byte) (MCPProjectionCanonicalComparison, error)
	entryPresent          func([]byte, string) (bool, error)
	parentPresent         func([]byte) (bool, error)
}

type mcpPlacementOperationsInput struct {
	placement             aggregate.MCPPlacement
	foldMutations         func([]byte, []MCPProjectionMutation) ([]byte, error)
	restoreMutations      func([]byte, []MCPProjectionMutation, bool) ([]byte, bool, error)
	verifyMutations       func([]byte, []MCPProjectionMutation) error
	observeCanonical      func([]byte, []string) (MCPProjectionObservation, error)
	mergeCanonicalEntry   func([]byte, string, []byte) ([]byte, error)
	removeProjection      func([]byte, string) ([]byte, error)
	restoreRemove         func([]byte, string, bool) ([]byte, bool, error)
	extractCanonicalEntry func([]byte, string) ([]byte, bool, error)
	compareCanonicalEntry func([]byte, string, []byte) (MCPProjectionCanonicalComparison, error)
	entryPresent          func([]byte, string) (bool, error)
	parentPresent         func([]byte) (bool, error)
}

var implementedMCPPlacementOperationCatalog []MCPPlacementOperations

func newMCPPlacementOperations(input mcpPlacementOperationsInput) (MCPPlacementOperations, error) {
	operations := MCPPlacementOperations{
		placement:             input.placement,
		foldMutations:         input.foldMutations,
		restoreMutations:      input.restoreMutations,
		verifyMutations:       input.verifyMutations,
		observeCanonical:      input.observeCanonical,
		mergeCanonicalEntry:   input.mergeCanonicalEntry,
		removeProjection:      input.removeProjection,
		restoreRemove:         input.restoreRemove,
		extractCanonicalEntry: input.extractCanonicalEntry,
		compareCanonicalEntry: input.compareCanonicalEntry,
		entryPresent:          input.entryPresent,
		parentPresent:         input.parentPresent,
	}
	if err := operations.validate(); err != nil {
		return MCPPlacementOperations{}, err
	}
	return operations, nil
}

func (operations MCPPlacementOperations) validate() error {
	if err := operations.placement.Validate(); err != nil {
		return fmt.Errorf("MCP placement operations placement: %w", err)
	}
	if operations.foldMutations == nil {
		return fmt.Errorf("MCP placement operations %q fold mutations operation is required", operations.placement.ID())
	}
	if operations.restoreMutations == nil {
		return fmt.Errorf("MCP placement operations %q restore mutations operation is required", operations.placement.ID())
	}
	if operations.verifyMutations == nil {
		return fmt.Errorf("MCP placement operations %q verify mutations operation is required", operations.placement.ID())
	}
	if operations.observeCanonical == nil {
		return fmt.Errorf("MCP placement operations %q canonical observation operation is required", operations.placement.ID())
	}
	if operations.mergeCanonicalEntry == nil {
		return fmt.Errorf("MCP placement operations %q merge canonical entry operation is required", operations.placement.ID())
	}
	if operations.removeProjection == nil {
		return fmt.Errorf("MCP placement operations %q remove projection operation is required", operations.placement.ID())
	}
	if operations.restoreRemove == nil {
		return fmt.Errorf("MCP placement operations %q restore-remove operation is required", operations.placement.ID())
	}
	if operations.extractCanonicalEntry == nil {
		return fmt.Errorf("MCP placement operations %q extract canonical entry operation is required", operations.placement.ID())
	}
	if operations.compareCanonicalEntry == nil {
		return fmt.Errorf("MCP placement operations %q compare canonical entry operation is required", operations.placement.ID())
	}
	if operations.entryPresent == nil {
		return fmt.Errorf("MCP placement operations %q entry-present operation is required", operations.placement.ID())
	}
	if operations.parentPresent == nil {
		return fmt.Errorf("MCP placement operations %q parent-present operation is required", operations.placement.ID())
	}
	return nil
}

// Placement returns the canonical placement row that owns these operations.
func (operations MCPPlacementOperations) Placement() aggregate.MCPPlacement {
	return operations.placement
}

// ContentPath returns the managed entry path for serverID inside this placement row.
func (operations MCPPlacementOperations) ContentPath(serverID string) (aggregate.ContentPath, error) {
	return operations.placement.ContentPath(serverID)
}

// ServerIDFromContentPath returns the stable server id represented by contentPath.
func (operations MCPPlacementOperations) ServerIDFromContentPath(contentPath aggregate.ContentPath) (string, bool) {
	return operations.placement.ServerIDFromContentPath(contentPath)
}

// FoldMutations applies a non-empty projection batch by parsing and encoding
// the host aggregate once.
func (operations MCPPlacementOperations) FoldMutations(existing []byte, mutations []MCPProjectionMutation) ([]byte, error) {
	if err := validateMCPProjectionMutations(mutations); err != nil {
		return nil, err
	}
	return operations.foldMutations(existing, mutations)
}

// RestoreMutations restores a non-empty selected projection batch while
// preserving current unmanaged siblings and the baseline parent-presence fact.
func (operations MCPPlacementOperations) RestoreMutations(
	existing []byte,
	mutations []MCPProjectionMutation,
	parentExistedBefore bool,
) ([]byte, bool, error) {
	if err := validateMCPProjectionMutations(mutations); err != nil {
		return nil, false, err
	}
	return operations.restoreMutations(existing, mutations, parentExistedBefore)
}

// VerifyMutations verifies a non-empty projection batch by parsing the final
// host aggregate once.
func (operations MCPPlacementOperations) VerifyMutations(content []byte, mutations []MCPProjectionMutation) error {
	if err := validateMCPProjectionMutations(mutations); err != nil {
		return err
	}
	return operations.verifyMutations(content, mutations)
}

// ObserveCanonicalEntries parses one aggregate once and returns only the
// selected canonical projections plus their parent-shape fact.
func (operations MCPPlacementOperations) ObserveCanonicalEntries(
	existing []byte,
	serverIDs []string,
) (MCPProjectionObservation, error) {
	return operations.observeCanonical(existing, append([]string(nil), serverIDs...))
}

// MergeCanonicalEntry returns a pure aggregate with canonical entry bytes upserted for serverID.
func (operations MCPPlacementOperations) MergeCanonicalEntry(existing []byte, serverID string, canonical []byte) ([]byte, error) {
	return operations.mergeCanonicalEntry(existing, serverID, canonical)
}

// RemoveProjection returns a pure aggregate with the managed server entry removed.
func (operations MCPPlacementOperations) RemoveProjection(existing []byte, serverID string) ([]byte, error) {
	return operations.removeProjection(existing, serverID)
}

// RestoreRemoveProjection returns rollback content for a remove-projection action.
func (operations MCPPlacementOperations) RestoreRemoveProjection(existing []byte, serverID string, parentExistedBefore bool) ([]byte, bool, error) {
	return operations.restoreRemove(existing, serverID, parentExistedBefore)
}

// ExtractCanonicalEntry returns canonical entry bytes for one managed server entry.
func (operations MCPPlacementOperations) ExtractCanonicalEntry(existing []byte, serverID string) ([]byte, bool, error) {
	return operations.extractCanonicalEntry(existing, serverID)
}

// CompareCanonicalEntry compares one managed server against locked canonical entry bytes.
func (operations MCPPlacementOperations) CompareCanonicalEntry(existing []byte, serverID string, canonical []byte) (MCPProjectionCanonicalComparison, error) {
	return operations.compareCanonicalEntry(existing, serverID, canonical)
}

// EntryPresent reports whether the aggregate contains a server key, even if the entry is unsupported.
func (operations MCPPlacementOperations) EntryPresent(existing []byte, serverID string) (bool, error) {
	return operations.entryPresent(existing, serverID)
}

// ParentPresent reports whether the aggregate contains the managed parent object/table.
func (operations MCPPlacementOperations) ParentPresent(existing []byte) (bool, error) {
	return operations.parentPresent(existing)
}

// ImplementedMCPPlacementOperationsForCodecContract returns the operation row for codecContractID.
func ImplementedMCPPlacementOperationsForCodecContract(codecContractID aggregate.CodecContractID) (MCPPlacementOperations, bool) {
	for _, operations := range implementedMCPPlacementOperationCatalog {
		if operations.placement.CodecContractID() == codecContractID {
			return operations, true
		}
	}
	return MCPPlacementOperations{}, false
}

func buildMCPPlacementOperationRows() []MCPPlacementOperations {
	return []MCPPlacementOperations{
		mustMCPPlacementOperations(aggregate.MCPPlacementClaudeProject, mcpPlacementOperationsInput{
			foldMutations:         foldClaudeProjectMCPProjectionMutations,
			restoreMutations:      restoreClaudeProjectMCPProjectionMutations,
			verifyMutations:       verifyClaudeProjectMCPProjectionMutations,
			observeCanonical:      observeClaudeProjectMCPProjections,
			mergeCanonicalEntry:   mergeClaudeProjectMCPServerCanonicalEntry,
			removeProjection:      removeClaudeProjectMCPServerProjection,
			restoreRemove:         restoreRemoveClaudeProjectMCPServerProjection,
			extractCanonicalEntry: extractClaudeProjectMCPServerProjectionBytes,
			compareCanonicalEntry: compareClaudeProjectMCPServerCanonicalEntry,
			entryPresent:          claudeProjectMCPServerEntryPresent,
			parentPresent:         claudeProjectMCPServersParentPresent,
		}),
		mustMCPPlacementOperations(aggregate.MCPPlacementClaudeGlobal, mcpPlacementOperationsInput{
			foldMutations:         foldClaudeGlobalMCPProjectionMutations,
			restoreMutations:      restoreClaudeGlobalMCPProjectionMutations,
			verifyMutations:       verifyClaudeGlobalMCPProjectionMutations,
			observeCanonical:      observeClaudeGlobalMCPProjections,
			mergeCanonicalEntry:   mergeClaudeGlobalMCPServerCanonicalEntry,
			removeProjection:      removeClaudeGlobalMCPServerProjection,
			restoreRemove:         restoreRemoveClaudeGlobalMCPServerProjection,
			extractCanonicalEntry: extractClaudeGlobalMCPServerProjectionBytes,
			compareCanonicalEntry: compareClaudeGlobalMCPServerCanonicalEntry,
			entryPresent:          claudeGlobalMCPServerEntryPresent,
			parentPresent:         claudeGlobalMCPServersParentPresent,
		}),
		mustMCPPlacementOperations(aggregate.MCPPlacementAntigravityGlobal, mcpPlacementOperationsInput{
			foldMutations:         foldAntigravityGlobalMCPProjectionMutations,
			restoreMutations:      restoreAntigravityGlobalMCPProjectionMutations,
			verifyMutations:       verifyAntigravityGlobalMCPProjectionMutations,
			observeCanonical:      observeAntigravityGlobalMCPProjections,
			mergeCanonicalEntry:   mergeAntigravityGlobalMCPServerCanonicalEntry,
			removeProjection:      removeAntigravityGlobalMCPServerProjection,
			restoreRemove:         restoreRemoveAntigravityGlobalMCPServerProjection,
			extractCanonicalEntry: extractAntigravityGlobalMCPServerProjectionBytes,
			compareCanonicalEntry: compareAntigravityGlobalMCPServerCanonicalEntry,
			entryPresent:          antigravityGlobalMCPServerEntryPresent,
			parentPresent:         antigravityGlobalMCPServersParentPresent,
		}),
		mustMCPPlacementOperations(aggregate.MCPPlacementOpenCodeProject, mcpPlacementOperationsInput{
			foldMutations:         foldOpenCodeProjectMCPProjectionMutations,
			restoreMutations:      restoreOpenCodeProjectMCPProjectionMutations,
			verifyMutations:       verifyOpenCodeProjectMCPProjectionMutations,
			observeCanonical:      observeOpenCodeProjectMCPProjections,
			mergeCanonicalEntry:   mergeOpenCodeProjectMCPServerCanonicalEntry,
			removeProjection:      removeOpenCodeProjectMCPServerProjection,
			restoreRemove:         restoreRemoveOpenCodeProjectMCPServerProjection,
			extractCanonicalEntry: extractOpenCodeProjectMCPServerProjectionBytes,
			compareCanonicalEntry: compareOpenCodeProjectMCPServerCanonicalEntry,
			entryPresent:          openCodeProjectMCPServerEntryPresent,
			parentPresent:         openCodeProjectMCPServersParentPresent,
		}),
		mustMCPPlacementOperations(aggregate.MCPPlacementOpenCodeGlobal, mcpPlacementOperationsInput{
			foldMutations:         foldOpenCodeGlobalMCPProjectionMutations,
			restoreMutations:      restoreOpenCodeGlobalMCPProjectionMutations,
			verifyMutations:       verifyOpenCodeGlobalMCPProjectionMutations,
			observeCanonical:      observeOpenCodeGlobalMCPProjections,
			mergeCanonicalEntry:   mergeOpenCodeGlobalMCPServerCanonicalEntry,
			removeProjection:      removeOpenCodeGlobalMCPServerProjection,
			restoreRemove:         restoreRemoveOpenCodeGlobalMCPServerProjection,
			extractCanonicalEntry: extractOpenCodeGlobalMCPServerProjectionBytes,
			compareCanonicalEntry: compareOpenCodeGlobalMCPServerCanonicalEntry,
			entryPresent:          openCodeGlobalMCPServerEntryPresent,
			parentPresent:         openCodeGlobalMCPServersParentPresent,
		}),
		mustMCPPlacementOperations(aggregate.MCPPlacementCodexProject, mcpPlacementOperationsInput{
			foldMutations:         foldCodexProjectMCPProjectionMutations,
			restoreMutations:      restoreCodexProjectMCPProjectionMutations,
			verifyMutations:       verifyCodexProjectMCPProjectionMutations,
			observeCanonical:      observeCodexProjectMCPProjections,
			mergeCanonicalEntry:   mergeCodexProjectMCPServerCanonicalEntry,
			removeProjection:      removeCodexProjectMCPServerProjection,
			restoreRemove:         restoreRemoveCodexProjectMCPServerProjection,
			extractCanonicalEntry: extractCodexProjectMCPServerProjectionBytes,
			compareCanonicalEntry: compareCodexProjectMCPServerCanonicalEntry,
			entryPresent:          codexProjectMCPServerEntryPresent,
			parentPresent:         codexProjectMCPServersParentPresent,
		}),
		mustMCPPlacementOperations(aggregate.MCPPlacementCodexGlobal, mcpPlacementOperationsInput{
			foldMutations:         foldCodexGlobalMCPProjectionMutations,
			restoreMutations:      restoreCodexGlobalMCPProjectionMutations,
			verifyMutations:       verifyCodexGlobalMCPProjectionMutations,
			observeCanonical:      observeCodexGlobalMCPProjections,
			mergeCanonicalEntry:   mergeCodexGlobalMCPServerCanonicalEntry,
			removeProjection:      removeCodexGlobalMCPServerProjection,
			restoreRemove:         restoreRemoveCodexGlobalMCPServerProjection,
			extractCanonicalEntry: extractCodexGlobalMCPServerProjectionBytes,
			compareCanonicalEntry: compareCodexGlobalMCPServerCanonicalEntry,
			entryPresent:          codexGlobalMCPServerEntryPresent,
			parentPresent:         codexGlobalMCPServersParentPresent,
		}),
	}
}

func mustMCPPlacementOperations(id aggregate.MCPPlacementID, input mcpPlacementOperationsInput) MCPPlacementOperations {
	placement, ok := aggregate.MCPPlacementForID(id)
	if !ok {
		panic(fmt.Sprintf("implemented MCP placement %q is missing", id))
	}
	input.placement = placement
	operations, err := newMCPPlacementOperations(input)
	if err != nil {
		panic(err)
	}
	return operations
}

func init() {
	operations := buildMCPPlacementOperationRows()
	if err := validateMCPPlacementOperationCatalog(operations, aggregate.ImplementedMCPPlacements()); err != nil {
		panic(err)
	}
	implementedMCPPlacementOperationCatalog = operations
}

func validateMCPPlacementOperationCatalog(operations []MCPPlacementOperations, placements []aggregate.MCPPlacement) error {
	placementIDs := make(map[aggregate.MCPPlacementID]aggregate.MCPPlacement, len(placements))
	for _, placement := range placements {
		placementIDs[placement.ID()] = placement
	}
	operationIDs := make(map[aggregate.MCPPlacementID]struct{}, len(operations))
	for _, operation := range operations {
		if err := operation.validate(); err != nil {
			return err
		}
		id := operation.Placement().ID()
		if _, ok := placementIDs[id]; !ok {
			return fmt.Errorf("MCP placement operations %q have no implemented placement row", id)
		}
		if _, ok := operationIDs[id]; ok {
			return fmt.Errorf("MCP placement operations share placement id %q", id)
		}
		operationIDs[id] = struct{}{}
	}
	for id := range placementIDs {
		if _, ok := operationIDs[id]; !ok {
			return fmt.Errorf("MCP placement %q has no operation row", id)
		}
	}
	return nil
}
