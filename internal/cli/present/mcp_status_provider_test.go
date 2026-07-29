package clipresent

import (
	"testing"

	mcpobserve "github.com/isty2e/daem/internal/assurance/observe/mcp"
	"github.com/isty2e/daem/internal/assurance/runtimeprobe"
	"github.com/isty2e/daem/internal/target"
)

func TestMCPStatusEvidenceKeepsProviderVersionSeparateFromConfigAndRuntime(t *testing.T) {
	provider, err := mcpobserve.NewProviderPrerequisiteObservation(
		mcpobserve.ProviderPrerequisiteObservationInput{
			State:   mcpobserve.ProviderCurrent,
			Version: "2.15.0",
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	status, err := mcpStatusEvidenceWithProvider(
		target.ScopeProject,
		mcpobserve.AggregateProjectionObservation{},
		provider,
		mcpobserve.LastDelegateAttemptObservation{},
		runtimeprobe.Observation{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(status.Host) != 3 ||
		status.Host[2].Dimension != "provider_prerequisite" ||
		status.Host[2].State != "current" {
		t.Fatalf("Host = %#v, want separate current provider prerequisite", status.Host)
	}
	if status.Projection[0].State == "current" || status.Runtime[0].State == "current" {
		t.Fatalf(
			"provider readiness leaked into projection/runtime: projection=%#v runtime=%#v",
			status.Projection,
			status.Runtime,
		)
	}
}
