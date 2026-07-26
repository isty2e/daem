package probe

import (
	"context"
	"fmt"
	"time"

	"github.com/isty2e/daem/internal/assurance/runtimeprobe"
	runtimeprobemcp "github.com/isty2e/daem/internal/assurance/runtimeprobe/mcp"
	"github.com/isty2e/daem/internal/subprocess"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
)

// DefaultTimeout is the current explicit MCP runtime probe timeout.
const DefaultTimeout = runtimeprobemcp.DefaultTimeout

// Mode identifies whether the explicit probe command only discloses effects or executes them.
type Mode string

const (
	ModeDryRun  Mode = "dry-run"
	ModeExecute Mode = "execute"
)

// RuntimeProbeExecutor is the effectful boundary used by the workflow.
type RuntimeProbeExecutor interface {
	Probe(context.Context, runtimeprobemcp.ProbeRequest, subprocess.WorkingDirectoryBinder) ([]runtimeprobe.Fact, error)
}

// CommandInput describes one explicit MCP runtime probe command.
type CommandInput struct {
	ServerName   string
	ManifestPath string
	LockfilePath string
	TargetValue  string
	ScopeValue   string
	Mode         Mode
	Timeout      time.Duration
}

// CommandResult contains the canonical outcome of one explicit MCP runtime probe.
type CommandResult struct {
	ManifestPath     string
	LockfilePath     string
	LockfileExplicit bool
	ServerName       string
	Target           target.Target
	Scope            target.Scope
	Mode             Mode
	Timeout          time.Duration
	Subject          topology.SubjectID
	WorkingDirectory string
	ProbeRequest     runtimeprobemcp.ProbeRequest
	Runtime          runtimeprobe.Observation
	SideEffects      []string
}

// HasRuntimeErrors reports whether launch/initialize evidence found a failed admitted dimension.
func (result CommandResult) HasRuntimeErrors() bool {
	if result.Mode != ModeExecute {
		return false
	}
	return result.Runtime.Launcher().IsFailure() ||
		result.Runtime.ProtocolInitialize().IsFailure()
}

func validateMode(mode Mode) error {
	switch mode {
	case ModeDryRun, ModeExecute:
		return nil
	default:
		return fmt.Errorf("unsupported probe mode %q", mode)
	}
}
