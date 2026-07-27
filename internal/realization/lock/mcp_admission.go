package lock

import (
	"fmt"

	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/profile"
	topologymcp "github.com/isty2e/daem/internal/topology/mcp"
)

// validateAdmittedMCPProjection replays the static MCP refinement facts that
// are available without Desired input or current host evidence.
func validateAdmittedMCPProjection(
	contract LockedSubjectContract,
) (bool, error) {
	placement, ok := aggregate.MCPPlacementForSubject(contract.SubjectID())
	if !ok {
		return false, nil
	}
	serverID, entityBacked := topologymcp.ServerID(contract.SubjectID())
	if !entityBacked || contract.EntityID().Kind() != entity.KindMCPServer || contract.EntityID().Name() != serverID {
		return true, fmt.Errorf(
			"MCP projection entity %q does not match subject %q",
			contract.EntityID(),
			contract.SubjectID(),
		)
	}
	realization, realized := contract.Realization()
	if !realized {
		return true, fmt.Errorf("MCP projection requires managed aggregate realization")
	}
	profilePlacement, ok := profile.Profile(placement.Target()).MCPPlacement(placement.ID())
	if !ok {
		return true, fmt.Errorf("MCP placement %q is not admitted by target profile %q", placement.ID(), placement.Target())
	}
	contribution, ok := realization.ManagedAggregateContribution()
	if !ok {
		return true, fmt.Errorf("MCP projection requires managed aggregate realization")
	}
	expectedRealization, err := mcpAggregateRealization(
		profilePlacement,
		contract.EntityID().Name(),
		contribution.CanonicalContribution(),
	)
	if err != nil {
		return true, err
	}
	if !realization.Equal(expectedRealization) {
		return true, fmt.Errorf("MCP realization does not match the admitted placement profile")
	}
	spec, ok := mcpProjectionLockSpecFor(profilePlacement.ID())
	if !ok {
		return true, fmt.Errorf("MCP placement %q has no admitted lock refinement", profilePlacement.ID())
	}
	replay, err := spec.replayCoverage()
	if err != nil {
		return true, err
	}
	operationContracts, err := spec.operationContracts(profilePlacement)
	if err != nil {
		return true, err
	}
	input := LockedSubjectContractInput{
		EntityID: contract.EntityID(), SubjectID: contract.SubjectID(), Realization: &expectedRealization,
		Ownership: OwnershipManifest, OnAbsent: OnAbsentRemoveBinding, Replay: replay,
		OperationContracts: operationContracts,
	}
	if delegatePlan, present := contract.DelegatePlan(); present {
		input.DelegatePlan = &delegatePlan
	}
	expected, err := NewLockedSubjectContract(input)
	if err != nil {
		return true, err
	}
	if !contract.Equal(expected) {
		return true, fmt.Errorf("MCP subject does not match the admitted lock refinement")
	}
	return true, nil
}
