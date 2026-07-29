package host

import (
	"bytes"
	"encoding/json"
	"fmt"

	burnttoml "github.com/BurntSushi/toml"
	"github.com/isty2e/daem/internal/encoding/jsonstrict"
	"github.com/tailscale/hujson"
)

const maximumConfigDepth = 32

type importKind string

const (
	importCursor        importKind = "cursor"
	importClaudeCode    importKind = "claude-code"
	importClaudeDesktop importKind = "claude-desktop"
	importCodex         importKind = "codex"
	importOpenCode      importKind = "opencode"
	importWindsurf      importKind = "windsurf"
	importVSCode        importKind = "vscode"
)

var orderedImportKinds = [...]importKind{
	importCursor,
	importClaudeCode,
	importClaudeDesktop,
	importCodex,
	importOpenCode,
	importWindsurf,
	importVSCode,
}

type normalDocument struct {
	serverNames         map[string]struct{}
	imports             []importKind
	hostConfigDiscovery string
}

func decodeNormalDocument(content []byte) (normalDocument, error) {
	root, err := decodeJSONCObject(content, "Pi MCP config")
	if err != nil {
		return normalDocument{}, err
	}
	result := normalDocument{serverNames: make(map[string]struct{})}
	if root == nil {
		return result, nil
	}
	servers, err := nullishObjectField(root, "mcpServers", "mcp-servers")
	if err != nil {
		return normalDocument{}, err
	}
	for name := range servers {
		result.serverNames[name] = struct{}{}
	}

	if rawImports, present := root["imports"]; present {
		values, ok := rawImports.([]any)
		if !ok {
			return normalDocument{}, fmt.Errorf("imports must be an array")
		}
		seen := make(map[importKind]struct{}, len(values))
		for index, raw := range values {
			value, ok := raw.(string)
			if !ok {
				return normalDocument{}, fmt.Errorf("imports[%d] must be a string", index)
			}
			kind := importKind(value)
			if !knownImportKind(kind) {
				return normalDocument{}, fmt.Errorf("imports[%d] kind %q is unsupported", index, value)
			}
			if _, duplicate := seen[kind]; duplicate {
				continue
			}
			seen[kind] = struct{}{}
			result.imports = append(result.imports, kind)
		}
	}
	if rawSettings, present := root["settings"]; present {
		settings, ok := rawSettings.(map[string]any)
		if !ok && rawSettings != nil {
			return normalDocument{}, fmt.Errorf("settings must be an object")
		}
		if settings != nil {
			if rawMode, present := settings["hostConfigDiscovery"]; present {
				mode, ok := rawMode.(string)
				if !ok || (mode != "off" && mode != "prompt" && mode != "on") {
					return normalDocument{}, fmt.Errorf(
						"settings.hostConfigDiscovery must be off, prompt, or on",
					)
				}
				result.hostConfigDiscovery = mode
			}
		}
	}
	return result, nil
}

func decodeJSONImportServerNames(
	content []byte,
	kind importKind,
) (map[string]struct{}, error) {
	root, err := decodeJSONCObject(content, string(kind)+" MCP import")
	if err != nil {
		return nil, err
	}
	if root == nil {
		return map[string]struct{}{}, nil
	}
	var raw any
	switch kind {
	case importClaudeCode, importClaudeDesktop:
		raw = root["mcpServers"]
	case importCursor, importWindsurf, importVSCode:
		raw = nullishField(root, "mcpServers", "mcp-servers")
	default:
		return nil, fmt.Errorf("JSON import kind %q is unsupported", kind)
	}
	servers, err := requiredNullableObject(raw, "imported MCP server table")
	if err != nil {
		return nil, err
	}
	result := make(map[string]struct{}, len(servers))
	for name := range servers {
		result[name] = struct{}{}
	}
	return result, nil
}

func decodeCodexImportServerNames(
	content []byte,
	tomlDocument bool,
) (map[string]struct{}, error) {
	var root map[string]any
	if tomlDocument {
		if _, err := burnttoml.Decode(string(content), &root); err != nil {
			return nil, err
		}
	} else {
		var err error
		root, err = decodeJSONCObject(content, "Codex MCP import")
		if err != nil {
			return nil, err
		}
	}
	if root == nil {
		return map[string]struct{}{}, nil
	}
	servers, err := requiredNullableObject(
		nullishField(root, "mcp_servers", "mcpServers"),
		"Codex MCP server table",
	)
	if err != nil {
		return nil, err
	}
	result := make(map[string]struct{}, len(servers))
	for name := range servers {
		result[name] = struct{}{}
	}
	return result, nil
}

func decodeOpenCodeConfig(content []byte) (map[string]map[string]any, error) {
	root, err := decodeJSONCObject(content, "OpenCode MCP import")
	if err != nil {
		return nil, err
	}
	if root == nil {
		return map[string]map[string]any{}, nil
	}
	servers, err := requiredNullableObject(root["mcp"], "OpenCode MCP server table")
	if err != nil {
		return nil, err
	}
	result := make(map[string]map[string]any, len(servers))
	for name, raw := range servers {
		entry, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		result[name] = entry
	}
	return result, nil
}

func openCodeServerNames(entries map[string]map[string]any) map[string]struct{} {
	result := make(map[string]struct{})
	for name, entry := range entries {
		if enabled, ok := entry["enabled"].(bool); ok && !enabled {
			continue
		}
		switch entry["type"] {
		case "local":
			command, ok := entry["command"].([]any)
			if !ok || len(command) == 0 {
				continue
			}
			valid := true
			for _, part := range command {
				if _, ok := part.(string); !ok {
					valid = false
					break
				}
			}
			if valid {
				result[name] = struct{}{}
			}
		case "remote":
			if _, ok := entry["url"].(string); ok {
				result[name] = struct{}{}
			}
		}
	}
	return result
}

func mergeOpenCodeEntries(
	base map[string]map[string]any,
	next map[string]map[string]any,
) {
	for name, nextEntry := range next {
		merged := make(map[string]any)
		for key, value := range base[name] {
			merged[key] = value
		}
		if nextType, present := nextEntry["type"].(string); present && nextType != merged["type"] {
			delete(merged, "command")
			delete(merged, "url")
		}
		for key, value := range nextEntry {
			merged[key] = value
		}
		base[name] = merged
	}
}

func decodeJSONCObject(content []byte, label string) (map[string]any, error) {
	standardized, err := hujson.Standardize(append([]byte(nil), content...))
	if err != nil {
		return nil, err
	}
	if err := jsonstrict.Validate(standardized, label, maximumConfigDepth); err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(standardized))
	decoder.UseNumber()
	var root any
	if err := decoder.Decode(&root); err != nil {
		return nil, err
	}
	object, ok := root.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s root must be an object", label)
	}
	return object, nil
}

func nullishObjectField(
	root map[string]any,
	primary string,
	fallback string,
) (map[string]any, error) {
	return requiredNullableObject(
		nullishField(root, primary, fallback),
		primary+" or "+fallback,
	)
}

func nullishField(root map[string]any, primary string, fallback string) any {
	value, present := root[primary]
	if !present || value == nil {
		return root[fallback]
	}
	return value
}

func requiredNullableObject(value any, label string) (map[string]any, error) {
	if value == nil {
		return nil, nil
	}
	object, ok := value.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s must be an object or null", label)
	}
	return object, nil
}

func knownImportKind(kind importKind) bool {
	for _, admitted := range orderedImportKinds {
		if kind == admitted {
			return true
		}
	}
	return false
}
