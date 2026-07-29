package aggregate_test

import (
	"reflect"
	"testing"

	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/target"
)

func TestImplementedMCPPlacementsExposeCurrentRowsOnly(t *testing.T) {
	cases := []struct {
		name              string
		target            target.Target
		scope             target.Scope
		wantID            aggregate.MCPPlacementID
		wantLayer         aggregate.MCPConfigLayer
		wantConfigPath    string
		wantConflictPath  string
		wantPathPrefix    aggregate.MCPContentPathPrefix
		wantContentPath   string
		wantCodec         aggregate.CodecContractID
		wantEnvMapping    aggregate.MCPEnvReferenceMapping
		wantEnvResolution aggregate.MCPEnvReferenceResolution
	}{
		{
			name:              "claude project",
			target:            target.TargetClaudeCode,
			scope:             target.ScopeProject,
			wantID:            aggregate.MCPPlacementClaudeProject,
			wantLayer:         aggregate.MCPConfigLayerClaudeProjectFile,
			wantConfigPath:    ".mcp.json",
			wantPathPrefix:    "/mcpServers",
			wantContentPath:   "/mcpServers/context7",
			wantCodec:         aggregate.MCPCodecClaudeProjectStdio,
			wantEnvMapping:    aggregate.MCPEnvMappingAliased,
			wantEnvResolution: aggregate.MCPEnvResolutionHostRuntime,
		},
		{
			name:              "claude global",
			target:            target.TargetClaudeCode,
			scope:             target.ScopeGlobal,
			wantID:            aggregate.MCPPlacementClaudeGlobal,
			wantLayer:         aggregate.MCPConfigLayerClaudeUserSharedJSON,
			wantConfigPath:    "~/.claude.json",
			wantPathPrefix:    "/mcpServers",
			wantContentPath:   "/mcpServers/context7",
			wantCodec:         aggregate.MCPCodecClaudeGlobalStdioEnv,
			wantEnvMapping:    aggregate.MCPEnvMappingAliased,
			wantEnvResolution: aggregate.MCPEnvResolutionHostRuntime,
		},
		{
			name:              "antigravity global",
			target:            target.TargetAntigravityCLI,
			scope:             target.ScopeGlobal,
			wantID:            aggregate.MCPPlacementAntigravityGlobal,
			wantLayer:         aggregate.MCPConfigLayerAntigravityGlobalDefaultFile,
			wantConfigPath:    "~/.gemini/config/mcp_config.json",
			wantPathPrefix:    "/mcpServers",
			wantContentPath:   "/mcpServers/context7",
			wantCodec:         aggregate.MCPCodecAntigravityGlobalAmbientEnv,
			wantEnvMapping:    aggregate.MCPEnvMappingSameName,
			wantEnvResolution: aggregate.MCPEnvResolutionHostRuntime,
		},
		{
			name:              "opencode project",
			target:            target.TargetOpenCode,
			scope:             target.ScopeProject,
			wantID:            aggregate.MCPPlacementOpenCodeProject,
			wantLayer:         aggregate.MCPConfigLayerOpenCodeProjectFile,
			wantConfigPath:    "opencode.json",
			wantConflictPath:  "opencode.jsonc",
			wantPathPrefix:    "/mcp",
			wantContentPath:   "/mcp/context7",
			wantCodec:         aggregate.MCPCodecOpenCodeProjectLocal,
			wantEnvMapping:    aggregate.MCPEnvMappingUnsupported,
			wantEnvResolution: aggregate.MCPEnvResolutionUnavailable,
		},
		{
			name:              "opencode global",
			target:            target.TargetOpenCode,
			scope:             target.ScopeGlobal,
			wantID:            aggregate.MCPPlacementOpenCodeGlobal,
			wantLayer:         aggregate.MCPConfigLayerOpenCodeGlobalDefaultJSON,
			wantConfigPath:    "~/.config/opencode/opencode.json",
			wantConflictPath:  "~/.config/opencode/opencode.jsonc",
			wantPathPrefix:    "/mcp",
			wantContentPath:   "/mcp/context7",
			wantCodec:         aggregate.MCPCodecOpenCodeGlobalLocalEnv,
			wantEnvMapping:    aggregate.MCPEnvMappingAliased,
			wantEnvResolution: aggregate.MCPEnvResolutionHostRuntime,
		},
		{
			name:              "codex project",
			target:            target.TargetCodex,
			scope:             target.ScopeProject,
			wantID:            aggregate.MCPPlacementCodexProject,
			wantLayer:         aggregate.MCPConfigLayerCodexProjectFile,
			wantConfigPath:    ".codex/config.toml",
			wantPathPrefix:    "/mcp_servers",
			wantContentPath:   "/mcp_servers/context7",
			wantCodec:         aggregate.MCPCodecCodexProjectStdioCommand,
			wantEnvMapping:    aggregate.MCPEnvMappingUnsupported,
			wantEnvResolution: aggregate.MCPEnvResolutionUnavailable,
		},
		{
			name:              "codex global",
			target:            target.TargetCodex,
			scope:             target.ScopeGlobal,
			wantID:            aggregate.MCPPlacementCodexGlobal,
			wantLayer:         aggregate.MCPConfigLayerCodexGlobalDefaultFile,
			wantConfigPath:    "~/.codex/config.toml",
			wantPathPrefix:    "/mcp_servers",
			wantContentPath:   "/mcp_servers/context7",
			wantCodec:         aggregate.MCPCodecCodexGlobalStdioEnvVars,
			wantEnvMapping:    aggregate.MCPEnvMappingSameName,
			wantEnvResolution: aggregate.MCPEnvResolutionHostRuntime,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			placement, ok := aggregate.ImplementedMCPPlacement(tc.target, tc.scope)
			if !ok {
				t.Fatalf("ImplementedMCPPlacement(%q, %q) was not found", tc.target, tc.scope)
			}
			if err := placement.Validate(); err != nil {
				t.Fatalf("placement did not validate: %v", err)
			}
			contentPath, err := placement.ContentPath("context7")
			if err != nil {
				t.Fatalf("ContentPath returned error: %v", err)
			}
			conflictingConfigPath, hasConflictingConfigPath := placement.ConflictingConfigPath()
			envReferences := placement.EnvReferenceContract()
			if placement.ID() != tc.wantID ||
				placement.ConfigLayer() != tc.wantLayer ||
				placement.AggregateRoot().String() != tc.wantConfigPath ||
				placement.ConfigPath().String() != tc.wantConfigPath ||
				conflictingConfigPath.String() != tc.wantConflictPath ||
				hasConflictingConfigPath != (tc.wantConflictPath != "") ||
				placement.MergeUnit() != aggregate.MCPMergeUnitServerEntry ||
				placement.ContentPathPrefix() != tc.wantPathPrefix ||
				string(contentPath) != tc.wantContentPath ||
				placement.SiblingRetention() != aggregate.MCPSiblingRetentionPreserveUnmanaged ||
				placement.CodecContractID() != tc.wantCodec ||
				placement.Absence() != aggregate.MCPAbsenceRemoveBinding ||
				envReferences.Mapping() != tc.wantEnvMapping ||
				envReferences.Resolution() != tc.wantEnvResolution ||
				envReferences.Supported() != (tc.wantEnvMapping != aggregate.MCPEnvMappingUnsupported) {
				t.Fatalf("placement = %#v", placement)
			}
			contribution, err := placement.Contribution("context7", "canonical-context7")
			if err != nil {
				t.Fatalf("Contribution returned error: %v", err)
			}
			if contribution.PlacementID() != string(tc.wantID) ||
				contribution.Target() != tc.target ||
				contribution.Scope() != tc.scope ||
				contribution.AggregateRoot().String() != tc.wantConfigPath ||
				contribution.ContentPath() != tc.wantContentPath ||
				contribution.MergeUnit() != aggregate.MergeUnit(aggregate.MCPMergeUnitServerEntry) ||
				contribution.Cardinality() != aggregate.ContributionExclusive ||
				contribution.SiblingRetention() != aggregate.SiblingRetention(aggregate.MCPSiblingRetentionPreserveUnmanaged) ||
				contribution.SiblingPreservation() != aggregate.PreserveSiblingsSemantic ||
				contribution.Equivalence() != aggregate.EquivalenceCanonicalSemantic ||
				contribution.CanonicalContribution() != "canonical-context7" ||
				contribution.CodecContractID() != tc.wantCodec ||
				!reflect.DeepEqual(contribution.ComparedFields(), placement.ComparedFields()) {
				t.Fatalf("contribution = %#v", contribution)
			}
			contract, err := placement.ProjectionContract("context7")
			if err != nil {
				t.Fatalf("ProjectionContract returned error: %v", err)
			}
			if !contract.Equal(contribution.Contract()) {
				t.Fatalf("projection contract = %#v, want contribution contract %#v", contract, contribution.Contract())
			}
		})
	}
}

func TestImplementedMCPPlacementIncludesPiProviderRows(t *testing.T) {
	for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
		placement, ok := aggregate.ImplementedMCPPlacement(target.TargetPi, scope)
		if !ok {
			t.Fatalf("ImplementedMCPPlacement(%q, %q) is missing", target.TargetPi, scope)
		}
		if placement.CodecContractID() != aggregate.MCPCodecPiAdapterStdio {
			t.Fatalf("Pi %q codec = %q, want %q", scope, placement.CodecContractID(), aggregate.MCPCodecPiAdapterStdio)
		}
	}
}

func TestMCPPlacementRejectsMalformedRowsAndServerIDs(t *testing.T) {
	valid := aggregate.MCPPlacementInput{
		ID:                     "codex.project.test-config",
		Target:                 target.TargetCodex,
		Scope:                  target.ScopeProject,
		ConfigLayer:            "codex-project-test-config",
		ConfigPath:             ".codex/test.toml",
		MergeUnit:              aggregate.MCPMergeUnitServerEntry,
		ContentPathPrefix:      "/mcp_servers",
		SiblingRetention:       aggregate.MCPSiblingRetentionPreserveUnmanaged,
		CodecContractID:        "codex-project-test-mcp-stdio-command-v1",
		ComparedFields:         []string{"target", "command", "target"},
		Absence:                aggregate.MCPAbsenceRemoveBinding,
		EnvReferenceMapping:    aggregate.MCPEnvMappingUnsupported,
		EnvReferenceResolution: aggregate.MCPEnvResolutionUnavailable,
	}
	cases := []struct {
		name string
		edit func(*aggregate.MCPPlacementInput)
	}{
		{name: "bad id", edit: func(input *aggregate.MCPPlacementInput) { input.ID = "bad/id" }},
		{name: "missing config path", edit: func(input *aggregate.MCPPlacementInput) { input.ConfigPath = "" }},
		{name: "conflict equals config path", edit: func(input *aggregate.MCPPlacementInput) { input.ConflictingConfigPath = input.ConfigPath }},
		{name: "missing merge unit", edit: func(input *aggregate.MCPPlacementInput) { input.MergeUnit = "" }},
		{name: "unsupported merge unit", edit: func(input *aggregate.MCPPlacementInput) { input.MergeUnit = "whole-file" }},
		{name: "bad content path prefix", edit: func(input *aggregate.MCPPlacementInput) { input.ContentPathPrefix = "mcp_servers" }},
		{name: "trailing slash content path prefix", edit: func(input *aggregate.MCPPlacementInput) { input.ContentPathPrefix = "/mcp_servers/" }},
		{name: "redundant slash content path prefix", edit: func(input *aggregate.MCPPlacementInput) { input.ContentPathPrefix = "/mcp//servers" }},
		{name: "dot segment content path prefix", edit: func(input *aggregate.MCPPlacementInput) { input.ContentPathPrefix = "/mcp/../servers" }},
		{name: "backslash content path prefix", edit: func(input *aggregate.MCPPlacementInput) { input.ContentPathPrefix = `/mcp\servers` }},
		{name: "control content path prefix", edit: func(input *aggregate.MCPPlacementInput) { input.ContentPathPrefix = "/mcp\x00servers" }},
		{name: "missing sibling retention", edit: func(input *aggregate.MCPPlacementInput) { input.SiblingRetention = "" }},
		{name: "unsupported sibling retention", edit: func(input *aggregate.MCPPlacementInput) { input.SiblingRetention = "replace_aggregate" }},
		{name: "missing compared fields", edit: func(input *aggregate.MCPPlacementInput) { input.ComparedFields = nil }},
		{name: "invalid compared field", edit: func(input *aggregate.MCPPlacementInput) { input.ComparedFields = []string{"bad field"} }},
		{name: "missing env mapping", edit: func(input *aggregate.MCPPlacementInput) { input.EnvReferenceMapping = "" }},
		{name: "missing env resolution", edit: func(input *aggregate.MCPPlacementInput) { input.EnvReferenceResolution = "" }},
		{name: "unsupported env with runtime resolution", edit: func(input *aggregate.MCPPlacementInput) {
			input.EnvReferenceResolution = aggregate.MCPEnvResolutionHostRuntime
		}},
		{name: "aliased env with unavailable resolution", edit: func(input *aggregate.MCPPlacementInput) {
			input.EnvReferenceMapping = aggregate.MCPEnvMappingAliased
		}},
		{name: "unsupported absence", edit: func(input *aggregate.MCPPlacementInput) { input.Absence = "delete_config" }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			input := valid
			tc.edit(&input)
			if placement, err := aggregate.NewMCPPlacement(input); err == nil {
				t.Fatalf("NewMCPPlacement = %#v, want error", placement)
			}
		})
	}

	placement, ok := aggregate.ImplementedMCPPlacement(target.TargetCodex, target.ScopeProject)
	if !ok {
		t.Fatal("Codex project placement missing")
	}
	for _, serverID := range []string{"", "bad/id", " context7", "context7 "} {
		if contentPath, err := placement.ContentPath(serverID); err == nil {
			t.Fatalf("ContentPath(%q) = %q, want error", serverID, contentPath)
		}
		if contribution, err := placement.Contribution(serverID, "canonical"); err == nil {
			t.Fatalf("Contribution(%q) = %#v, want error", serverID, contribution)
		}
		if contract, err := placement.ProjectionContract(serverID); err == nil {
			t.Fatalf("ProjectionContract(%q) = %#v, want error", serverID, contract)
		}
	}
}

func TestMCPProjectionAddressSeparatesProjectAndGlobalScopes(t *testing.T) {
	projectPlacement, ok := aggregate.ImplementedMCPPlacement(target.TargetCodex, target.ScopeProject)
	if !ok {
		t.Fatal("Codex project placement missing")
	}
	globalPlacement, ok := aggregate.ImplementedMCPPlacement(target.TargetCodex, target.ScopeGlobal)
	if !ok {
		t.Fatal("Codex global placement missing")
	}

	projectContract, err := projectPlacement.ProjectionContract("context7")
	if err != nil {
		t.Fatalf("project ProjectionContract returned error: %v", err)
	}
	globalContract, err := globalPlacement.ProjectionContract("context7")
	if err != nil {
		t.Fatalf("global ProjectionContract returned error: %v", err)
	}
	projectAddress := projectContract.Address()
	globalAddress := globalContract.Address()

	if projectAddress.ContentPath() != globalAddress.ContentPath() {
		t.Fatalf("test setup no longer exercises same content path: %q vs %q", projectAddress.ContentPath(), globalAddress.ContentPath())
	}
	if projectAddress.Document().AggregateRoot() == globalAddress.Document().AggregateRoot() {
		t.Fatalf("project/global aggregate roots collapsed: %q", projectAddress.Document().AggregateRoot())
	}
	if projectAddress.PlacementID() == globalAddress.PlacementID() {
		t.Fatalf("project/global placement identities collapsed: %q", projectAddress.PlacementID())
	}
	if projectAddress == globalAddress {
		t.Fatalf("project/global projection addresses collapsed: %#v", projectAddress)
	}
}

func TestMCPProjectionAddressIncludesPlacementIDForSameRootAndPath(t *testing.T) {
	basePlacement, ok := aggregate.ImplementedMCPPlacement(target.TargetCodex, target.ScopeProject)
	if !ok {
		t.Fatal("Codex project placement missing")
	}
	alternatePlacement, err := aggregate.NewMCPPlacement(aggregate.MCPPlacementInput{
		ID:                     "codex.project.same-root-alternate",
		Target:                 target.TargetCodex,
		Scope:                  target.ScopeProject,
		ConfigLayer:            "codex-project-same-root-alternate",
		ConfigPath:             basePlacement.ConfigPath().String(),
		MergeUnit:              aggregate.MCPMergeUnitServerEntry,
		ContentPathPrefix:      basePlacement.ContentPathPrefix(),
		SiblingRetention:       aggregate.MCPSiblingRetentionPreserveUnmanaged,
		CodecContractID:        "codex-project-same-root-alternate-v1",
		ComparedFields:         basePlacement.ComparedFields(),
		Absence:                aggregate.MCPAbsenceRemoveBinding,
		EnvReferenceMapping:    aggregate.MCPEnvMappingUnsupported,
		EnvReferenceResolution: aggregate.MCPEnvResolutionUnavailable,
	})
	if err != nil {
		t.Fatalf("NewMCPPlacement returned error: %v", err)
	}

	baseContract, err := basePlacement.ProjectionContract("context7")
	if err != nil {
		t.Fatalf("base ProjectionContract returned error: %v", err)
	}
	alternateContract, err := alternatePlacement.ProjectionContract("context7")
	if err != nil {
		t.Fatalf("alternate ProjectionContract returned error: %v", err)
	}
	baseAddress := baseContract.Address()
	alternateAddress := alternateContract.Address()

	if baseAddress.Document().AggregateRoot() != alternateAddress.Document().AggregateRoot() ||
		baseAddress.ContentPath() != alternateAddress.ContentPath() {
		t.Fatalf("test setup no longer exercises same root/path: %#v vs %#v", baseAddress, alternateAddress)
	}
	if baseAddress == alternateAddress {
		t.Fatalf("projection address collapsed distinct placements: %#v", baseAddress)
	}
}

func TestMCPPlacementStaticContractAccessorsAreCanonicalAndDefensive(t *testing.T) {
	placement, ok := aggregate.ImplementedMCPPlacement(target.TargetCodex, target.ScopeProject)
	if !ok {
		t.Fatal("Codex project placement missing")
	}

	fields := placement.ComparedFields()
	if !reflect.DeepEqual(fields, []string{
		"adapter_contract", "args", "command", "config_path", "content_path",
		"scope", "server_id", "target",
	}) {
		t.Fatalf("ComparedFields = %#v", fields)
	}
	fields[0] = "mutated"
	if placement.ComparedFields()[0] != "adapter_contract" {
		t.Fatalf("ComparedFields retained caller mutation: %#v", placement.ComparedFields())
	}
}
