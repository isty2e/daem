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

func TestCodexGlobalContributionLowersOnlySameNameEnvironmentReferences(t *testing.T) {
	const resolvedSecret = "must-not-enter-canonical-projection"
	t.Setenv("CONTEXT7_TOKEN", resolvedSecret)

	placement, ok := aggregate.ImplementedMCPPlacement(target.TargetCodex, target.ScopeGlobal)
	if !ok {
		t.Fatal("Codex global placement missing")
	}
	sameNameTransport := desiredtest.MCPStdio(
		t,
		desiredtest.MCPCommand(t, "npx"),
		nil,
		map[string]desiredmcp.EnvReference{
			"CONTEXT7_TOKEN": desiredtest.MCPEnvReference(t, "CONTEXT7_TOKEN"),
		},
	)
	sameNameBinding := desiredtest.MCPBinding(
		t,
		target.TargetCodex,
		target.ScopeGlobal,
		sameNameTransport,
		desiredmcp.OnAbsentRemoveBinding,
	)
	server := desiredtest.MCPServer(t, desiredmcp.Spec{
		Name:     "context7",
		Bindings: []desiredmcp.Binding{sameNameBinding},
	})

	canonical, err := CanonicalMCPBindingContribution(server, sameNameBinding, placement)
	if err != nil {
		t.Fatalf("CanonicalMCPBindingContribution returned error: %v", err)
	}
	if !strings.Contains(string(canonical), `env_vars = ["CONTEXT7_TOKEN"]`) {
		t.Fatalf("canonical contribution = %s, want same-name env_vars", canonical)
	}
	if strings.Contains(string(canonical), resolvedSecret) {
		t.Fatalf("canonical contribution persisted resolved environment value: %s", canonical)
	}

	aliasedTransport := desiredtest.MCPStdio(
		t,
		desiredtest.MCPCommand(t, "npx"),
		nil,
		map[string]desiredmcp.EnvReference{
			"CHILD_TOKEN": desiredtest.MCPEnvReference(t, "HOST_TOKEN"),
		},
	)
	aliasedBinding := desiredtest.MCPBinding(
		t,
		target.TargetCodex,
		target.ScopeGlobal,
		aliasedTransport,
		desiredmcp.OnAbsentRemoveBinding,
	)
	aliasedServer := desiredtest.MCPServer(t, desiredmcp.Spec{
		Name:     "aliased",
		Bindings: []desiredmcp.Binding{aliasedBinding},
	})
	if _, err := CanonicalMCPBindingContribution(aliasedServer, aliasedBinding, placement); err == nil ||
		!strings.Contains(err.Error(), "same-name") {
		t.Fatalf("aliased contribution error = %v, want same-name rejection", err)
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

func TestEveryImplementedMCPPlacementMatchesItsCodecEnvironmentContract(t *testing.T) {
	for _, placement := range aggregate.ImplementedMCPPlacements() {
		t.Run(string(placement.ID()), func(t *testing.T) {
			transport := desiredtest.MCPStdio(
				t,
				desiredtest.MCPCommand(t, "npx"),
				nil,
				nil,
			)
			binding := desiredtest.MCPBinding(
				t,
				placement.Target(),
				placement.Scope(),
				transport,
				desiredmcp.OnAbsentRemoveBinding,
			)
			server := desiredtest.MCPServer(t, desiredmcp.Spec{
				Name:     "catalog-contract",
				Bindings: []desiredmcp.Binding{binding},
			})
			if _, err := CanonicalMCPBindingContribution(server, binding, placement); err != nil {
				t.Fatalf("codec rejected catalog environment contract: %v", err)
			}
		})
	}
}
