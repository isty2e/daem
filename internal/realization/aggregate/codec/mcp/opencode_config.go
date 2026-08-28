package mcpcodec

import (
	"context"

	"github.com/isty2e/daem/internal/realization/aggregate"
)

func mergeOpenCodeProjectMCPServerCanonicalEntry(existing []byte, serverID string, canonical []byte) ([]byte, error) {
	return mergeOpenCodeMCPServerCanonicalEntry(existing, serverID, canonical, openCodeProjectMCPConfigSpec())
}

func foldOpenCodeProjectMCPProjectionMutations(existing []byte, mutations []MCPProjectionMutation) ([]byte, error) {
	return foldOpenCodeMCPProjectionMutations(existing, mutations, openCodeProjectMCPConfigSpec())
}

func foldOpenCodeGlobalMCPProjectionMutations(existing []byte, mutations []MCPProjectionMutation) ([]byte, error) {
	return foldMCPJSONServerMutations(
		existing,
		mutations,
		openCodeGlobalMCPConfigSpec(),
		decodeOpenCodeGlobalMCPServerEntry,
	)
}

func observeOpenCodeProjectMCPProjections(existing []byte, serverIDs []string) (MCPProjectionObservation, error) {
	return observeOpenCodeMCPProjections(existing, serverIDs, openCodeProjectMCPConfigSpec())
}

func observeOpenCodeGlobalMCPProjections(existing []byte, serverIDs []string) (MCPProjectionObservation, error) {
	return observeMCPJSONServerProjections(
		existing,
		serverIDs,
		openCodeGlobalMCPConfigSpec(),
		decodeOpenCodeGlobalMCPServerEntry,
	)
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
	return verifyMCPJSONServerMutations(
		content,
		mutations,
		openCodeGlobalMCPConfigSpec(),
		decodeOpenCodeGlobalMCPServerEntry,
		openCodeGlobalMCPServerEntriesEqual,
	)
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
	return mergeMCPJSONServerCanonicalEntry(
		existing,
		serverID,
		canonical,
		openCodeGlobalMCPConfigSpec(),
		decodeOpenCodeGlobalMCPServerEntry,
	)
}

func mergeOpenCodeMCPServerCanonicalEntry(existing []byte, serverID string, canonical []byte, spec mcpConfigSpec) ([]byte, error) {
	return mergeMCPJSONServerCanonicalEntry(existing, serverID, canonical, spec, decodeOpenCodeProjectMCPServerEntry)
}

func removeOpenCodeProjectMCPServerProjection(existing []byte, serverID string) ([]byte, error) {
	return removeOpenCodeMCPServerProjection(existing, serverID, openCodeProjectMCPConfigSpec())
}

func removeOpenCodeGlobalMCPServerProjection(existing []byte, serverID string) ([]byte, error) {
	return removeMCPJSONServerProjection(
		existing,
		serverID,
		openCodeGlobalMCPConfigSpec(),
		decodeOpenCodeGlobalMCPServerEntry,
	)
}

func removeOpenCodeMCPServerProjection(existing []byte, serverID string, spec mcpConfigSpec) ([]byte, error) {
	return removeMCPJSONServerProjection(existing, serverID, spec, decodeOpenCodeProjectMCPServerEntry)
}

func restoreRemoveOpenCodeProjectMCPServerProjection(existing []byte, serverID string, parentExistedBefore bool) ([]byte, bool, error) {
	return restoreRemoveOpenCodeMCPServerProjection(existing, serverID, parentExistedBefore, openCodeProjectMCPConfigSpec())
}

func restoreRemoveOpenCodeGlobalMCPServerProjection(existing []byte, serverID string, parentExistedBefore bool) ([]byte, bool, error) {
	return restoreRemoveMCPJSONServerProjection(
		existing,
		serverID,
		parentExistedBefore,
		openCodeGlobalMCPConfigSpec(),
		decodeOpenCodeGlobalMCPServerEntry,
	)
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
func ExtractOpenCodeProjectMCPServerProjection(existing []byte, serverID string) (OpenCodeProjectMCPServerEntry, bool, error) {
	return extractOpenCodeMCPServerProjection(existing, serverID, openCodeProjectMCPConfigSpec())
}

// ExtractOpenCodeGlobalMCPServerProjection extracts a canonical managed server entry.
func ExtractOpenCodeGlobalMCPServerProjection(existing []byte, serverID string) (OpenCodeGlobalMCPServerEntry, bool, error) {
	return extractMCPJSONServerProjection(
		existing,
		serverID,
		openCodeGlobalMCPConfigSpec(),
		decodeOpenCodeGlobalMCPServerEntry,
	)
}

func extractOpenCodeMCPServerProjection(existing []byte, serverID string, spec mcpConfigSpec) (OpenCodeProjectMCPServerEntry, bool, error) {
	return extractMCPJSONServerProjection(existing, serverID, spec, decodeOpenCodeProjectMCPServerEntry)
}

func ExtractOpenCodeProjectMCPServerProjections(
	ctx context.Context,
	existing []byte,
	reject MCPProjectionRejectionSink,
) ([]MCPNoEnvServerProjection, error) {
	return extractOpenCodeMCPServerProjections(
		ctx,
		existing,
		openCodeProjectMCPConfigSpec(),
		aggregate.OpenCodeProjectMCPLocalCommandV1,
		reject,
	)
}

func ExtractOpenCodeGlobalMCPServerProjections(
	ctx context.Context,
	existing []byte,
	reject MCPProjectionRejectionSink,
) ([]OpenCodeGlobalMCPServerProjection, error) {
	if err := requireMCPProjectionRejectionSink(reject); err != nil {
		return nil, err
	}
	config, err := decodeMCPConfigContext(ctx, existing, openCodeGlobalMCPConfigSpec())
	if err != nil {
		if contextErr := ctx.Err(); contextErr != nil {
			return nil, contextErr
		}
		return nil, err
	}
	projections := make([]OpenCodeGlobalMCPServerProjection, 0)
	serverIDs := sortedMCPServerIDs(config.servers)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	for _, serverID := range serverIDs {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if err := validateServerID(serverID); err != nil {
			if err := reject(mcpProjectionRejection(aggregate.MCPPlacementOpenCodeGlobal, serverID, err)); err != nil {
				return nil, err
			}
			continue
		}
		entry, entryErr := decodeOpenCodeGlobalMCPServerEntry(config.servers[serverID], serverID)
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if entryErr != nil {
			if err := reject(mcpProjectionRejection(aggregate.MCPPlacementOpenCodeGlobal, serverID, entryErr)); err != nil {
				return nil, err
			}
			continue
		}
		projections = append(projections, OpenCodeGlobalMCPServerProjection{
			ServerID:        serverID,
			Command:         entry.Command[0],
			Args:            append([]string(nil), entry.Command[1:]...),
			Environment:     cloneStringMap(entry.Environment),
			AdapterContract: aggregate.OpenCodeGlobalMCPLocalEnvV1,
		})
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return projections, nil
}

func extractOpenCodeMCPServerProjections(
	ctx context.Context,
	existing []byte,
	spec mcpConfigSpec,
	adapterContract string,
	reject MCPProjectionRejectionSink,
) ([]MCPNoEnvServerProjection, error) {
	if err := requireMCPProjectionRejectionSink(reject); err != nil {
		return nil, err
	}
	config, err := decodeMCPConfigContext(ctx, existing, spec)
	if err != nil {
		if contextErr := ctx.Err(); contextErr != nil {
			return nil, contextErr
		}
		return nil, err
	}
	projections := make([]MCPNoEnvServerProjection, 0)
	serverIDs := sortedMCPServerIDs(config.servers)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	for _, serverID := range serverIDs {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if err := validateServerID(serverID); err != nil {
			if err := reject(mcpProjectionRejection(aggregate.MCPPlacementOpenCodeProject, serverID, err)); err != nil {
				return nil, err
			}
			continue
		}
		entry, entryErr := decodeOpenCodeProjectMCPServerEntry(config.servers[serverID], serverID)
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if entryErr != nil {
			if err := reject(mcpProjectionRejection(aggregate.MCPPlacementOpenCodeProject, serverID, entryErr)); err != nil {
				return nil, err
			}
			continue
		}
		projections = append(projections, MCPNoEnvServerProjection{
			ServerID:        serverID,
			Command:         entry.Command[0],
			Args:            append([]string(nil), entry.Command[1:]...),
			AdapterContract: adapterContract,
		})
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return projections, nil
}

func extractOpenCodeProjectMCPServerProjectionBytes(existing []byte, serverID string) ([]byte, bool, error) {
	return extractOpenCodeMCPServerProjectionBytes(existing, serverID, openCodeProjectMCPConfigSpec())
}

func extractOpenCodeGlobalMCPServerProjectionBytes(existing []byte, serverID string) ([]byte, bool, error) {
	return extractMCPJSONServerProjectionBytes(
		existing,
		serverID,
		openCodeGlobalMCPConfigSpec(),
		decodeOpenCodeGlobalMCPServerEntry,
	)
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
