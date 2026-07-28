package mcpcodec

import "github.com/isty2e/daem/internal/realization/aggregate"

func mergeClaudeGlobalMCPServerCanonicalEntry(existing []byte, serverID string, canonical []byte) ([]byte, error) {
	return mergeMCPJSONServerCanonicalEntry(
		existing,
		serverID,
		canonical,
		claudeGlobalMCPConfigSpec(),
		decodeClaudeGlobalMCPServerEntry,
	)
}

func foldClaudeGlobalMCPProjectionMutations(existing []byte, mutations []MCPProjectionMutation) ([]byte, error) {
	return foldMCPJSONServerMutations(
		existing,
		mutations,
		claudeGlobalMCPConfigSpec(),
		decodeClaudeGlobalMCPServerEntry,
	)
}

func observeClaudeGlobalMCPProjections(existing []byte, serverIDs []string) (MCPProjectionObservation, error) {
	return observeMCPJSONServerProjections(
		existing,
		serverIDs,
		claudeGlobalMCPConfigSpec(),
		decodeClaudeGlobalMCPServerEntry,
	)
}

func verifyClaudeGlobalMCPProjectionMutations(content []byte, mutations []MCPProjectionMutation) error {
	return verifyMCPJSONServerMutations(
		content,
		mutations,
		claudeGlobalMCPConfigSpec(),
		decodeClaudeGlobalMCPServerEntry,
		claudeGlobalMCPServerEntriesEqual,
	)
}

func removeClaudeGlobalMCPServerProjection(existing []byte, serverID string) ([]byte, error) {
	return removeMCPJSONServerProjection(
		existing,
		serverID,
		claudeGlobalMCPConfigSpec(),
		decodeClaudeGlobalMCPServerEntry,
	)
}

func restoreRemoveClaudeGlobalMCPServerProjection(existing []byte, serverID string, parentExistedBefore bool) ([]byte, bool, error) {
	return restoreRemoveMCPJSONServerProjection(
		existing,
		serverID,
		parentExistedBefore,
		claudeGlobalMCPConfigSpec(),
		decodeClaudeGlobalMCPServerEntry,
	)
}

// ExtractClaudeGlobalMCPServerProjection extracts a canonical user/global managed server entry.
func ExtractClaudeGlobalMCPServerProjection(existing []byte, serverID string) (ClaudeGlobalMCPServerEntry, bool, error) {
	return extractMCPJSONServerProjection(existing, serverID, claudeGlobalMCPConfigSpec(), decodeClaudeGlobalMCPServerEntry)
}

func ExtractClaudeGlobalMCPServerProjections(existing []byte) ([]ClaudeGlobalMCPServerProjection, []MCPProjectionRejection, error) {
	config, err := decodeMCPConfig(existing, claudeGlobalMCPConfigSpec())
	if err != nil {
		return nil, nil, err
	}
	projections := make([]ClaudeGlobalMCPServerProjection, 0, len(config.servers))
	rejections := make([]MCPProjectionRejection, 0)
	for _, serverID := range sortedMCPServerIDs(config.servers) {
		contentPath := ClaudeGlobalMCPContentPath(serverID)
		if err := validateServerID(serverID); err != nil {
			rejections = append(rejections, mcpProjectionRejection(contentPath, err))
			continue
		}
		entry, err := decodeClaudeGlobalMCPServerEntry(config.servers[serverID], serverID)
		if err != nil {
			rejections = append(rejections, mcpProjectionRejection(contentPath, err))
			continue
		}
		projections = append(projections, ClaudeGlobalMCPServerProjection{
			ServerID:        serverID,
			Command:         entry.Command,
			Args:            append([]string(nil), entry.Args...),
			Env:             cloneStringMap(entry.Env),
			AdapterContract: aggregate.ClaudeGlobalMCPStdioEnvAdapterV1,
		})
	}
	return projections, rejections, nil
}

func extractClaudeGlobalMCPServerProjectionBytes(existing []byte, serverID string) ([]byte, bool, error) {
	return extractMCPJSONServerProjectionBytes(
		existing,
		serverID,
		claudeGlobalMCPConfigSpec(),
		decodeClaudeGlobalMCPServerEntry,
	)
}

func claudeGlobalMCPServerEntryPresent(existing []byte, serverID string) (bool, error) {
	return mcpJSONServerEntryPresent(existing, serverID, claudeGlobalMCPConfigSpec())
}

func claudeGlobalMCPServersParentPresent(existing []byte) (bool, error) {
	return mcpJSONServersParentPresent(existing, claudeGlobalMCPConfigSpec())
}
