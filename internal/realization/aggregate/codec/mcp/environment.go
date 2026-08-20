package mcpcodec

import (
	"bytes"
	"fmt"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
	desiredmcp "github.com/isty2e/daem/internal/desired/mcp"
)

func canonicalMCPBindingEnv(values map[string]desiredmcp.EnvReference) (map[string]string, error) {
	env := make(map[string]string, len(values))
	for name, reference := range values {
		fromEnv := strings.TrimSpace(reference.FromEnv())
		if fromEnv == "" {
			return nil, fmt.Errorf("env.%s.from_env: required", name)
		}
		env[name] = "${" + fromEnv + "}"
	}
	return env, nil
}

func canonicalOpenCodeMCPBindingEnvironment(values map[string]desiredmcp.EnvReference) (map[string]string, error) {
	environment := make(map[string]string, len(values))
	for name, reference := range values {
		fromEnv := strings.TrimSpace(reference.FromEnv())
		if fromEnv == "" {
			return nil, fmt.Errorf("env.%s.from_env: required", name)
		}
		environment[name] = "{env:" + fromEnv + "}"
	}
	return environment, nil
}

func canonicalMCPEnv(values map[string]string) (map[string]string, error) {
	env := make(map[string]string, len(values))
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if err := validateEnvName(key, "env."+key); err != nil {
			return nil, err
		}
		value := values[key]
		if err := validateHostEnvReference(value, "env."+key); err != nil {
			return nil, err
		}
		env[key] = value
	}
	return env, nil
}

func canonicalOpenCodeMCPEnvironment(values map[string]string) (map[string]string, error) {
	environment := make(map[string]string, len(values))
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if err := validateEnvName(key, "environment."+key); err != nil {
			return nil, err
		}
		value := values[key]
		if err := validateOpenCodeHostEnvReference(value, "environment."+key); err != nil {
			return nil, err
		}
		environment[key] = value
	}
	return environment, nil
}

func canonicalSameNameMCPEnvironmentNames(values []string, subject string) ([]string, error) {
	seen := make(map[string]struct{}, len(values))
	for index, value := range values {
		if err := validateEnvName(value, fmt.Sprintf("%s[%d]", subject, index)); err != nil {
			return nil, err
		}
		seen[value] = struct{}{}
	}
	envVars := make([]string, 0, len(seen))
	for value := range seen {
		envVars = append(envVars, value)
	}
	sort.Strings(envVars)
	return envVars, nil
}

func validateEnvName(value string, subject string) error {
	if value == "" || value[0] >= '0' && value[0] <= '9' {
		return newMCPProjectionError(MCPProjectionReasonProjectionEquivalenceUndefined, subject, "env name is invalid")
	}
	for index := 0; index < len(value); index++ {
		character := value[index]
		if (character >= 'A' && character <= 'Z') ||
			(character >= 'a' && character <= 'z') ||
			(character >= '0' && character <= '9') ||
			character == '_' {
			continue
		}
		return newMCPProjectionError(MCPProjectionReasonProjectionEquivalenceUndefined, subject, "env name is invalid")
	}
	return nil
}

func validateHostEnvReference(value string, subject string) error {
	if !strings.HasPrefix(value, "${") || !strings.HasSuffix(value, "}") {
		return newMCPProjectionError(MCPProjectionReasonSecretLiteralForbidden, subject, "env value must be a host env reference")
	}
	name := strings.TrimSuffix(strings.TrimPrefix(value, "${"), "}")
	if err := validateEnvName(name, subject); err != nil {
		return newMCPProjectionError(MCPProjectionReasonSecretLiteralForbidden, subject, "env value must reference a valid host env name")
	}
	return nil
}

func validateOpenCodeHostEnvReference(value string, subject string) error {
	if !strings.HasPrefix(value, "{env:") || !strings.HasSuffix(value, "}") {
		return newMCPProjectionError(
			MCPProjectionReasonSecretLiteralForbidden,
			subject,
			"environment value must be an OpenCode host env reference",
		)
	}
	name := strings.TrimSuffix(strings.TrimPrefix(value, "{env:"), "}")
	if err := validateEnvName(name, subject); err != nil {
		return newMCPProjectionError(
			MCPProjectionReasonSecretLiteralForbidden,
			subject,
			"environment value must reference a valid host env name",
		)
	}
	return nil
}

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
	raw, err := decodeCodexTOMLMap(content)
	if err != nil {
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
	return canonicalSameNameMCPEnvironmentNames(names, subject)
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
