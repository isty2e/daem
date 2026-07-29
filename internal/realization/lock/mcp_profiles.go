package lock

import (
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/target"
)

var antigravityGlobalMCPProjectionSpec = mcpProjectionLockSpec{
	Placement:                mustProfileMCPPlacement(target.TargetAntigravityCLI, aggregate.MCPPlacementAntigravityGlobal),
	Label:                    "Antigravity CLI global MCP",
	LauncherDependencyPolicy: mcpProjectionAllowCredentialDependencies,
	ReplayExclusions:         antigravityGlobalMCPReplayExclusions,
	WritePreconditions: []string{
		"adapter_contract_current",
		"managed_subtree_absent_or_managed",
		"global_config_json_valid_or_missing",
		"unsupported_managed_fields_absent",
	},
	RemovePreconditions: []string{
		"adapter_contract_current",
		"managed_binding_baseline",
		"global_config_json_valid",
		"unsupported_managed_fields_absent",
	},
}

var claudeGlobalMCPProjectionSpec = mcpProjectionLockSpec{
	Placement:                mustProfileMCPPlacement(target.TargetClaudeCode, aggregate.MCPPlacementClaudeGlobal),
	Label:                    "Claude Code user/global MCP",
	LauncherDependencyPolicy: mcpProjectionAllowCredentialDependencies,
	ReplayExclusions:         claudeGlobalMCPReplayExclusions,
	WritePreconditions: []string{
		"adapter_contract_current",
		"managed_subtree_absent_or_managed",
		"claude_user_shared_json_valid_or_missing",
		"unsupported_managed_fields_absent",
	},
	RemovePreconditions: []string{
		"adapter_contract_current",
		"managed_binding_baseline",
		"claude_user_shared_json_valid",
		"unsupported_managed_fields_absent",
	},
}

var claudeProjectMCPProjectionSpec = mcpProjectionLockSpec{
	Placement:                mustProfileMCPPlacement(target.TargetClaudeCode, aggregate.MCPPlacementClaudeProject),
	Label:                    "Claude project MCP",
	LauncherDependencyPolicy: mcpProjectionAllowCredentialDependencies,
	ReplayExclusions:         claudeProjectMCPReplayExclusions,
	WritePreconditions: []string{
		"adapter_contract_current",
		"managed_subtree_absent_or_managed",
		"project_config_json_valid_or_missing",
		"unsupported_managed_fields_absent",
	},
	RemovePreconditions: []string{
		"adapter_contract_current",
		"managed_binding_baseline",
		"project_config_json_valid",
		"unsupported_managed_fields_absent",
	},
}

var codexProjectMCPProjectionSpec = mcpProjectionLockSpec{
	Placement:                mustProfileMCPPlacement(target.TargetCodex, aggregate.MCPPlacementCodexProject),
	Label:                    "Codex project MCP",
	LauncherDependencyPolicy: mcpProjectionRejectNonLauncherDependencies,
	ReplayExclusions:         codexProjectMCPReplayExclusions,
	WritePreconditions: []string{
		"adapter_contract_current",
		"managed_subtree_absent_or_managed",
		"project_codex_config_toml_valid_or_missing",
		"unsupported_managed_fields_absent",
	},
	RemovePreconditions: []string{
		"adapter_contract_current",
		"managed_binding_baseline",
		"project_codex_config_toml_valid",
		"unsupported_managed_fields_absent",
	},
}

var codexGlobalMCPProjectionSpec = mcpProjectionLockSpec{
	Placement:                mustProfileMCPPlacement(target.TargetCodex, aggregate.MCPPlacementCodexGlobal),
	Label:                    "Codex global MCP",
	LauncherDependencyPolicy: mcpProjectionAllowCredentialDependencies,
	ReplayExclusions:         codexGlobalMCPReplayExclusions,
	WritePreconditions: []string{
		"adapter_contract_current",
		"managed_subtree_absent_or_managed",
		"global_codex_config_toml_valid_or_missing",
		"unsupported_managed_fields_absent",
	},
	RemovePreconditions: []string{
		"adapter_contract_current",
		"managed_binding_baseline",
		"global_codex_config_toml_valid",
		"unsupported_managed_fields_absent",
	},
}

var openCodeProjectMCPProjectionSpec = mcpProjectionLockSpec{
	Placement:                mustProfileMCPPlacement(target.TargetOpenCode, aggregate.MCPPlacementOpenCodeProject),
	Label:                    "OpenCode project MCP",
	LauncherDependencyPolicy: mcpProjectionRejectNonLauncherDependencies,
	ReplayExclusions:         openCodeMCPReplayExclusions,
	WritePreconditions: []string{
		"adapter_contract_current",
		"managed_subtree_absent_or_managed",
		"project_opencode_json_valid_or_missing",
		"project_opencode_jsonc_absent",
		"unsupported_managed_fields_absent",
	},
	RemovePreconditions: []string{
		"adapter_contract_current",
		"managed_binding_baseline",
		"project_opencode_json_valid",
		"project_opencode_jsonc_absent",
		"unsupported_managed_fields_absent",
	},
}

var openCodeGlobalMCPProjectionSpec = mcpProjectionLockSpec{
	Placement:                mustProfileMCPPlacement(target.TargetOpenCode, aggregate.MCPPlacementOpenCodeGlobal),
	Label:                    "OpenCode global MCP",
	LauncherDependencyPolicy: mcpProjectionAllowCredentialDependencies,
	ReplayExclusions:         openCodeMCPReplayExclusions,
	WritePreconditions: []string{
		"adapter_contract_current",
		"managed_subtree_absent_or_managed",
		"global_opencode_json_valid_or_missing",
		"global_opencode_jsonc_absent",
		"unsupported_managed_fields_absent",
	},
	RemovePreconditions: []string{
		"adapter_contract_current",
		"managed_binding_baseline",
		"global_opencode_json_valid",
		"global_opencode_jsonc_absent",
		"unsupported_managed_fields_absent",
	},
}

var piProjectMCPProjectionSpec = mcpProjectionLockSpec{
	Placement:                mustProfileMCPPlacement(target.TargetPi, aggregate.MCPPlacementPiProject),
	Label:                    "Pi project MCP through pi-mcp-adapter",
	LauncherDependencyPolicy: mcpProjectionAllowCredentialDependencies,
	ReplayExclusions:         piMCPReplayExclusions,
	ProviderRequired:         true,
	WritePreconditions: []string{
		"adapter_contract_current",
		"provider_contribution_available",
		"provider_version_compatible",
		"effective_config_collision_free",
		"managed_subtree_absent_or_managed",
		"project_adapter_config_strict_json_or_missing",
		"provider_jsonc_absent",
		"unsupported_managed_fields_absent",
	},
	RemovePreconditions: []string{
		"adapter_contract_current",
		"provider_contribution_available",
		"provider_version_compatible",
		"effective_config_collision_free",
		"managed_binding_baseline",
		"project_adapter_config_strict_json",
		"provider_jsonc_absent",
		"unsupported_managed_fields_absent",
	},
}

var piGlobalMCPProjectionSpec = mcpProjectionLockSpec{
	Placement:                mustProfileMCPPlacement(target.TargetPi, aggregate.MCPPlacementPiGlobal),
	Label:                    "Pi global MCP through pi-mcp-adapter",
	LauncherDependencyPolicy: mcpProjectionAllowCredentialDependencies,
	ReplayExclusions:         piMCPReplayExclusions,
	ProviderRequired:         true,
	WritePreconditions: []string{
		"adapter_contract_current",
		"provider_contribution_available",
		"provider_version_compatible",
		"effective_config_collision_free",
		"managed_subtree_absent_or_managed",
		"global_adapter_config_strict_json_or_missing",
		"provider_jsonc_absent",
		"unsupported_managed_fields_absent",
	},
	RemovePreconditions: []string{
		"adapter_contract_current",
		"provider_contribution_available",
		"provider_version_compatible",
		"effective_config_collision_free",
		"managed_binding_baseline",
		"global_adapter_config_strict_json",
		"provider_jsonc_absent",
		"unsupported_managed_fields_absent",
	},
}

var antigravityGlobalMCPReplayExclusions = []ReplayExclusion{
	{Component: "launcher executable and package manager state", Reason: ReplayExclusionRuntimeDependency},
	{Component: "OAuth/session/auth cache state", Reason: ReplayExclusionOAuthSession},
	{Component: "endpoint health and server startup", Reason: ReplayExclusionRuntimeReadiness},
	{Component: "runtime tool inventory", Reason: ReplayExclusionToolInventory},
	{Component: "plugin or carrier installation", Reason: ReplayExclusionPluginCarrier},
	{Component: "Antigravity IDE configuration", Reason: ReplayExclusionHostApproval},
}

var claudeGlobalMCPReplayExclusions = []ReplayExclusion{
	{Component: "launcher executable and package manager state", Reason: ReplayExclusionRuntimeDependency},
	{Component: "OAuth/session/auth cache state", Reason: ReplayExclusionOAuthSession},
	{Component: "Claude Code user/global trust or approval state", Reason: ReplayExclusionHostApproval},
	{Component: "endpoint health and server startup", Reason: ReplayExclusionRuntimeReadiness},
	{Component: "runtime tool inventory", Reason: ReplayExclusionToolInventory},
	{Component: "plugin or carrier installation", Reason: ReplayExclusionPluginCarrier},
}

var claudeProjectMCPReplayExclusions = []ReplayExclusion{
	{Component: "launcher executable and package manager state", Reason: ReplayExclusionRuntimeDependency},
	{Component: "OAuth/session/auth cache state", Reason: ReplayExclusionOAuthSession},
	{Component: "Claude project approval state", Reason: ReplayExclusionHostApproval},
	{Component: "endpoint health and server startup", Reason: ReplayExclusionRuntimeReadiness},
	{Component: "runtime tool inventory", Reason: ReplayExclusionToolInventory},
	{Component: "plugin or carrier installation", Reason: ReplayExclusionPluginCarrier},
}

var codexProjectMCPReplayExclusions = []ReplayExclusion{
	{Component: "launcher executable and package manager state", Reason: ReplayExclusionRuntimeDependency},
	{Component: "OAuth/session/auth cache state", Reason: ReplayExclusionOAuthSession},
	{Component: "Codex project trust or approval state", Reason: ReplayExclusionHostApproval},
	{Component: "endpoint health and server startup", Reason: ReplayExclusionRuntimeReadiness},
	{Component: "runtime tool inventory", Reason: ReplayExclusionToolInventory},
	{Component: "plugin or carrier installation", Reason: ReplayExclusionPluginCarrier},
}

var codexGlobalMCPReplayExclusions = []ReplayExclusion{
	{Component: "launcher executable and package manager state", Reason: ReplayExclusionRuntimeDependency},
	{Component: "OAuth/session/auth cache state", Reason: ReplayExclusionOAuthSession},
	{Component: "Codex global trust or approval state", Reason: ReplayExclusionHostApproval},
	{Component: "endpoint health and server startup", Reason: ReplayExclusionRuntimeReadiness},
	{Component: "runtime tool inventory", Reason: ReplayExclusionToolInventory},
	{Component: "plugin or carrier installation", Reason: ReplayExclusionPluginCarrier},
}

var openCodeMCPReplayExclusions = []ReplayExclusion{
	{Component: "launcher executable and package manager state", Reason: ReplayExclusionRuntimeDependency},
	{Component: "OAuth/session/auth cache state", Reason: ReplayExclusionOAuthSession},
	{Component: "endpoint health and server startup", Reason: ReplayExclusionRuntimeReadiness},
	{Component: "runtime tool inventory", Reason: ReplayExclusionToolInventory},
	{Component: "plugin or carrier installation", Reason: ReplayExclusionPluginCarrier},
	{Component: "OpenCode effective merged config and admin overlays", Reason: ReplayExclusionHostApproval},
}

var piMCPReplayExclusions = []ReplayExclusion{
	{Component: "pi-mcp-adapter package installation and exact current version", Reason: ReplayExclusionPluginCarrier},
	{Component: "lower config layers, imports, and host config discovery", Reason: ReplayExclusionHostApproval},
	{Component: "Pi project trust and global-provider pre-trust exposure", Reason: ReplayExclusionHostApproval},
	{Component: "launcher executable and package manager state", Reason: ReplayExclusionRuntimeDependency},
	{Component: "endpoint health and server startup", Reason: ReplayExclusionRuntimeReadiness},
	{Component: "runtime tool inventory", Reason: ReplayExclusionToolInventory},
}
