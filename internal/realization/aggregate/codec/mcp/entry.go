package mcpcodec

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

var allowedClaudeProjectMCPManagedFields = map[string]struct{}{
	"type":    {},
	"command": {},
	"args":    {},
	"env":     {},
}

var allowedClaudeGlobalMCPManagedFields = map[string]struct{}{
	"type":    {},
	"command": {},
	"args":    {},
	"env":     {},
}

var allowedAntigravityGlobalMCPManagedFields = map[string]struct{}{
	"command": {},
	"args":    {},
}

var allowedOpenCodeProjectMCPManagedFields = map[string]struct{}{
	"type":    {},
	"command": {},
}

var allowedOpenCodeGlobalMCPManagedFields = map[string]struct{}{
	"type":        {},
	"command":     {},
	"environment": {},
}

func decodeClaudeProjectMCPServerEntry(raw json.RawMessage, serverID string) (ClaudeProjectMCPServerEntry, error) {
	subject := ClaudeProjectMCPContentPath(serverID)
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil || fields == nil {
		return ClaudeProjectMCPServerEntry{}, newMCPProjectionError(
			MCPProjectionReasonProjectionEquivalenceUndefined,
			subject,
			"managed MCP server entry is not a JSON object",
		)
	}

	fieldNames := make([]string, 0, len(fields))
	for fieldName := range fields {
		fieldNames = append(fieldNames, fieldName)
	}
	sort.Strings(fieldNames)
	for _, fieldName := range fieldNames {
		if _, ok := allowedClaudeProjectMCPManagedFields[fieldName]; !ok {
			return ClaudeProjectMCPServerEntry{}, newMCPProjectionError(
				MCPProjectionReasonUnsupportedManagedField,
				subject,
				fmt.Sprintf("unsupported managed MCP server field %q", fieldName),
			)
		}
	}

	entry := ClaudeProjectMCPServerEntry{
		Args: []string{},
		Env:  map[string]string{},
	}
	if err := decodeRequiredString(fields, "type", subject, &entry.Type); err != nil {
		return ClaudeProjectMCPServerEntry{}, err
	}
	if entry.Type != claudeProjectMCPTransportStdio {
		return ClaudeProjectMCPServerEntry{}, newMCPProjectionError(
			MCPProjectionReasonUnsupportedTransport,
			subject,
			fmt.Sprintf("unsupported MCP transport %q", entry.Type),
		)
	}
	if err := decodeRequiredString(fields, "command", subject, &entry.Command); err != nil {
		return ClaudeProjectMCPServerEntry{}, err
	}
	if err := validateMCPCommand(entry.Command); err != nil {
		return ClaudeProjectMCPServerEntry{}, err
	}
	if rawArgs, ok := fields["args"]; ok {
		if err := json.Unmarshal(rawArgs, &entry.Args); err != nil || entry.Args == nil {
			return ClaudeProjectMCPServerEntry{}, newMCPProjectionError(
				MCPProjectionReasonProjectionEquivalenceUndefined,
				subject,
				"args must be a JSON string array",
			)
		}
	}
	if rawEnv, ok := fields["env"]; ok {
		if err := json.Unmarshal(rawEnv, &entry.Env); err != nil || entry.Env == nil {
			return ClaudeProjectMCPServerEntry{}, newMCPProjectionError(
				MCPProjectionReasonProjectionEquivalenceUndefined,
				subject,
				"env must be a JSON string map",
			)
		}
		if _, err := canonicalMCPEnv(entry.Env); err != nil {
			return ClaudeProjectMCPServerEntry{}, err
		}
	}
	return entry, nil
}

func decodeClaudeGlobalMCPServerEntry(raw json.RawMessage, serverID string) (ClaudeGlobalMCPServerEntry, error) {
	subject := ClaudeGlobalMCPContentPath(serverID)
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil || fields == nil {
		return ClaudeGlobalMCPServerEntry{}, newMCPProjectionError(
			MCPProjectionReasonProjectionEquivalenceUndefined,
			subject,
			"managed MCP server entry is not a JSON object",
		)
	}

	fieldNames := make([]string, 0, len(fields))
	for fieldName := range fields {
		fieldNames = append(fieldNames, fieldName)
	}
	sort.Strings(fieldNames)

	entry := ClaudeGlobalMCPServerEntry{
		Args: []string{},
		Env:  map[string]string{},
	}
	if err := decodeRequiredString(fields, "type", subject, &entry.Type); err != nil {
		return ClaudeGlobalMCPServerEntry{}, err
	}
	if entry.Type != claudeProjectMCPTransportStdio {
		return ClaudeGlobalMCPServerEntry{}, newMCPProjectionError(
			MCPProjectionReasonUnsupportedTransport,
			subject,
			fmt.Sprintf("unsupported MCP transport %q", entry.Type),
		)
	}

	for _, fieldName := range fieldNames {
		if _, ok := allowedClaudeGlobalMCPManagedFields[fieldName]; !ok {
			return ClaudeGlobalMCPServerEntry{}, newMCPProjectionError(
				MCPProjectionReasonUnsupportedManagedField,
				subject,
				fmt.Sprintf("unsupported managed MCP server field %q", fieldName),
			)
		}
	}

	if err := decodeRequiredString(fields, "command", subject, &entry.Command); err != nil {
		return ClaudeGlobalMCPServerEntry{}, err
	}
	if err := validateMCPCommand(entry.Command); err != nil {
		return ClaudeGlobalMCPServerEntry{}, err
	}
	if rawArgs, ok := fields["args"]; ok {
		if err := json.Unmarshal(rawArgs, &entry.Args); err != nil || entry.Args == nil {
			return ClaudeGlobalMCPServerEntry{}, newMCPProjectionError(
				MCPProjectionReasonProjectionEquivalenceUndefined,
				subject,
				"args must be a JSON string array",
			)
		}
	}
	if rawEnv, ok := fields["env"]; ok {
		if err := json.Unmarshal(rawEnv, &entry.Env); err != nil || entry.Env == nil {
			return ClaudeGlobalMCPServerEntry{}, newMCPProjectionError(
				MCPProjectionReasonProjectionEquivalenceUndefined,
				subject,
				"env must be a JSON string map",
			)
		}
		if _, err := canonicalMCPEnv(entry.Env); err != nil {
			return ClaudeGlobalMCPServerEntry{}, err
		}
	}
	return entry, nil
}

func decodeAntigravityGlobalMCPServerEntry(raw json.RawMessage, serverID string) (AntigravityGlobalMCPServerEntry, error) {
	subject := AntigravityGlobalMCPContentPath(serverID)
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil || fields == nil {
		return AntigravityGlobalMCPServerEntry{}, newMCPProjectionError(
			MCPProjectionReasonProjectionEquivalenceUndefined,
			subject,
			"managed MCP server entry is not a JSON object",
		)
	}

	fieldNames := make([]string, 0, len(fields))
	for fieldName := range fields {
		fieldNames = append(fieldNames, fieldName)
	}
	sort.Strings(fieldNames)
	for _, fieldName := range fieldNames {
		if _, ok := allowedAntigravityGlobalMCPManagedFields[fieldName]; !ok {
			return AntigravityGlobalMCPServerEntry{}, newMCPProjectionError(
				MCPProjectionReasonUnsupportedManagedField,
				subject,
				fmt.Sprintf("unsupported managed MCP server field %q", fieldName),
			)
		}
	}

	entry := AntigravityGlobalMCPServerEntry{Args: []string{}}
	if err := decodeRequiredString(fields, "command", subject, &entry.Command); err != nil {
		return AntigravityGlobalMCPServerEntry{}, err
	}
	if err := validateMCPCommand(entry.Command); err != nil {
		return AntigravityGlobalMCPServerEntry{}, err
	}
	if rawArgs, ok := fields["args"]; ok {
		if err := json.Unmarshal(rawArgs, &entry.Args); err != nil || entry.Args == nil {
			return AntigravityGlobalMCPServerEntry{}, newMCPProjectionError(
				MCPProjectionReasonProjectionEquivalenceUndefined,
				subject,
				"args must be a JSON string array",
			)
		}
	}
	return entry, nil
}

func decodeOpenCodeProjectMCPServerEntry(raw json.RawMessage, serverID string) (OpenCodeProjectMCPServerEntry, error) {
	subject := OpenCodeProjectMCPContentPath(serverID)
	entry, _, err := decodeOpenCodeLocalMCPServerFields(
		raw,
		subject,
		allowedOpenCodeProjectMCPManagedFields,
	)
	return entry, err
}

func decodeOpenCodeGlobalMCPServerEntry(raw json.RawMessage, serverID string) (OpenCodeGlobalMCPServerEntry, error) {
	subject := OpenCodeGlobalMCPContentPath(serverID)
	local, fields, err := decodeOpenCodeLocalMCPServerFields(
		raw,
		subject,
		allowedOpenCodeGlobalMCPManagedFields,
	)
	if err != nil {
		return OpenCodeGlobalMCPServerEntry{}, err
	}
	entry := OpenCodeGlobalMCPServerEntry{
		Type:        local.Type,
		Command:     append([]string(nil), local.Command...),
		Environment: map[string]string{},
	}
	if rawEnvironment, ok := fields["environment"]; ok {
		if err := json.Unmarshal(rawEnvironment, &entry.Environment); err != nil || entry.Environment == nil {
			return OpenCodeGlobalMCPServerEntry{}, newMCPProjectionError(
				MCPProjectionReasonProjectionEquivalenceUndefined,
				subject,
				"environment must be a JSON string map",
			)
		}
		if _, err := canonicalOpenCodeMCPEnvironment(entry.Environment); err != nil {
			return OpenCodeGlobalMCPServerEntry{}, err
		}
	}
	return entry, nil
}

func decodeOpenCodeLocalMCPServerFields(
	raw json.RawMessage,
	subject string,
	allowedFields map[string]struct{},
) (OpenCodeProjectMCPServerEntry, map[string]json.RawMessage, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil || fields == nil {
		return OpenCodeProjectMCPServerEntry{}, nil, newMCPProjectionError(
			MCPProjectionReasonProjectionEquivalenceUndefined,
			subject,
			"managed MCP server entry is not a JSON object",
		)
	}

	fieldNames := make([]string, 0, len(fields))
	for fieldName := range fields {
		fieldNames = append(fieldNames, fieldName)
	}
	sort.Strings(fieldNames)
	for _, fieldName := range fieldNames {
		if _, ok := allowedFields[fieldName]; !ok {
			return OpenCodeProjectMCPServerEntry{}, nil, newMCPProjectionError(
				MCPProjectionReasonUnsupportedManagedField,
				subject,
				fmt.Sprintf("unsupported managed MCP server field %q", fieldName),
			)
		}
	}

	entry := OpenCodeProjectMCPServerEntry{}
	if err := decodeRequiredString(fields, "type", subject, &entry.Type); err != nil {
		return OpenCodeProjectMCPServerEntry{}, nil, err
	}
	if entry.Type != openCodeProjectMCPTypeLocal {
		return OpenCodeProjectMCPServerEntry{}, nil, newMCPProjectionError(
			MCPProjectionReasonUnsupportedTransport,
			subject,
			fmt.Sprintf("unsupported MCP type %q", entry.Type),
		)
	}
	rawCommand, ok := fields["command"]
	if !ok {
		return OpenCodeProjectMCPServerEntry{}, nil, newMCPProjectionError(
			MCPProjectionReasonProjectionEquivalenceUndefined,
			subject,
			`managed MCP server field "command" is required`,
		)
	}
	if err := json.Unmarshal(rawCommand, &entry.Command); err != nil || entry.Command == nil || len(entry.Command) == 0 {
		return OpenCodeProjectMCPServerEntry{}, nil, newMCPProjectionError(
			MCPProjectionReasonProjectionEquivalenceUndefined,
			subject,
			"command must be a non-empty JSON string array",
		)
	}
	if err := validateMCPCommand(entry.Command[0]); err != nil {
		return OpenCodeProjectMCPServerEntry{}, nil, err
	}
	return entry, fields, nil
}

func decodeRequiredString(fields map[string]json.RawMessage, fieldName string, subject string, destination *string) error {
	raw, ok := fields[fieldName]
	if !ok {
		return newMCPProjectionError(
			MCPProjectionReasonProjectionEquivalenceUndefined,
			subject,
			fmt.Sprintf("managed MCP server field %q is required", fieldName),
		)
	}
	if err := json.Unmarshal(raw, destination); err != nil || *destination == "" {
		return newMCPProjectionError(
			MCPProjectionReasonProjectionEquivalenceUndefined,
			subject,
			fmt.Sprintf("managed MCP server field %q must be a non-empty string", fieldName),
		)
	}
	return nil
}

func claudeProjectMCPRuntimeProbeLaunch(
	canonicalProjection string,
) (string, []string, map[string]string, error) {
	entry, err := decodeClaudeProjectMCPServerEntry(
		json.RawMessage(canonicalProjection),
		"runtime-probe",
	)
	if err != nil {
		return "", nil, nil, err
	}
	if entry.Type != "stdio" {
		return "", nil, nil, fmt.Errorf(
			"Claude project MCP transport %q is not admitted for launch/initialize probe",
			entry.Type,
		)
	}
	env := make(map[string]string, len(entry.Env))
	for serverEnv, value := range entry.Env {
		env[serverEnv] = strings.TrimSuffix(strings.TrimPrefix(value, "${"), "}")
	}
	return entry.Command, append([]string(nil), entry.Args...), env, nil
}

func openCodeProjectMCPRuntimeProbeLaunch(
	canonicalProjection string,
) (string, []string, map[string]string, error) {
	entry, err := decodeOpenCodeProjectMCPServerEntry(
		json.RawMessage(canonicalProjection),
		"runtime-probe",
	)
	if err != nil {
		return "", nil, nil, err
	}
	if entry.Type != "local" {
		return "", nil, nil, fmt.Errorf(
			"OpenCode project MCP type %q is not admitted for launch/initialize probe",
			entry.Type,
		)
	}
	return entry.Command[0], append([]string(nil), entry.Command[1:]...), map[string]string{}, nil
}
