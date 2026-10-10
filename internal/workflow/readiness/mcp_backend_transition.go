package readiness

import (
	"fmt"

	desiredmcp "github.com/isty2e/daem/internal/desired/mcp"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/profile"
)

// MCPBackendTransitionError refuses reinterpretation of an owned Pi entry through a different codec.
type MCPBackendTransitionError struct {
	address aggregate.ProjectionAddress
	stored  desiredmcp.Backend
	desired desiredmcp.Backend
}

func (failure *MCPBackendTransitionError) Error() string {
	return fmt.Sprintf("Pi MCP backend transition is unsupported: server=%.256q target=%s scope=%s stored=%s desired=%s; retire the existing binding with apply, clear any unused Adapter provider intent, then re-add it; no automatic migration was performed",
		failure.address.ContentPath(), failure.address.Document().Target(), failure.address.Document().Scope(), failure.stored, failure.desired)
}

func piMCPBackendTransition(previous, desired aggregate.ProjectionContract) error {
	stored, oldPi := profile.PiMCPContractForCodec(previous.CodecContractID())
	next, newPi := profile.PiMCPContractForCodec(desired.CodecContractID())
	if !oldPi || !newPi || stored.Backend() == next.Backend() {
		return nil
	}
	return &MCPBackendTransitionError{address: desired.Address(), stored: stored.Backend(), desired: next.Backend()}
}
