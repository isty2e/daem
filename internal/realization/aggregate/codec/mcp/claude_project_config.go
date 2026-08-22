package mcpcodec

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"sort"

	"github.com/isty2e/daem/internal/encoding/jsonstrict"
	"github.com/isty2e/daem/internal/realization/aggregate"
)

const maximumMCPJSONDepth = 64

func mergeClaudeProjectMCPServerCanonicalEntry(existing []byte, serverID string, canonical []byte) ([]byte, error) {
	return mergeMCPJSONServerCanonicalEntry(
		existing,
		serverID,
		canonical,
		claudeProjectMCPConfigSpec(),
		decodeClaudeProjectMCPServerEntry,
	)
}

func foldClaudeProjectMCPProjectionMutations(existing []byte, mutations []MCPProjectionMutation) ([]byte, error) {
	return foldMCPJSONServerMutations(
		existing,
		mutations,
		claudeProjectMCPConfigSpec(),
		decodeClaudeProjectMCPServerEntry,
	)
}

func observeClaudeProjectMCPProjections(existing []byte, serverIDs []string) (MCPProjectionObservation, error) {
	return observeMCPJSONServerProjections(
		existing,
		serverIDs,
		claudeProjectMCPConfigSpec(),
		decodeClaudeProjectMCPServerEntry,
	)
}

func verifyClaudeProjectMCPProjectionMutations(content []byte, mutations []MCPProjectionMutation) error {
	return verifyMCPJSONServerMutations(
		content,
		mutations,
		claudeProjectMCPConfigSpec(),
		decodeClaudeProjectMCPServerEntry,
		mcpServerEntriesEqual,
	)
}

func removeClaudeProjectMCPServerProjection(existing []byte, serverID string) ([]byte, error) {
	return removeMCPJSONServerProjection(
		existing,
		serverID,
		claudeProjectMCPConfigSpec(),
		decodeClaudeProjectMCPServerEntry,
	)
}

func restoreRemoveClaudeProjectMCPServerProjection(existing []byte, serverID string, parentExistedBefore bool) ([]byte, bool, error) {
	return restoreRemoveMCPJSONServerProjection(
		existing,
		serverID,
		parentExistedBefore,
		claudeProjectMCPConfigSpec(),
		decodeClaudeProjectMCPServerEntry,
	)
}

// ExtractClaudeProjectMCPServerProjection extracts a canonical managed server entry.
func ExtractClaudeProjectMCPServerProjection(existing []byte, serverID string) (ClaudeProjectMCPServerEntry, bool, error) {
	return extractMCPJSONServerProjection(existing, serverID, claudeProjectMCPConfigSpec(), decodeClaudeProjectMCPServerEntry)
}

func ExtractClaudeProjectMCPServerProjections(ctx context.Context, existing []byte) ([]ClaudeProjectMCPServerProjection, []MCPProjectionRejection, error) {
	config, err := decodeMCPConfigContext(ctx, existing, claudeProjectMCPConfigSpec())
	if err != nil {
		if contextErr := ctx.Err(); contextErr != nil {
			return nil, nil, contextErr
		}
		return nil, nil, err
	}
	projections := make([]ClaudeProjectMCPServerProjection, 0, len(config.servers))
	rejections := make([]MCPProjectionRejection, 0)
	serverIDs := sortedMCPServerIDs(config.servers)
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	for _, serverID := range serverIDs {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		contentPath := ClaudeProjectMCPContentPath(serverID)
		if err := validateServerID(serverID); err != nil {
			rejections = append(rejections, mcpProjectionRejection(contentPath, err))
			continue
		}
		entry, entryErr := decodeClaudeProjectMCPServerEntry(config.servers[serverID], serverID)
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		if entryErr != nil {
			rejections = append(rejections, mcpProjectionRejection(contentPath, entryErr))
			continue
		}
		projections = append(projections, ClaudeProjectMCPServerProjection{
			ServerID:        serverID,
			Command:         entry.Command,
			Args:            append([]string(nil), entry.Args...),
			Env:             cloneStringMap(entry.Env),
			AdapterContract: aggregate.ClaudeProjectMCPStdioAdapterV1,
		})
	}
	return projections, rejections, ctx.Err()
}

func extractClaudeProjectMCPServerProjectionBytes(existing []byte, serverID string) ([]byte, bool, error) {
	return extractMCPJSONServerProjectionBytes(
		existing,
		serverID,
		claudeProjectMCPConfigSpec(),
		decodeClaudeProjectMCPServerEntry,
	)
}

func sortedMCPServerIDs(servers map[string]json.RawMessage) []string {
	serverIDs := make([]string, 0, len(servers))
	for serverID := range servers {
		serverIDs = append(serverIDs, serverID)
	}
	sort.Strings(serverIDs)
	return serverIDs
}

func mcpProjectionRejection(contentPath string, err error) MCPProjectionRejection {
	reason, ok := MCPProjectionReasonCodeOf(err)
	if !ok {
		reason = MCPProjectionReasonProjectionEquivalenceUndefined
	}
	return MCPProjectionRejection{ContentPath: contentPath, Reason: reason}
}

func cloneStringMap(values map[string]string) map[string]string {
	result := make(map[string]string, len(values))
	maps.Copy(result, values)
	return result
}

func claudeProjectMCPServerEntryPresent(existing []byte, serverID string) (bool, error) {
	return mcpJSONServerEntryPresent(existing, serverID, claudeProjectMCPConfigSpec())
}

func claudeProjectMCPServersParentPresent(existing []byte) (bool, error) {
	return mcpJSONServersParentPresent(existing, claudeProjectMCPConfigSpec())
}

type mcpConfigSpec struct {
	configPath        string
	label             string
	serversKey        string
	serversPath       string
	documentAdmission func([]byte, mcpConfigSpec) error
}

func (spec mcpConfigSpec) withDocumentAdmission(
	admission func([]byte, mcpConfigSpec) error,
) mcpConfigSpec {
	spec.documentAdmission = admission
	return spec
}

func claudeProjectMCPConfigSpec() mcpConfigSpec {
	return mcpConfigSpecForPlacement(aggregate.MCPPlacementClaudeProject, "Claude project MCP", mcpManagedServersField)
}

func claudeGlobalMCPConfigSpec() mcpConfigSpec {
	return mcpConfigSpecForPlacement(aggregate.MCPPlacementClaudeGlobal, "Claude Code user/global MCP", mcpManagedServersField)
}

func antigravityGlobalMCPConfigSpec() mcpConfigSpec {
	return mcpConfigSpecForPlacement(aggregate.MCPPlacementAntigravityGlobal, "Antigravity CLI global MCP", mcpManagedServersField)
}

func openCodeProjectMCPConfigSpec() mcpConfigSpec {
	return mcpConfigSpecForPlacement(aggregate.MCPPlacementOpenCodeProject, "OpenCode project MCP", openCodeProjectMCPManagedField)
}

func openCodeGlobalMCPConfigSpec() mcpConfigSpec {
	return mcpConfigSpecForPlacement(aggregate.MCPPlacementOpenCodeGlobal, "OpenCode global MCP", openCodeProjectMCPManagedField)
}

func mcpConfigSpecForPlacement(id aggregate.MCPPlacementID, label string, serversKey string) mcpConfigSpec {
	placement, ok := aggregate.MCPPlacementForID(id)
	if !ok {
		panic(fmt.Sprintf("implemented MCP placement %q is missing", id))
	}
	return mcpConfigSpec{
		configPath:  placement.ConfigPath().String(),
		label:       label,
		serversKey:  serversKey,
		serversPath: string(placement.ContentPathPrefix()),
	}
}

type mcpConfig struct {
	spec    mcpConfigSpec
	top     map[string]json.RawMessage
	servers map[string]json.RawMessage
}

func decodeMCPConfig(content []byte, spec mcpConfigSpec) (mcpConfig, error) {
	return decodeMCPConfigContext(context.Background(), content, spec)
}

func decodeMCPConfigContext(ctx context.Context, content []byte, spec mcpConfigSpec) (mcpConfig, error) {
	if ctx == nil {
		return mcpConfig{}, fmt.Errorf("MCP projection extraction context is required")
	}
	if err := ctx.Err(); err != nil {
		return mcpConfig{}, err
	}
	config := mcpConfig{
		spec:    spec,
		top:     make(map[string]json.RawMessage),
		servers: make(map[string]json.RawMessage),
	}
	if content == nil {
		return config, nil
	}
	if err := validateMCPDocumentSize(content); err != nil {
		return mcpConfig{}, mcpJSONHostDocumentError(spec, err)
	}
	trimmed := bytes.TrimSpace(content)
	if err := ctx.Err(); err != nil {
		return mcpConfig{}, err
	}
	if len(trimmed) == 0 {
		return mcpConfig{}, newMCPProjectionError(
			MCPProjectionReasonConfigMalformed,
			spec.configPath,
			spec.label+" config JSON is empty",
		)
	}
	admit := spec.documentAdmission
	if admit == nil {
		if err := admitStrictMCPJSONDocumentContext(ctx, content, spec); err != nil {
			return mcpConfig{}, err
		}
	} else if err := admit(content, spec); err != nil {
		if contextErr := ctx.Err(); contextErr != nil {
			return mcpConfig{}, contextErr
		}
		return mcpConfig{}, err
	}
	if err := ctx.Err(); err != nil {
		return mcpConfig{}, err
	}

	decoder := json.NewDecoder(bytes.NewReader(content))
	if err := decoder.Decode(&config.top); err != nil {
		if contextErr := ctx.Err(); contextErr != nil {
			return mcpConfig{}, contextErr
		}
		return mcpConfig{}, newMCPProjectionError(
			MCPProjectionReasonConfigMalformed,
			spec.configPath,
			fmt.Sprintf("decode %s config JSON: %v", spec.label, err),
		)
	}
	if err := ctx.Err(); err != nil {
		return mcpConfig{}, err
	}
	if config.top == nil {
		return mcpConfig{}, newMCPProjectionError(
			MCPProjectionReasonConfigMalformed,
			spec.configPath,
			spec.label+" config JSON must be an object",
		)
	}

	rawServers, ok := config.top[spec.serversKey]
	if !ok {
		return config, ctx.Err()
	}
	if err := decodeObject(rawServers, &config.servers, spec.serversPath); err != nil {
		if contextErr := ctx.Err(); contextErr != nil {
			return mcpConfig{}, contextErr
		}
		return mcpConfig{}, err
	}
	if err := ctx.Err(); err != nil {
		return mcpConfig{}, err
	}
	return config, nil
}

func admitStrictMCPJSONDocument(content []byte, spec mcpConfigSpec) error {
	return admitStrictMCPJSONDocumentContext(context.Background(), content, spec)
}

func admitStrictMCPJSONDocumentContext(ctx context.Context, content []byte, spec mcpConfigSpec) error {
	if err := jsonstrict.ValidateContext(ctx, content, spec.label+" config JSON", maximumMCPJSONDepth); err != nil {
		if contextErr := ctx.Err(); contextErr != nil {
			return contextErr
		}
		return mcpJSONHostDocumentError(spec, err)
	}
	return nil
}

func mcpJSONHostDocumentError(spec mcpConfigSpec, err error) error {
	reason := MCPProjectionReasonConfigMalformed
	if errors.Is(err, jsonstrict.ErrDuplicateObjectKey) {
		reason = MCPProjectionReasonDuplicateKey
	}
	return newMCPProjectionError(
		reason,
		spec.configPath,
		fmt.Sprintf("decode %s config JSON: %v", spec.label, err),
	)
}

type mcpJSONConfigEncodingPlan struct {
	expectedBytes    int64
	keepDocument     bool
	includeMCPParent bool
}

func (config mcpConfig) encodingPlan(parentExistedBefore bool) (mcpJSONConfigEncodingPlan, error) {
	if len(config.servers) == 0 && !parentExistedBefore {
		top := maps.Clone(config.top)
		delete(top, config.spec.serversKey)
		if len(top) == 0 {
			return mcpJSONConfigEncodingPlan{}, nil
		}
		expectedBytes, err := canonicalJSONEncodedSize(top)
		if err != nil {
			return mcpJSONConfigEncodingPlan{}, canonicalMCPJSONError(
				"",
				"encode canonical MCP JSON",
				err,
			)
		}
		return mcpJSONConfigEncodingPlan{
			expectedBytes: expectedBytes,
			keepDocument:  true,
		}, nil
	}

	expectedBytes, err := canonicalMCPJSONConfigEncodedSize(
		config.top,
		config.spec.serversKey,
		config.servers,
	)
	if err != nil {
		return mcpJSONConfigEncodingPlan{}, canonicalMCPJSONError(
			"",
			"encode canonical MCP JSON",
			err,
		)
	}
	return mcpJSONConfigEncodingPlan{
		expectedBytes:    expectedBytes,
		keepDocument:     true,
		includeMCPParent: true,
	}, nil
}

func (config mcpConfig) preflightEncodePreservingMCPParent(parentExistedBefore bool) error {
	_, err := config.encodingPlan(parentExistedBefore)
	return err
}

func (config mcpConfig) encode() ([]byte, error) {
	content, _, err := config.encodePreservingMCPParent(true)
	return content, err
}

func (config mcpConfig) encodePreservingMCPParent(parentExistedBefore bool) ([]byte, bool, error) {
	plan, err := config.encodingPlan(parentExistedBefore)
	if err != nil {
		return nil, false, err
	}
	if !plan.keepDocument {
		return nil, false, nil
	}
	if !plan.includeMCPParent {
		delete(config.top, config.spec.serversKey)
		content, err := marshalPreflightedCanonicalJSON(config.top, plan.expectedBytes)
		return content, true, err
	}

	serversRaw, err := encodeSortedRawObject(config.servers)
	if err != nil {
		return nil, false, err
	}
	config.top[config.spec.serversKey] = serversRaw
	content, err := marshalPreflightedCanonicalJSON(config.top, plan.expectedBytes)
	return content, true, err
}

func decodeObject(raw json.RawMessage, destination *map[string]json.RawMessage, subject string) error {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil || object == nil {
		return newMCPProjectionError(
			MCPProjectionReasonProjectionEquivalenceUndefined,
			subject,
			"projection object is not a JSON object",
		)
	}
	*destination = object
	return nil
}

func encodeSortedRawObject(values map[string]json.RawMessage) (json.RawMessage, error) {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	ordered := make(map[string]json.RawMessage, len(values))
	for _, key := range keys {
		ordered[key] = values[key]
	}
	content, err := canonicalJSON(ordered)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(content), nil
}
