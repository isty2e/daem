package mcpcodec

import "github.com/isty2e/daem/internal/realization/aggregate"

func mergeOpenCodeProjectMCPServerCanonicalEntry(existing []byte, serverID string, canonical []byte) ([]byte, error) {
	return mergeOpenCodeMCPServerCanonicalEntry(existing, serverID, canonical, openCodeProjectMCPConfigSpec())
}

func foldOpenCodeProjectMCPProjectionMutations(existing []byte, mutations []MCPProjectionMutation) ([]byte, error) {
	return foldOpenCodeMCPProjectionMutations(existing, mutations, openCodeProjectMCPConfigSpec())
}

func foldOpenCodeGlobalMCPProjectionMutations(existing []byte, mutations []MCPProjectionMutation) ([]byte, error) {
	return foldOpenCodeMCPProjectionMutations(existing, mutations, openCodeGlobalMCPConfigSpec())
}

func observeOpenCodeProjectMCPProjections(existing []byte, serverIDs []string) (MCPProjectionObservation, error) {
	return observeOpenCodeMCPProjections(existing, serverIDs, openCodeProjectMCPConfigSpec())
}

func observeOpenCodeGlobalMCPProjections(existing []byte, serverIDs []string) (MCPProjectionObservation, error) {
	return observeOpenCodeMCPProjections(existing, serverIDs, openCodeGlobalMCPConfigSpec())
}

func observeOpenCodeMCPProjections(
	existing []byte,
	serverIDs []string,
	spec mcpConfigSpec,
) (MCPProjectionObservation, error) {
	return observeMCPJSONServerProjections(existing, serverIDs, spec, decodeOpenCodeProjectMCPServerEntry)
}

func foldOpenCodeMCPProjectionMutations(
	existing []byte,
	mutations []MCPProjectionMutation,
	spec mcpConfigSpec,
) ([]byte, error) {
	return foldMCPJSONServerMutations(existing, mutations, spec, decodeOpenCodeProjectMCPServerEntry)
}

func verifyOpenCodeProjectMCPProjectionMutations(content []byte, mutations []MCPProjectionMutation) error {
	return verifyOpenCodeMCPProjectionMutations(content, mutations, openCodeProjectMCPConfigSpec())
}

func verifyOpenCodeGlobalMCPProjectionMutations(content []byte, mutations []MCPProjectionMutation) error {
	return verifyOpenCodeMCPProjectionMutations(content, mutations, openCodeGlobalMCPConfigSpec())
}

func verifyOpenCodeMCPProjectionMutations(
	content []byte,
	mutations []MCPProjectionMutation,
	spec mcpConfigSpec,
) error {
	return verifyMCPJSONServerMutations(
		content,
		mutations,
		spec,
		decodeOpenCodeProjectMCPServerEntry,
		openCodeProjectMCPServerEntriesEqual,
	)
}

func mergeOpenCodeGlobalMCPServerCanonicalEntry(existing []byte, serverID string, canonical []byte) ([]byte, error) {
	return mergeOpenCodeMCPServerCanonicalEntry(existing, serverID, canonical, openCodeGlobalMCPConfigSpec())
}

func mergeOpenCodeMCPServerCanonicalEntry(existing []byte, serverID string, canonical []byte, spec mcpConfigSpec) ([]byte, error) {
	return mergeMCPJSONServerCanonicalEntry(existing, serverID, canonical, spec, decodeOpenCodeProjectMCPServerEntry)
}

func removeOpenCodeProjectMCPServerProjection(existing []byte, serverID string) ([]byte, error) {
	return removeOpenCodeMCPServerProjection(existing, serverID, openCodeProjectMCPConfigSpec())
}

func removeOpenCodeGlobalMCPServerProjection(existing []byte, serverID string) ([]byte, error) {
	return removeOpenCodeMCPServerProjection(existing, serverID, openCodeGlobalMCPConfigSpec())
}

func removeOpenCodeMCPServerProjection(existing []byte, serverID string, spec mcpConfigSpec) ([]byte, error) {
	return removeMCPJSONServerProjection(existing, serverID, spec, decodeOpenCodeProjectMCPServerEntry)
}

func restoreRemoveOpenCodeProjectMCPServerProjection(existing []byte, serverID string, parentExistedBefore bool) ([]byte, bool, error) {
	return restoreRemoveOpenCodeMCPServerProjection(existing, serverID, parentExistedBefore, openCodeProjectMCPConfigSpec())
}

func restoreRemoveOpenCodeGlobalMCPServerProjection(existing []byte, serverID string, parentExistedBefore bool) ([]byte, bool, error) {
	return restoreRemoveOpenCodeMCPServerProjection(existing, serverID, parentExistedBefore, openCodeGlobalMCPConfigSpec())
}

func restoreRemoveOpenCodeMCPServerProjection(existing []byte, serverID string, parentExistedBefore bool, spec mcpConfigSpec) ([]byte, bool, error) {
	return restoreRemoveMCPJSONServerProjection(
		existing,
		serverID,
		parentExistedBefore,
		spec,
		decodeOpenCodeProjectMCPServerEntry,
	)
}

// ExtractOpenCodeProjectMCPServerProjection extracts a canonical managed server entry.
func ExtractOpenCodeProjectMCPServerProjection(existing []byte, serverID string) (OpenCodeMCPServerEntry, bool, error) {
	return extractOpenCodeMCPServerProjection(existing, serverID, openCodeProjectMCPConfigSpec())
}

// ExtractOpenCodeGlobalMCPServerProjection extracts a canonical managed server entry.
func ExtractOpenCodeGlobalMCPServerProjection(existing []byte, serverID string) (OpenCodeMCPServerEntry, bool, error) {
	return extractOpenCodeMCPServerProjection(existing, serverID, openCodeGlobalMCPConfigSpec())
}

func extractOpenCodeMCPServerProjection(existing []byte, serverID string, spec mcpConfigSpec) (OpenCodeMCPServerEntry, bool, error) {
	return extractMCPJSONServerProjection(existing, serverID, spec, decodeOpenCodeProjectMCPServerEntry)
}

func ExtractOpenCodeProjectMCPServerProjections(existing []byte) ([]MCPNoEnvServerProjection, []MCPProjectionRejection, error) {
	return extractOpenCodeMCPServerProjections(existing, openCodeProjectMCPConfigSpec(), aggregate.OpenCodeProjectMCPLocalCommandV1)
}

func ExtractOpenCodeGlobalMCPServerProjections(existing []byte) ([]MCPNoEnvServerProjection, []MCPProjectionRejection, error) {
	projectProjections, rejections, err := extractOpenCodeMCPServerProjections(existing, openCodeGlobalMCPConfigSpec(), aggregate.OpenCodeGlobalMCPLocalCommandV1)
	if err != nil {
		return nil, nil, err
	}
	projections := make([]MCPNoEnvServerProjection, 0, len(projectProjections))
	for _, projection := range projectProjections {
		projections = append(projections, MCPNoEnvServerProjection{
			ServerID:        projection.ServerID,
			Command:         projection.Command,
			Args:            append([]string(nil), projection.Args...),
			AdapterContract: projection.AdapterContract,
		})
	}
	return projections, rejections, nil
}

func extractOpenCodeMCPServerProjections(existing []byte, spec mcpConfigSpec, adapterContract string) ([]MCPNoEnvServerProjection, []MCPProjectionRejection, error) {
	config, err := decodeMCPConfig(existing, spec)
	if err != nil {
		return nil, nil, err
	}
	projections := make([]MCPNoEnvServerProjection, 0, len(config.servers))
	rejections := make([]MCPProjectionRejection, 0)
	for _, serverID := range sortedMCPServerIDs(config.servers) {
		contentPath := OpenCodeProjectMCPContentPath(serverID)
		if err := validateServerID(serverID); err != nil {
			rejections = append(rejections, mcpProjectionRejection(contentPath, err))
			continue
		}
		entry, err := decodeOpenCodeProjectMCPServerEntry(config.servers[serverID], serverID)
		if err != nil {
			rejections = append(rejections, mcpProjectionRejection(contentPath, err))
			continue
		}
		projections = append(projections, MCPNoEnvServerProjection{
			ServerID:        serverID,
			Command:         entry.Command[0],
			Args:            append([]string(nil), entry.Command[1:]...),
			AdapterContract: adapterContract,
		})
	}
	return projections, rejections, nil
}

func extractOpenCodeProjectMCPServerProjectionBytes(existing []byte, serverID string) ([]byte, bool, error) {
	return extractOpenCodeMCPServerProjectionBytes(existing, serverID, openCodeProjectMCPConfigSpec())
}

func extractOpenCodeGlobalMCPServerProjectionBytes(existing []byte, serverID string) ([]byte, bool, error) {
	return extractOpenCodeMCPServerProjectionBytes(existing, serverID, openCodeGlobalMCPConfigSpec())
}

func extractOpenCodeMCPServerProjectionBytes(existing []byte, serverID string, spec mcpConfigSpec) ([]byte, bool, error) {
	return extractMCPJSONServerProjectionBytes(existing, serverID, spec, decodeOpenCodeProjectMCPServerEntry)
}

func openCodeProjectMCPServerEntryPresent(existing []byte, serverID string) (bool, error) {
	return openCodeMCPServerEntryPresent(existing, serverID, openCodeProjectMCPConfigSpec())
}

func openCodeGlobalMCPServerEntryPresent(existing []byte, serverID string) (bool, error) {
	return openCodeMCPServerEntryPresent(existing, serverID, openCodeGlobalMCPConfigSpec())
}

func openCodeMCPServerEntryPresent(existing []byte, serverID string, spec mcpConfigSpec) (bool, error) {
	return mcpJSONServerEntryPresent(existing, serverID, spec)
}

func openCodeProjectMCPServersParentPresent(existing []byte) (bool, error) {
	return openCodeMCPServersParentPresent(existing, openCodeProjectMCPConfigSpec())
}

func openCodeGlobalMCPServersParentPresent(existing []byte) (bool, error) {
	return openCodeMCPServersParentPresent(existing, openCodeGlobalMCPConfigSpec())
}

func openCodeMCPServersParentPresent(existing []byte, spec mcpConfigSpec) (bool, error) {
	return mcpJSONServersParentPresent(existing, spec)
}
