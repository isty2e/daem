package mcpcodec

import (
	"bytes"
	"fmt"

	"github.com/BurntSushi/toml"
)

func decodeCodexGlobalMCPServerEntry(content []byte, serverID string) (CodexGlobalMCPServerEntry, error) {
	if err := validateServerID(serverID); err != nil {
		return CodexGlobalMCPServerEntry{}, err
	}
	if len(bytes.TrimSpace(content)) == 0 {
		return CodexGlobalMCPServerEntry{}, newMCPProjectionError(
			MCPProjectionReasonConfigMalformed,
			CodexGlobalMCPContentPath(serverID),
			"Codex MCP canonical entry TOML is empty",
		)
	}
	var raw map[string]any
	if _, err := toml.Decode(string(content), &raw); err != nil {
		return CodexGlobalMCPServerEntry{}, newMCPProjectionError(
			MCPProjectionReasonConfigMalformed,
			CodexGlobalMCPContentPath(serverID),
			fmt.Sprintf("decode Codex MCP canonical entry TOML: %v", err),
		)
	}
	return decodeCodexGlobalMCPServerEntryValue(raw, serverID)
}

func decodeCodexGlobalMCPServerEntryValue(value any, serverID string) (CodexGlobalMCPServerEntry, error) {
	subject := CodexGlobalMCPContentPath(serverID)
	object, ok := value.(map[string]any)
	if !ok || object == nil {
		return CodexGlobalMCPServerEntry{}, newMCPProjectionError(
			MCPProjectionReasonProjectionEquivalenceUndefined,
			subject,
			"projection object is not a TOML table",
		)
	}
	for key := range object {
		switch key {
		case "command", "args", "env_vars":
		case "env":
			return CodexGlobalMCPServerEntry{}, newMCPProjectionError(
				MCPProjectionReasonSecretLiteralForbidden,
				subject+"/env",
				"literal environment values are not an admitted Codex global MCP projection",
			)
		default:
			return CodexGlobalMCPServerEntry{}, newMCPProjectionError(
				MCPProjectionReasonUnsupportedManagedField,
				subject+"/"+key,
				"unsupported Codex global MCP managed field",
			)
		}
	}
	command, args, err := decodeCodexMCPCommandArgs(object, subject)
	if err != nil {
		return CodexGlobalMCPServerEntry{}, err
	}
	envVars, err := codexGlobalMCPEnvVars(object["env_vars"], subject+"/env_vars")
	if err != nil {
		return CodexGlobalMCPServerEntry{}, err
	}
	return CodexGlobalMCPServerEntry{
		Command: command,
		Args:    args,
		EnvVars: envVars,
	}, nil
}

func codexGlobalMCPEnvVars(value any, subject string) ([]string, error) {
	if value == nil {
		return []string{}, nil
	}

	var items []any
	switch typed := value.(type) {
	case []string:
		items = make([]any, len(typed))
		for index, item := range typed {
			items[index] = item
		}
	case []map[string]any:
		items = make([]any, len(typed))
		for index, item := range typed {
			items[index] = item
		}
	case []any:
		items = typed
	default:
		return nil, newMCPProjectionError(
			MCPProjectionReasonProjectionEquivalenceUndefined,
			subject,
			"env_vars must be an array of names or local name objects",
		)
	}

	names := make([]string, 0, len(items))
	for index, item := range items {
		itemSubject := fmt.Sprintf("%s[%d]", subject, index)
		name, err := codexGlobalMCPEnvVarName(item, itemSubject)
		if err != nil {
			return nil, err
		}
		names = append(names, name)
	}
	return canonicalCodexGlobalMCPEnvVars(names, subject)
}

func codexGlobalMCPEnvVarName(value any, subject string) (string, error) {
	if name, ok := value.(string); ok {
		if err := validateEnvName(name, subject); err != nil {
			return "", err
		}
		return name, nil
	}
	object, ok := value.(map[string]any)
	if !ok || object == nil {
		return "", newMCPProjectionError(
			MCPProjectionReasonProjectionEquivalenceUndefined,
			subject,
			"env var must be a name or local name object",
		)
	}
	for key := range object {
		switch key {
		case "name", "source":
		default:
			return "", newMCPProjectionError(
				MCPProjectionReasonUnsupportedManagedField,
				subject+"/"+key,
				"unsupported Codex MCP env var field",
			)
		}
	}
	name, ok := object["name"].(string)
	if !ok {
		return "", newMCPProjectionError(
			MCPProjectionReasonProjectionEquivalenceUndefined,
			subject+"/name",
			"env var name is required and must be a string",
		)
	}
	if err := validateEnvName(name, subject+"/name"); err != nil {
		return "", err
	}
	if rawSource, present := object["source"]; present {
		source, ok := rawSource.(string)
		if !ok || source != "local" {
			return "", newMCPProjectionError(
				MCPProjectionReasonProjectionEquivalenceUndefined,
				subject+"/source",
				"only local Codex MCP environment sources are admitted",
			)
		}
	}
	return name, nil
}

func encodeCodexGlobalMCPServerEntry(entry CodexGlobalMCPServerEntry) ([]byte, error) {
	content, err := toml.Marshal(CodexGlobalMCPServerEntry{
		Command: entry.Command,
		Args:    append([]string(nil), entry.Args...),
		EnvVars: append([]string(nil), entry.EnvVars...),
	})
	if err != nil {
		return nil, err
	}
	return content, nil
}

func codexGlobalMCPServerEntryMap(entry CodexGlobalMCPServerEntry) map[string]any {
	result := map[string]any{
		"command": entry.Command,
		"args":    append([]string(nil), entry.Args...),
	}
	if len(entry.EnvVars) != 0 {
		result["env_vars"] = append([]string(nil), entry.EnvVars...)
	}
	return result
}
