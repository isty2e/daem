package mcpcodec

import (
	"bytes"
	"context"
	"fmt"
	"sort"

	"github.com/BurntSushi/toml"
	"github.com/isty2e/daem/internal/encoding/tomlstrict"
	"github.com/isty2e/daem/internal/realization/aggregate"
)

func mergeCodexProjectMCPServerCanonicalEntry(existing []byte, serverID string, canonical []byte) ([]byte, error) {
	if err := validateCodexMCPServerID(serverID); err != nil {
		return nil, err
	}
	desired, err := decodeCodexProjectMCPServerEntry(canonical, serverID)
	if err != nil {
		return nil, err
	}

	config, err := decodeCodexProjectMCPConfig(existing)
	if err != nil {
		return nil, err
	}
	if existingValue, exists := config.servers[serverID]; exists {
		if _, err := decodeCodexProjectMCPServerEntryValue(existingValue, serverID); err != nil {
			return nil, err
		}
	}
	config.servers[serverID] = codexProjectMCPServerEntryMap(desired)
	return config.encode()
}

func mergeCodexGlobalMCPServerCanonicalEntry(existing []byte, serverID string, canonical []byte) ([]byte, error) {
	if err := validateCodexMCPServerID(serverID); err != nil {
		return nil, err
	}
	desired, err := decodeCodexGlobalMCPServerEntry(canonical, serverID)
	if err != nil {
		return nil, err
	}

	config, err := decodeCodexProjectMCPConfig(existing)
	if err != nil {
		return nil, err
	}
	if existingValue, exists := config.servers[serverID]; exists {
		if _, err := decodeCodexGlobalMCPServerEntryValue(existingValue, serverID); err != nil {
			return nil, err
		}
	}
	config.servers[serverID] = codexGlobalMCPServerEntryMap(desired)
	return config.encode()
}

func removeCodexProjectMCPServerProjection(existing []byte, serverID string) ([]byte, error) {
	if err := validateCodexMCPServerID(serverID); err != nil {
		return nil, err
	}
	config, err := decodeCodexProjectMCPConfig(existing)
	if err != nil {
		return nil, err
	}
	if existingValue, exists := config.servers[serverID]; exists {
		if _, err := decodeCodexProjectMCPServerEntryValue(existingValue, serverID); err != nil {
			return nil, err
		}
		delete(config.servers, serverID)
	}
	return config.encode()
}

func removeCodexGlobalMCPServerProjection(existing []byte, serverID string) ([]byte, error) {
	if err := validateCodexMCPServerID(serverID); err != nil {
		return nil, err
	}
	config, err := decodeCodexProjectMCPConfig(existing)
	if err != nil {
		return nil, err
	}
	if existingValue, exists := config.servers[serverID]; exists {
		if _, err := decodeCodexGlobalMCPServerEntryValue(existingValue, serverID); err != nil {
			return nil, err
		}
		delete(config.servers, serverID)
	}
	return config.encode()
}

func restoreRemoveCodexProjectMCPServerProjection(existing []byte, serverID string, parentExistedBefore bool) ([]byte, bool, error) {
	if err := validateCodexMCPServerID(serverID); err != nil {
		return nil, false, err
	}
	config, err := decodeCodexProjectMCPConfig(existing)
	if err != nil {
		return nil, false, err
	}
	if existingValue, exists := config.servers[serverID]; exists {
		if _, err := decodeCodexProjectMCPServerEntryValue(existingValue, serverID); err != nil {
			return nil, false, err
		}
		delete(config.servers, serverID)
	}
	content, keepFile, err := config.encodePreservingMCPParent(parentExistedBefore)
	if err != nil {
		return nil, false, err
	}
	return content, keepFile, nil
}

func restoreRemoveCodexGlobalMCPServerProjection(existing []byte, serverID string, parentExistedBefore bool) ([]byte, bool, error) {
	if err := validateCodexMCPServerID(serverID); err != nil {
		return nil, false, err
	}
	config, err := decodeCodexProjectMCPConfig(existing)
	if err != nil {
		return nil, false, err
	}
	if existingValue, exists := config.servers[serverID]; exists {
		if _, err := decodeCodexGlobalMCPServerEntryValue(existingValue, serverID); err != nil {
			return nil, false, err
		}
		delete(config.servers, serverID)
	}
	content, keepFile, err := config.encodePreservingMCPParent(parentExistedBefore)
	if err != nil {
		return nil, false, err
	}
	return content, keepFile, nil
}

// ExtractCodexProjectMCPServerProjection extracts a canonical managed server entry.
func ExtractCodexProjectMCPServerProjection(existing []byte, serverID string) (CodexProjectMCPServerEntry, bool, error) {
	if err := validateCodexMCPServerID(serverID); err != nil {
		return CodexProjectMCPServerEntry{}, false, err
	}
	config, err := decodeCodexProjectMCPConfig(existing)
	if err != nil {
		return CodexProjectMCPServerEntry{}, false, err
	}
	value, ok := config.servers[serverID]
	if !ok {
		return CodexProjectMCPServerEntry{}, false, nil
	}
	entry, err := decodeCodexProjectMCPServerEntryValue(value, serverID)
	if err != nil {
		return CodexProjectMCPServerEntry{}, false, err
	}
	return entry, true, nil
}

// ExtractCodexGlobalMCPServerProjection extracts a canonical managed server entry.
func ExtractCodexGlobalMCPServerProjection(existing []byte, serverID string) (CodexGlobalMCPServerEntry, bool, error) {
	if err := validateCodexMCPServerID(serverID); err != nil {
		return CodexGlobalMCPServerEntry{}, false, err
	}
	config, err := decodeCodexProjectMCPConfig(existing)
	if err != nil {
		return CodexGlobalMCPServerEntry{}, false, err
	}
	value, ok := config.servers[serverID]
	if !ok {
		return CodexGlobalMCPServerEntry{}, false, nil
	}
	entry, err := decodeCodexGlobalMCPServerEntryValue(value, serverID)
	if err != nil {
		return CodexGlobalMCPServerEntry{}, false, err
	}
	return entry, true, nil
}

func ExtractCodexProjectMCPServerProjections(existing []byte) ([]MCPNoEnvServerProjection, []MCPProjectionRejection, error) {
	config, err := decodeCodexProjectMCPConfig(existing)
	if err != nil {
		return nil, nil, err
	}
	projections := make([]MCPNoEnvServerProjection, 0, len(config.servers))
	rejections := make([]MCPProjectionRejection, 0)
	for _, serverID := range sortedCodexMCPServerIDs(config.servers) {
		contentPath := CodexProjectMCPContentPath(serverID)
		if err := validateCodexMCPServerID(serverID); err != nil {
			rejections = append(rejections, mcpProjectionRejection(contentPath, err))
			continue
		}
		entry, err := decodeCodexProjectMCPServerEntryValue(config.servers[serverID], serverID)
		if err != nil {
			rejections = append(rejections, mcpProjectionRejection(contentPath, err))
			continue
		}
		projections = append(projections, MCPNoEnvServerProjection{
			ServerID:        serverID,
			Command:         entry.Command,
			Args:            append([]string(nil), entry.Args...),
			AdapterContract: aggregate.CodexProjectMCPStdioCommandV1,
		})
	}
	return projections, rejections, nil
}

func ExtractCodexGlobalMCPServerProjections(existing []byte) ([]CodexGlobalMCPServerProjection, []MCPProjectionRejection, error) {
	config, err := decodeCodexProjectMCPConfig(existing)
	if err != nil {
		return nil, nil, err
	}
	projections := make([]CodexGlobalMCPServerProjection, 0, len(config.servers))
	rejections := make([]MCPProjectionRejection, 0)
	for _, serverID := range sortedCodexMCPServerIDs(config.servers) {
		contentPath := CodexGlobalMCPContentPath(serverID)
		if err := validateCodexMCPServerID(serverID); err != nil {
			rejections = append(rejections, mcpProjectionRejection(contentPath, err))
			continue
		}
		entry, err := decodeCodexGlobalMCPServerEntryValue(config.servers[serverID], serverID)
		if err != nil {
			rejections = append(rejections, mcpProjectionRejection(contentPath, err))
			continue
		}
		projections = append(projections, CodexGlobalMCPServerProjection{
			ServerID:        serverID,
			Command:         entry.Command,
			Args:            append([]string(nil), entry.Args...),
			EnvVars:         append([]string(nil), entry.EnvVars...),
			AdapterContract: aggregate.CodexGlobalMCPStdioEnvVarsV1,
		})
	}
	return projections, rejections, nil
}

func extractCodexProjectMCPServerProjectionBytes(existing []byte, serverID string) ([]byte, bool, error) {
	entry, present, err := ExtractCodexProjectMCPServerProjection(existing, serverID)
	if err != nil || !present {
		return nil, present, err
	}
	content, err := encodeCodexProjectMCPServerEntry(entry, serverID)
	if err != nil {
		return nil, false, err
	}
	return content, true, nil
}

func extractCodexGlobalMCPServerProjectionBytes(existing []byte, serverID string) ([]byte, bool, error) {
	entry, present, err := ExtractCodexGlobalMCPServerProjection(existing, serverID)
	if err != nil || !present {
		return nil, present, err
	}
	content, err := encodeCodexGlobalMCPServerEntry(entry, serverID)
	if err != nil {
		return nil, false, err
	}
	return content, true, nil
}

func codexProjectMCPServerEntryPresent(existing []byte, serverID string) (bool, error) {
	if err := validateCodexMCPServerID(serverID); err != nil {
		return false, err
	}
	config, err := decodeCodexProjectMCPConfig(existing)
	if err != nil {
		return false, err
	}
	_, ok := config.servers[serverID]
	return ok, nil
}

func codexGlobalMCPServerEntryPresent(existing []byte, serverID string) (bool, error) {
	return codexProjectMCPServerEntryPresent(existing, serverID)
}

func codexProjectMCPServersParentPresent(existing []byte) (bool, error) {
	config, err := decodeCodexProjectMCPConfig(existing)
	if err != nil {
		return false, err
	}
	_, ok := config.top[codexProjectMCPManagedField]
	return ok, nil
}

func codexGlobalMCPServersParentPresent(existing []byte) (bool, error) {
	return codexProjectMCPServersParentPresent(existing)
}

type codexProjectMCPConfig struct {
	top     map[string]any
	servers map[string]any
}

const maximumCodexMCPServerIDBytes = tomlstrict.MaximumKeyBytes - len(codexProjectMCPManagedField)

func admitCodexTOML(content []byte) error {
	if err := validateMCPDocumentSize(content); err != nil {
		return err
	}
	return tomlstrict.Admit(context.Background(), content, tomlstrict.StandardLimits())
}

func decodeCodexTOMLMap(content []byte) (map[string]any, error) {
	if err := admitCodexTOML(content); err != nil {
		return nil, err
	}
	var decoded map[string]any
	if _, err := tomlstrict.DecodeAdmitted(context.Background(), content, &decoded); err != nil {
		return nil, err
	}
	return decoded, nil
}

func canonicalCodexTOMLError(subject string, operation string, err error) error {
	return newMCPProjectionError(
		MCPProjectionReasonCanonicalInvalid,
		subject,
		fmt.Sprintf("%s: %v", operation, err),
	)
}

func encodeCodexTOMLValue(value any, subject string, operation string) ([]byte, error) {
	content, err := toml.Marshal(value)
	if err != nil {
		return nil, canonicalCodexTOMLError(subject, operation, err)
	}
	if err := admitCodexTOML(content); err != nil {
		return nil, canonicalCodexTOMLError(subject, operation, err)
	}
	return content, nil
}

func validateCodexMCPServerID(serverID string) error {
	if err := validateServerID(serverID); err != nil {
		return err
	}
	if len(serverID) > maximumCodexMCPServerIDBytes {
		return newMCPProjectionError(
			MCPProjectionReasonCanonicalInvalid,
			"/mcp_servers",
			fmt.Sprintf("server id exceeds the Codex TOML key budget of %d bytes", maximumCodexMCPServerIDBytes),
		)
	}
	return nil
}

func validateCodexMCPEntryInput(
	serverID string,
	command string,
	args []string,
	envVars []string,
) error {
	if err := validateCodexMCPServerID(serverID); err != nil {
		return err
	}
	subject := CodexProjectMCPContentPath(serverID)
	if len(args) > tomlstrict.MaximumWork || len(envVars) > tomlstrict.MaximumWork-len(args) {
		return newMCPProjectionError(
			MCPProjectionReasonCanonicalInvalid,
			subject,
			fmt.Sprintf("Codex MCP entry exceeds the TOML work budget of %d values", tomlstrict.MaximumWork),
		)
	}
	remaining := maximumDocumentBytes
	consume := func(value string) bool {
		valueBytes := int64(len(value))
		if valueBytes > remaining {
			return false
		}
		remaining -= valueBytes
		return true
	}
	if !consume(serverID) || !consume(command) {
		return newMCPProjectionError(
			MCPProjectionReasonCanonicalInvalid,
			subject,
			fmt.Sprintf("Codex MCP entry values exceed %d bytes before TOML encoding", maximumDocumentBytes),
		)
	}
	for _, value := range args {
		if !consume(value) {
			return newMCPProjectionError(
				MCPProjectionReasonCanonicalInvalid,
				subject,
				fmt.Sprintf("Codex MCP entry values exceed %d bytes before TOML encoding", maximumDocumentBytes),
			)
		}
	}
	for _, value := range envVars {
		if !consume(value) {
			return newMCPProjectionError(
				MCPProjectionReasonCanonicalInvalid,
				subject,
				fmt.Sprintf("Codex MCP entry values exceed %d bytes before TOML encoding", maximumDocumentBytes),
			)
		}
	}
	return nil
}

func encodeCodexMCPServerEntry(
	serverID string,
	command string,
	args []string,
	envVars []string,
	canonicalValue any,
	hostValue map[string]any,
) ([]byte, error) {
	if err := validateCodexMCPEntryInput(serverID, command, args, envVars); err != nil {
		return nil, err
	}
	subject := CodexProjectMCPContentPath(serverID)
	canonical, err := encodeCodexTOMLValue(canonicalValue, subject, "encode Codex MCP canonical entry")
	if err != nil {
		return nil, err
	}
	_, err = encodeCodexTOMLValue(
		map[string]any{
			codexProjectMCPManagedField: map[string]any{serverID: hostValue},
		},
		subject,
		"render Codex MCP canonical entry",
	)
	if err != nil {
		return nil, err
	}
	return canonical, nil
}

// decodeCodexProjectMCPConfig stays separate from the JSON aggregate helper:
// Codex uses TOML table semantics and entry maps, while the shared helper owns
// only JSON object aggregates over raw entry bytes.
func decodeCodexProjectMCPConfig(content []byte) (codexProjectMCPConfig, error) {
	config := codexProjectMCPConfig{
		top:     make(map[string]any),
		servers: make(map[string]any),
	}
	if content == nil {
		return config, nil
	}
	if len(bytes.TrimSpace(content)) == 0 {
		return codexProjectMCPConfig{}, newMCPProjectionError(
			MCPProjectionReasonConfigMalformed,
			aggregate.CodexProjectMCPConfigPath,
			"Codex MCP config TOML is empty",
		)
	}
	top, err := decodeCodexTOMLMap(content)
	if err != nil {
		return codexProjectMCPConfig{}, newMCPProjectionError(
			MCPProjectionReasonConfigMalformed,
			aggregate.CodexProjectMCPConfigPath,
			fmt.Sprintf("decode Codex MCP config TOML: %v", err),
		)
	}
	config.top = top
	rawServers, ok := config.top[codexProjectMCPManagedField]
	if !ok {
		return config, nil
	}
	servers, ok := rawServers.(map[string]any)
	if !ok {
		return codexProjectMCPConfig{}, newMCPProjectionError(
			MCPProjectionReasonProjectionEquivalenceUndefined,
			"/mcp_servers",
			"projection table is not a TOML table",
		)
	}
	config.servers = servers
	return config, nil
}

func (config codexProjectMCPConfig) encode() ([]byte, error) {
	content, _, err := config.encodePreservingMCPParent(true)
	return content, err
}

func (config codexProjectMCPConfig) encodePreservingMCPParent(parentExistedBefore bool) ([]byte, bool, error) {
	if len(config.servers) == 0 && !parentExistedBefore {
		delete(config.top, codexProjectMCPManagedField)
		if len(config.top) == 0 {
			return nil, false, nil
		}
		content, err := encodeCodexTOMLValue(config.top, "", "encode Codex MCP host document")
		return content, true, err
	}
	config.top[codexProjectMCPManagedField] = orderedCodexMCPServers(config.servers)
	content, err := encodeCodexTOMLValue(config.top, "", "encode Codex MCP host document")
	return content, true, err
}

func decodeCodexProjectMCPServerEntry(content []byte, serverID string) (CodexProjectMCPServerEntry, error) {
	if err := validateCodexMCPServerID(serverID); err != nil {
		return CodexProjectMCPServerEntry{}, err
	}
	if len(bytes.TrimSpace(content)) == 0 {
		return CodexProjectMCPServerEntry{}, newMCPProjectionError(
			MCPProjectionReasonCanonicalInvalid,
			CodexProjectMCPContentPath(serverID),
			"Codex MCP canonical entry TOML is empty",
		)
	}
	raw, err := decodeCodexTOMLMap(content)
	if err != nil {
		return CodexProjectMCPServerEntry{}, canonicalCodexTOMLError(
			CodexProjectMCPContentPath(serverID),
			"decode Codex MCP canonical entry TOML",
			err,
		)
	}
	entry, err := decodeCodexProjectMCPServerEntryValue(raw, serverID)
	if err != nil {
		return CodexProjectMCPServerEntry{}, canonicalCodexTOMLError(
			CodexProjectMCPContentPath(serverID),
			"validate Codex MCP canonical entry",
			err,
		)
	}
	canonical, err := encodeCodexProjectMCPServerEntry(entry, serverID)
	if err != nil {
		return CodexProjectMCPServerEntry{}, err
	}
	if !bytes.Equal(content, canonical) {
		return CodexProjectMCPServerEntry{}, newMCPProjectionError(
			MCPProjectionReasonCanonicalInvalid,
			CodexProjectMCPContentPath(serverID),
			"canonical Codex MCP entry bytes do not match the codec encoding",
		)
	}
	return entry, nil
}

func decodeCodexProjectMCPServerEntryValue(value any, serverID string) (CodexProjectMCPServerEntry, error) {
	subject := CodexProjectMCPContentPath(serverID)
	object, ok := value.(map[string]any)
	if !ok || object == nil {
		return CodexProjectMCPServerEntry{}, newMCPProjectionError(
			MCPProjectionReasonProjectionEquivalenceUndefined,
			subject,
			"projection object is not a TOML table",
		)
	}
	for _, key := range sortedCodexMCPObjectKeys(object) {
		switch key {
		case "command", "args":
		default:
			return CodexProjectMCPServerEntry{}, newMCPProjectionError(
				MCPProjectionReasonUnsupportedManagedField,
				subject+"/"+key,
				"unsupported Codex MCP managed field",
			)
		}
	}
	command, args, err := decodeCodexMCPCommandArgs(object, subject)
	if err != nil {
		return CodexProjectMCPServerEntry{}, err
	}
	return CodexProjectMCPServerEntry{
		Command: command,
		Args:    args,
	}, nil
}

func decodeCodexMCPCommandArgs(object map[string]any, subject string) (string, []string, error) {
	command, ok := object["command"].(string)
	if !ok {
		return "", nil, newMCPProjectionError(
			MCPProjectionReasonProjectionEquivalenceUndefined,
			subject+"/command",
			"command is required and must be a string",
		)
	}
	if err := validateMCPCommand(command); err != nil {
		return "", nil, err
	}

	args, err := codexProjectMCPArgs(object["args"], subject+"/args")
	if err != nil {
		return "", nil, err
	}
	return command, args, nil
}

func codexProjectMCPArgs(value any, subject string) ([]string, error) {
	if value == nil {
		return []string{}, nil
	}
	if args, ok := value.([]string); ok {
		return append([]string(nil), args...), nil
	}
	rawArgs, ok := value.([]any)
	if !ok {
		return nil, newMCPProjectionError(
			MCPProjectionReasonProjectionEquivalenceUndefined,
			subject,
			"args must be an array of strings",
		)
	}
	args := make([]string, 0, len(rawArgs))
	for index, rawArg := range rawArgs {
		arg, ok := rawArg.(string)
		if !ok {
			return nil, newMCPProjectionError(
				MCPProjectionReasonProjectionEquivalenceUndefined,
				fmt.Sprintf("%s[%d]", subject, index),
				"arg must be a string",
			)
		}
		args = append(args, arg)
	}
	return args, nil
}

func encodeCodexProjectMCPServerEntry(entry CodexProjectMCPServerEntry, serverID string) ([]byte, error) {
	canonical := CodexProjectMCPServerEntry{
		Command: entry.Command,
		Args:    append([]string(nil), entry.Args...),
	}
	return encodeCodexMCPServerEntry(
		serverID,
		canonical.Command,
		canonical.Args,
		nil,
		canonical,
		codexProjectMCPServerEntryMap(canonical),
	)
}

func codexProjectMCPServerEntryMap(entry CodexProjectMCPServerEntry) map[string]any {
	return map[string]any{
		"command": entry.Command,
		"args":    append([]string(nil), entry.Args...),
	}
}

func sortedCodexMCPServerIDs(servers map[string]any) []string {
	serverIDs := make([]string, 0, len(servers))
	for serverID := range servers {
		serverIDs = append(serverIDs, serverID)
	}
	sort.Strings(serverIDs)
	return serverIDs
}

func sortedCodexMCPObjectKeys(object map[string]any) []string {
	keys := make([]string, 0, len(object))
	for key := range object {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func orderedCodexMCPServers(servers map[string]any) map[string]any {
	ordered := make(map[string]any, len(servers))
	for _, serverID := range sortedCodexMCPServerIDs(servers) {
		ordered[serverID] = servers[serverID]
	}
	return ordered
}
