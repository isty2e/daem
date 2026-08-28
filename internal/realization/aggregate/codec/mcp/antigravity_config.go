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

func ExtractAntigravityGlobalMCPServerProjections(
	ctx context.Context,
	existing []byte,
	reject MCPProjectionRejectionSink,
) ([]AntigravityGlobalMCPServerProjection, error) {
	if err := requireMCPProjectionRejectionSink(reject); err != nil {
		return nil, err
	}
	config, err := decodeMCPConfigContext(ctx, existing, antigravityGlobalMCPConfigSpec())
	if err != nil {
		if contextErr := ctx.Err(); contextErr != nil {
			return nil, contextErr
		}
		return nil, err
	}
	projections := make([]AntigravityGlobalMCPServerProjection, 0)
	serverIDs := sortedMCPServerIDs(config.servers)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	for _, serverID := range serverIDs {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if err := validateServerID(serverID); err != nil {
			if err := reject(mcpProjectionRejection(aggregate.MCPPlacementAntigravityGlobal, serverID, err)); err != nil {
				return nil, err
			}
			continue
		}
		entry, entryErr := decodeAntigravityGlobalMCPServerEntry(config.servers[serverID], serverID)
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if entryErr != nil {
			if err := reject(mcpProjectionRejection(aggregate.MCPPlacementAntigravityGlobal, serverID, entryErr)); err != nil {
				return nil, err
			}
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
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return projections, nil
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
