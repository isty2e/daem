package mcpcodec

import (
	"encoding/json"
	"strings"
	"testing"

	desiredmcp "github.com/isty2e/daem/internal/desired/mcp"
	desiredtest "github.com/isty2e/daem/internal/desired/testfixture"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/target"
)

func TestClaudeProjectContributionKeepsEnvironmentReferencesSymbolic(t *testing.T) {
	const resolvedSecret = "must-not-enter-canonical-projection"
	t.Setenv("HOST_CONTEXT7_TOKEN", resolvedSecret)

	env := map[string]desiredmcp.EnvReference{
		"CONTEXT7_TOKEN": desiredtest.MCPEnvReference(t, "HOST_CONTEXT7_TOKEN"),
	}
	transport := desiredtest.MCPStdio(
		t,
		desiredtest.MCPCommand(t, "npx"),
		[]string{"-y", "@upstash/context7-mcp"},
		env,
	)
	binding := desiredtest.MCPBinding(
		t,
		target.TargetClaudeCode,
		target.ScopeProject,
		transport,
		desiredmcp.OnAbsentRemoveBinding,
	)
	server, err := desiredmcp.New(desiredmcp.Spec{
		Name:     "context7",
		Bindings: []desiredmcp.Binding{binding},
	})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	placement, ok := aggregate.ImplementedMCPPlacement(target.TargetClaudeCode, target.ScopeProject)
	if !ok {
		t.Fatal("Claude project placement missing")
	}

	canonical, err := CanonicalMCPBindingContribution(server, binding, placement)
	if err != nil {
		t.Fatalf("CanonicalMCPBindingContribution returned error: %v", err)
	}
	var entry struct {
		Env map[string]string `json:"env"`
	}
	if err := json.Unmarshal(canonical, &entry); err != nil {
		t.Fatalf("decode canonical contribution: %v", err)
	}
	if got := entry.Env["CONTEXT7_TOKEN"]; got != "${HOST_CONTEXT7_TOKEN}" {
		t.Fatalf("canonical env reference = %q, want symbolic host source", got)
	}
	if strings.Contains(string(canonical), resolvedSecret) {
		t.Fatalf("canonical contribution persisted resolved environment value: %s", canonical)
	}
}

func TestCanonicalMCPBindingContributionRejectsPlacementCodecEnvironmentContractDrift(t *testing.T) {
	transport := desiredtest.MCPStdio(
		t,
		desiredtest.MCPCommand(t, "npx"),
		nil,
		nil,
	)
	binding := desiredtest.MCPBinding(
		t,
		target.TargetClaudeCode,
		target.ScopeProject,
		transport,
		desiredmcp.OnAbsentRemoveBinding,
	)
	server, err := desiredmcp.New(desiredmcp.Spec{
		Name:     "context7",
		Bindings: []desiredmcp.Binding{binding},
	})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	mismatchedPlacement, err := aggregate.NewMCPPlacement(aggregate.MCPPlacementInput{
		ID:                     aggregate.MCPPlacementClaudeProject,
		Target:                 target.TargetClaudeCode,
		Scope:                  target.ScopeProject,
		ConfigLayer:            aggregate.MCPConfigLayerClaudeProjectFile,
		ConfigPath:             aggregate.ClaudeProjectMCPConfigPath,
		MergeUnit:              aggregate.MCPMergeUnitServerEntry,
		ContentPathPrefix:      "/mcpServers",
		SiblingRetention:       aggregate.MCPSiblingRetentionPreserveUnmanaged,
		CodecContractID:        aggregate.MCPCodecClaudeProjectStdio,
		ComparedFields:         []string{"command"},
		Absence:                aggregate.MCPAbsenceRemoveBinding,
		EnvReferenceMapping:    aggregate.MCPEnvMappingUnsupported,
		EnvReferenceResolution: aggregate.MCPEnvResolutionUnavailable,
	})
	if err != nil {
		t.Fatalf("NewMCPPlacement returned error: %v", err)
	}

	_, err = CanonicalMCPBindingContribution(server, binding, mismatchedPlacement)
	if err == nil || !strings.Contains(err.Error(), "does not match codec capability") {
		t.Fatalf("CanonicalMCPBindingContribution error = %v, want codec capability mismatch", err)
	}
}
