package mcpcodec

func restoreClaudeProjectMCPProjectionMutations(
	existing []byte,
	mutations []MCPProjectionMutation,
	parentExistedBefore bool,
) ([]byte, bool, error) {
	return restoreMCPJSONServerMutations(
		existing,
		mutations,
		parentExistedBefore,
		claudeProjectMCPConfigSpec(),
		decodeClaudeProjectMCPServerEntry,
	)
}

func restoreClaudeGlobalMCPProjectionMutations(
	existing []byte,
	mutations []MCPProjectionMutation,
	parentExistedBefore bool,
) ([]byte, bool, error) {
	return restoreMCPJSONServerMutations(
		existing,
		mutations,
		parentExistedBefore,
		claudeGlobalMCPConfigSpec(),
		decodeClaudeGlobalMCPServerEntry,
	)
}

func restoreAntigravityGlobalMCPProjectionMutations(
	existing []byte,
	mutations []MCPProjectionMutation,
	parentExistedBefore bool,
) ([]byte, bool, error) {
	return restoreMCPJSONServerMutations(
		existing,
		mutations,
		parentExistedBefore,
		antigravityGlobalMCPConfigSpec(),
		decodeAntigravityGlobalMCPServerEntry,
	)
}

func restoreOpenCodeProjectMCPProjectionMutations(
	existing []byte,
	mutations []MCPProjectionMutation,
	parentExistedBefore bool,
) ([]byte, bool, error) {
	return restoreMCPJSONServerMutations(
		existing,
		mutations,
		parentExistedBefore,
		openCodeProjectMCPConfigSpec(),
		decodeOpenCodeProjectMCPServerEntry,
	)
}

func restoreOpenCodeGlobalMCPProjectionMutations(
	existing []byte,
	mutations []MCPProjectionMutation,
	parentExistedBefore bool,
) ([]byte, bool, error) {
	return restoreMCPJSONServerMutations(
		existing,
		mutations,
		parentExistedBefore,
		openCodeGlobalMCPConfigSpec(),
		decodeOpenCodeGlobalMCPServerEntry,
	)
}

func restoreCodexProjectMCPProjectionMutations(
	existing []byte,
	mutations []MCPProjectionMutation,
	parentExistedBefore bool,
) ([]byte, bool, error) {
	return restoreCodexMCPProjectionMutations(
		existing,
		mutations,
		parentExistedBefore,
		"Codex project MCP",
		codexProjectMCPEntryContract,
	)
}

func restoreCodexGlobalMCPProjectionMutations(
	existing []byte,
	mutations []MCPProjectionMutation,
	parentExistedBefore bool,
) ([]byte, bool, error) {
	return restoreCodexMCPProjectionMutations(
		existing,
		mutations,
		parentExistedBefore,
		"Codex global MCP",
		codexGlobalMCPEntryContract,
	)
}
