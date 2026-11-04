package recovery

import (
	"fmt"

	"github.com/isty2e/daem/internal/realization/aggregate"
)

// PartialDocumentState keeps regular-root metadata separate from selected codec
// facts. It is neither namespace authority nor managed-subject membership.
type PartialDocumentState struct {
	exists      bool
	mode        PermissionMode
	projections map[aggregate.ProjectionAddress]aggregate.ProjectionState
	initialized bool
}

func NewPartialDocumentState(exists bool, mode *PermissionMode, snapshots []aggregate.Snapshot) (PartialDocumentState, error) {
	if exists != (mode != nil) {
		return PartialDocumentState{}, fmt.Errorf("partial document presence requires exactly its regular-file mode")
	}
	if mode != nil && *mode&^PermissionMode(0o777) != 0 {
		return PartialDocumentState{}, fmt.Errorf("partial document mode must contain only permission bits")
	}
	state := PartialDocumentState{
		exists: exists, initialized: true,
		projections: make(map[aggregate.ProjectionAddress]aggregate.ProjectionState),
	}
	if mode != nil {
		state.mode = *mode
	}

	for index, snapshot := range snapshots {
		if _, err := snapshot.Selection(); err != nil {
			return PartialDocumentState{}, fmt.Errorf("partial document snapshot[%d]: %w", index, err)
		}
		if snapshot.DocumentExisted() != exists {
			return PartialDocumentState{}, fmt.Errorf("partial document snapshot[%d] has different root presence", index)
		}
		for _, projection := range snapshot.States() {
			address := projection.Contract().Address()
			if _, duplicate := state.projections[address]; duplicate {
				return PartialDocumentState{}, fmt.Errorf("partial document repeats a selected projection")
			}
			state.projections[address] = projection
		}
	}
	return state, nil
}

// PartialDocumentMatch is only the root-metadata and aggregate-projection
// component of recovery. Dedicated order and durable-authority joins remain separate.
type PartialDocumentMatch struct {
	before      bool
	expected    bool
	recoverable bool
}

func (match PartialDocumentMatch) MatchesBefore() bool   { return match.before }
func (match PartialDocumentMatch) MatchesExpected() bool { return match.expected }
func (match PartialDocumentMatch) Recoverable() bool     { return match.recoverable }

// MatchPartialDocumentStates does not pair selected phase with historical root
// presence. Third metadata or any third selected value blocks the whole match.
func MatchPartialDocumentStates(before, expected, current PartialDocumentState) (PartialDocumentMatch, error) {
	if !before.initialized || !expected.initialized || !current.initialized {
		return PartialDocumentMatch{}, fmt.Errorf("partial document states must be initialized")
	}
	if err := before.sameSelection(expected); err != nil {
		return PartialDocumentMatch{}, fmt.Errorf("partial document expected selection: %w", err)
	}
	if err := before.sameSelection(current); err != nil {
		return PartialDocumentMatch{}, fmt.Errorf("partial document current selection: %w", err)
	}
	modeChanged := before.exists && expected.exists && before.mode != expected.mode
	if modeChanged && !expected.hasMCPDocumentMode() {
		return PartialDocumentMatch{}, fmt.Errorf("partial document mode transition requires a selected fixed MCP policy")
	}
	if !current.SameRootMetadata(before) && !current.SameRootMetadata(expected) {
		return PartialDocumentMatch{}, nil
	}

	match := PartialDocumentMatch{before: true, expected: true, recoverable: true}
	if modeChanged {
		match.before = current.SameRootMetadata(before)
		match.expected = current.SameRootMetadata(expected)
	}
	for address, projection := range current.projections {
		original, candidate := before.projections[address], expected.projections[address]
		isBefore, isExpected := projection.Equal(original), projection.Equal(candidate)
		if _, native := aggregate.HostConfigSyntaxForCodec(projection.Contract().CodecContractID()); native {
			isBefore = projection.SelectedValueEqual(original)
			isExpected = projection.SelectedValueEqual(candidate)
		}
		match.before = match.before && isBefore
		match.expected = match.expected && isExpected
		match.recoverable = match.recoverable && (isBefore || isExpected)
	}
	return match, nil
}

func (state PartialDocumentState) hasMCPDocumentMode() bool {
	if !state.exists || state.mode != PermissionMode(aggregate.DocumentFileMode) {
		return false
	}
	for _, projection := range state.projections {
		contract := projection.Contract()
		placement, found := aggregate.MCPPlacementForCodec(aggregate.MCPPlacementID(contract.Address().PlacementID()), contract.CodecContractID())
		address := contract.Address().Document()
		if found && placement.Target() == address.Target() && placement.Scope() == address.Scope() && placement.ConfigPath() == address.AggregateRoot() {
			return true
		}
	}
	return false
}

func (state PartialDocumentState) sameSelection(other PartialDocumentState) error {
	if len(state.projections) != len(other.projections) {
		return fmt.Errorf("selected projection coverage differs")
	}
	for address, projection := range state.projections {
		candidate, present := other.projections[address]
		if !present || !projection.Contract().Equal(candidate.Contract()) {
			return fmt.Errorf("selected projection contract differs")
		}
	}
	return nil
}

// SameRootMetadata compares presence and exact regular-file mode, not selected
// values or filesystem identity.
func (state PartialDocumentState) SameRootMetadata(other PartialDocumentState) bool {
	return state.initialized && other.initialized && state.exists == other.exists && (!state.exists || state.mode == other.mode)
}

// RegularFileMode distinguishes exact0000 from absent or uninitialized evidence.
func (state PartialDocumentState) RegularFileMode() (PermissionMode, bool) {
	return state.mode, state.initialized && state.exists
}
