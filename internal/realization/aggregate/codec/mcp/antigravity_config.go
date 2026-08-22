package mcpcodec

import (
	"context"

	"github.com/isty2e/daem/internal/realization/aggregate"
)

func mergeAntigravityGlobalMCPServerCanonicalEntry(existing []byte, serverID string, canonical []byte) ([]byte, error) {
	return mergeMCPJSONServerCanonicalEntry(
		existing,
		serverID,
		canonical,
		antigravityGlobalMCPConfigSpec(),
		decodeAntigravityGlobalMCPServerEntry,
	)
}

func foldAntigravityGlobalMCPProjectionMutations(existing []byte, mutations []MCPProjectionMutation) ([]byte, error) {
	return foldMCPJSONServerMutations(
		existing,
		mutations,
		antigravityGlobalMCPConfigSpec(),
		decodeAntigravityGlobalMCPServerEntry,
	)
}

func observeAntigravityGlobalMCPProjections(existing []byte, serverIDs []string) (MCPProjectionObservation, error) {
	return observeMCPJSONServerProjections(
		existing,
		serverIDs,
		antigravityGlobalMCPConfigSpec(),
		decodeAntigravityGlobalMCPServerEntry,
	)
}

func verifyAntigravityGlobalMCPProjectionMutations(content []byte, mutations []MCPProjectionMutation) error {
	return verifyMCPJSONServerMutations(
		content,
		mutations,
		antigravityGlobalMCPConfigSpec(),
		decodeAntigravityGlobalMCPServerEntry,
		antigravityGlobalMCPServerEntriesEqual,
	)
}

func removeAntigravityGlobalMCPServerProjection(existing []byte, serverID string) ([]byte, error) {
	return removeMCPJSONServerProjection(
		existing,
		serverID,
		antigravityGlobalMCPConfigSpec(),
		decodeAntigravityGlobalMCPServerEntry,
	)
}

func restoreRemoveAntigravityGlobalMCPServerProjection(existing []byte, serverID string, parentExistedBefore bool) ([]byte, bool, error) {
	return restoreRemoveMCPJSONServerProjection(
		existing,
		serverID,
		parentExistedBefore,
		antigravityGlobalMCPConfigSpec(),
		decodeAntigravityGlobalMCPServerEntry,
	)
}

// ExtractAntigravityGlobalMCPServerProjection extracts a canonical managed server entry.
func ExtractAntigravityGlobalMCPServerProjection(existing []byte, serverID string) (AntigravityGlobalMCPServerEntry, bool, error) {
	return extractMCPJSONServerProjection(existing, serverID, antigravityGlobalMCPConfigSpec(), decodeAntigravityGlobalMCPServerEntry)
}

func ExtractAntigravityGlobalMCPServerProjections(ctx context.Context, existing []byte) ([]AntigravityGlobalMCPServerProjection, []MCPProjectionRejection, error) {
	config, err := decodeMCPConfigContext(ctx, existing, antigravityGlobalMCPConfigSpec())
	if err != nil {
		if contextErr := ctx.Err(); contextErr != nil {
			return nil, nil, contextErr
		}
		return nil, nil, err
	}
	projections := make([]AntigravityGlobalMCPServerProjection, 0, len(config.servers))
	rejections := make([]MCPProjectionRejection, 0)
	serverIDs := sortedMCPServerIDs(config.servers)
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	for _, serverID := range serverIDs {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		if err := validateServerID(serverID); err != nil {
			rejections = append(rejections, mcpProjectionRejection(aggregate.MCPPlacementAntigravityGlobal, serverID, err))
			continue
		}
		entry, entryErr := decodeAntigravityGlobalMCPServerEntry(config.servers[serverID], serverID)
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		if entryErr != nil {
			rejections = append(rejections, mcpProjectionRejection(aggregate.MCPPlacementAntigravityGlobal, serverID, entryErr))
			continue
		}
		projections = append(projections, AntigravityGlobalMCPServerProjection{
			ServerID:         serverID,
			Command:          entry.Command,
			Args:             append([]string(nil), entry.Args...),
			EnvironmentNames: []string{},
			AdapterContract:  aggregate.AntigravityGlobalMCPAmbientEnvV1,
		})
	}
	return projections, rejections, ctx.Err()
}

func extractAntigravityGlobalMCPServerProjectionBytes(existing []byte, serverID string) ([]byte, bool, error) {
	return extractMCPJSONServerProjectionBytes(
		existing,
		serverID,
		antigravityGlobalMCPConfigSpec(),
		decodeAntigravityGlobalMCPServerEntry,
	)
}

func antigravityGlobalMCPServerEntryPresent(existing []byte, serverID string) (bool, error) {
	return mcpJSONServerEntryPresent(existing, serverID, antigravityGlobalMCPConfigSpec())
}

func antigravityGlobalMCPServersParentPresent(existing []byte) (bool, error) {
	return mcpJSONServersParentPresent(existing, antigravityGlobalMCPConfigSpec())
}
