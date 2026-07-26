package mcpcodec

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"sort"

	"github.com/isty2e/daem/internal/realization/aggregate"
)

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

func ExtractClaudeProjectMCPServerProjections(existing []byte) ([]ClaudeProjectMCPServerProjection, []MCPProjectionRejection, error) {
	config, err := decodeMCPConfig(existing, claudeProjectMCPConfigSpec())
	if err != nil {
		return nil, nil, err
	}
	projections := make([]ClaudeProjectMCPServerProjection, 0, len(config.servers))
	rejections := make([]MCPProjectionRejection, 0)
	for _, serverID := range sortedMCPServerIDs(config.servers) {
		contentPath := ClaudeProjectMCPContentPath(serverID)
		if err := validateServerID(serverID); err != nil {
			rejections = append(rejections, mcpProjectionRejection(contentPath, err))
			continue
		}
		entry, err := decodeClaudeProjectMCPServerEntry(config.servers[serverID], serverID)
		if err != nil {
			rejections = append(rejections, mcpProjectionRejection(contentPath, err))
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
	return projections, rejections, nil
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
	configPath  string
	label       string
	serversKey  string
	serversPath string
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
		configPath:  placement.ConfigPath(),
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
	config := mcpConfig{
		spec:    spec,
		top:     make(map[string]json.RawMessage),
		servers: make(map[string]json.RawMessage),
	}
	if content == nil {
		return config, nil
	}
	if len(bytes.TrimSpace(content)) == 0 {
		return mcpConfig{}, newMCPProjectionError(
			MCPProjectionReasonConfigMalformed,
			spec.configPath,
			spec.label+" config JSON is empty",
		)
	}
	if err := rejectDuplicateMCPConfigKeys(content, spec); err != nil {
		return mcpConfig{}, err
	}

	decoder := json.NewDecoder(bytes.NewReader(content))
	if err := decoder.Decode(&config.top); err != nil {
		return mcpConfig{}, newMCPProjectionError(
			MCPProjectionReasonConfigMalformed,
			spec.configPath,
			fmt.Sprintf("decode %s config JSON: %v", spec.label, err),
		)
	}
	var extra json.RawMessage
	if err := decoder.Decode(&extra); err == nil {
		return mcpConfig{}, newMCPProjectionError(
			MCPProjectionReasonConfigMalformed,
			spec.configPath,
			spec.label+" config JSON contains multiple values",
		)
	} else if err != io.EOF {
		return mcpConfig{}, newMCPProjectionError(
			MCPProjectionReasonConfigMalformed,
			spec.configPath,
			fmt.Sprintf("decode trailing %s config JSON: %v", spec.label, err),
		)
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
		return config, nil
	}
	if err := decodeObject(rawServers, &config.servers, spec.serversPath); err != nil {
		return mcpConfig{}, err
	}
	return config, nil
}

func (config mcpConfig) encode() ([]byte, error) {
	content, _, err := config.encodePreservingMCPParent(true)
	return content, err
}

func (config mcpConfig) encodePreservingMCPParent(parentExistedBefore bool) ([]byte, bool, error) {
	if len(config.servers) == 0 && !parentExistedBefore {
		delete(config.top, config.spec.serversKey)
		if len(config.top) == 0 {
			return nil, false, nil
		}
		content, err := canonicalJSON(config.top)
		return content, true, err
	}
	serversRaw, err := encodeSortedRawObject(config.servers)
	if err != nil {
		return nil, false, err
	}
	config.top[config.spec.serversKey] = serversRaw
	content, err := canonicalJSON(config.top)
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

func rejectDuplicateMCPConfigKeys(content []byte, spec mcpConfigSpec) error {
	decoder := json.NewDecoder(bytes.NewReader(content))
	if err := scanMCPConfigJSONValue(decoder, spec.configPath, spec); err != nil {
		return err
	}
	if _, err := decoder.Token(); err == nil {
		return newMCPProjectionError(
			MCPProjectionReasonConfigMalformed,
			spec.configPath,
			spec.label+" config JSON contains multiple values",
		)
	} else if err != io.EOF {
		return newMCPProjectionError(
			MCPProjectionReasonConfigMalformed,
			spec.configPath,
			fmt.Sprintf("decode trailing %s config JSON: %v", spec.label, err),
		)
	}
	return nil
}

func scanMCPConfigJSONValue(decoder *json.Decoder, subject string, spec mcpConfigSpec) error {
	token, err := decoder.Token()
	if err != nil {
		return newMCPProjectionError(
			MCPProjectionReasonConfigMalformed,
			subject,
			fmt.Sprintf("decode %s config JSON: %v", spec.label, err),
		)
	}
	delimiter, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delimiter {
	case '{':
		return scanMCPConfigJSONObject(decoder, subject, spec)
	case '[':
		return scanMCPConfigJSONArray(decoder, subject, spec)
	default:
		return newMCPProjectionError(
			MCPProjectionReasonConfigMalformed,
			subject,
			"unexpected JSON delimiter",
		)
	}
}

func scanMCPConfigJSONObject(decoder *json.Decoder, subject string, spec mcpConfigSpec) error {
	seen := make(map[string]struct{})
	for decoder.More() {
		keyToken, err := decoder.Token()
		if err != nil {
			return newMCPProjectionError(
				MCPProjectionReasonConfigMalformed,
				subject,
				fmt.Sprintf("decode %s config object key: %v", spec.label, err),
			)
		}
		key, ok := keyToken.(string)
		if !ok {
			return newMCPProjectionError(
				MCPProjectionReasonConfigMalformed,
				subject,
				spec.label+" config object key is not a string",
			)
		}
		if _, ok := seen[key]; ok {
			return newMCPProjectionError(
				MCPProjectionReasonProjectionEquivalenceUndefined,
				subject,
				fmt.Sprintf("JSON object contains duplicate key %q", key),
			)
		}
		seen[key] = struct{}{}
		if err := scanMCPConfigJSONValue(decoder, subject+"/"+key, spec); err != nil {
			return err
		}
	}
	endToken, err := decoder.Token()
	if err != nil {
		return newMCPProjectionError(
			MCPProjectionReasonConfigMalformed,
			subject,
			fmt.Sprintf("decode %s config object terminator: %v", spec.label, err),
		)
	}
	if endToken != json.Delim('}') {
		return newMCPProjectionError(
			MCPProjectionReasonConfigMalformed,
			subject,
			spec.label+" config object is not closed",
		)
	}
	return nil
}

func scanMCPConfigJSONArray(decoder *json.Decoder, subject string, spec mcpConfigSpec) error {
	for decoder.More() {
		if err := scanMCPConfigJSONValue(decoder, subject+"[]", spec); err != nil {
			return err
		}
	}
	endToken, err := decoder.Token()
	if err != nil {
		return newMCPProjectionError(
			MCPProjectionReasonConfigMalformed,
			subject,
			fmt.Sprintf("decode %s config array terminator: %v", spec.label, err),
		)
	}
	if endToken != json.Delim(']') {
		return newMCPProjectionError(
			MCPProjectionReasonConfigMalformed,
			subject,
			spec.label+" config array is not closed",
		)
	}
	return nil
}
