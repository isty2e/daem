package claudeplugin

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"

	"github.com/isty2e/daem/internal/assurance/observe/relation"
)

const installedPluginsVersion = 2

// InstalledInventoryInput identifies the passive Claude installed-plugin
// relation source and the exact relations selected for correlation.
type InstalledInventoryInput struct {
	ConfigRoot  string
	WorkDir     string
	ProjectRoot string
	Relations   []ScopedRelation
}

type installedPluginsFile struct {
	Version int             `json:"version"`
	Plugins json.RawMessage `json:"plugins"`
}

type installedPluginRecord struct {
	Scope       HostScope `json:"scope"`
	ProjectPath string    `json:"projectPath"`
}

// ReadInstalledInventory reads Claude's version-2 installed relation file.
// Absence is fresh empty evidence; malformed or unsupported content is an
// error and must never be converted into evidence of absence.
func ReadInstalledInventory(input InstalledInventoryInput) (Inventory, error) {
	path, err := InstalledInventoryPath(input)
	if err != nil {
		return Inventory{}, err
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return NewInventory(InventorySpec{
			Availability: relation.InventorySupported,
			Freshness:    relation.EvidenceFresh,
		})
	}
	if err != nil {
		return Inventory{}, fmt.Errorf("read Claude installed plugin inventory %q: %w", path, err)
	}

	file, err := decodeInstalledPluginsFile(data)
	if err != nil {
		return Inventory{}, fmt.Errorf("decode Claude installed plugin inventory %q: %w", path, err)
	}
	if file.Version != installedPluginsVersion {
		return Inventory{}, fmt.Errorf("decode Claude installed plugin inventory %q: unsupported version %d", path, file.Version)
	}
	if len(file.Plugins) == 0 || bytes.Equal(bytes.TrimSpace(file.Plugins), []byte("null")) {
		return Inventory{}, fmt.Errorf("decode Claude installed plugin inventory %q: plugins object is required", path)
	}
	plugins, err := decodeUniqueJSONObject(file.Plugins)
	if err != nil {
		return Inventory{}, fmt.Errorf("decode Claude installed plugin inventory %q: plugins: %w", path, err)
	}

	projectRoot, err := canonicalPath(input.ProjectRoot)
	if err != nil {
		return Inventory{}, fmt.Errorf("resolve Claude inventory project root: %w", err)
	}
	relevantScopes := installedInventoryRelationScopes(input.Relations)
	pluginKeys := make([]string, 0, len(plugins))
	for pluginKey := range plugins {
		if len(relevantScopes) != 0 {
			if _, relevant := relevantScopes[pluginKey]; !relevant {
				continue
			}
		}
		pluginKeys = append(pluginKeys, pluginKey)
	}
	sort.Strings(pluginKeys)
	rows := make([]Row, 0)
	for _, pluginKey := range pluginKeys {
		rawRecords := plugins[pluginKey]
		if len(rawRecords) == 0 || bytes.Equal(bytes.TrimSpace(rawRecords), []byte("null")) {
			return Inventory{}, fmt.Errorf("decode Claude installed plugin inventory %q: plugin %q rows must be an array", path, pluginKey)
		}
		if err := rejectDuplicateJSONKeys(rawRecords); err != nil {
			return Inventory{}, fmt.Errorf("decode Claude installed plugin inventory %q: plugin %q rows: %w", path, pluginKey, err)
		}
		var records []installedPluginRecord
		if err := json.Unmarshal(rawRecords, &records); err != nil {
			return Inventory{}, fmt.Errorf("decode Claude installed plugin inventory %q: plugin %q rows: %w", path, pluginKey, err)
		}
		for index, record := range records {
			scope, err := installedRecordScope(record.Scope)
			if err != nil {
				return Inventory{}, fmt.Errorf("decode Claude installed plugin inventory %q: plugin %q row %d: %w", path, pluginKey, index, err)
			}
			if scopes, bounded := relevantScopes[pluginKey]; bounded {
				if _, relevant := scopes[scope]; !relevant {
					continue
				}
			}
			if scope == HostScopeProject || scope == HostScopeLocal {
				if record.ProjectPath == "" {
					return Inventory{}, fmt.Errorf("decode Claude installed plugin inventory %q: plugin %q row %d projectPath: required for %s scope", path, pluginKey, index, scope)
				}
				if !filepath.IsAbs(record.ProjectPath) {
					return Inventory{}, fmt.Errorf("decode Claude installed plugin inventory %q: plugin %q row %d projectPath: absolute path required", path, pluginKey, index)
				}
				recordProjectRoot, err := canonicalPath(record.ProjectPath)
				if err != nil {
					return Inventory{}, fmt.Errorf("decode Claude installed plugin inventory %q: plugin %q row %d projectPath: %w", path, pluginKey, index, err)
				}
				if projectRoot == "" || !sameCanonicalPath(recordProjectRoot, projectRoot) {
					continue
				}
			}

			row, err := newSourceExactRow(pluginKey, scope)
			if err != nil {
				return Inventory{}, fmt.Errorf("decode Claude installed plugin inventory %q: plugin %q row %d: %w", path, pluginKey, index, err)
			}
			rows = append(rows, row)
		}
	}

	return NewInventory(InventorySpec{
		Availability: relation.InventorySupported,
		Freshness:    relation.EvidenceFresh,
		Rows:         rows,
	})
}

// InstalledInventoryPath returns the exact host file consumed by
// ReadInstalledInventory for mutation evidence and diagnostics.
func InstalledInventoryPath(input InstalledInventoryInput) (string, error) {
	configRoot, err := installedConfigRoot(input.ConfigRoot, input.WorkDir)
	if err != nil {
		return "", err
	}
	return filepath.Join(configRoot, "plugins", "installed_plugins.json"), nil
}

func installedConfigRoot(configRoot string, workDir string) (string, error) {
	root := configRoot
	if root == "" {
		root = os.Getenv("CLAUDE_CONFIG_DIR")
	}
	if root == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve Claude config home: %w", err)
		}
		root = filepath.Join(home, ".claude")
	}
	if filepath.IsAbs(root) {
		return filepath.Clean(root), nil
	}
	base := workDir
	if base == "" {
		var err error
		base, err = os.Getwd()
		if err != nil {
			return "", fmt.Errorf("resolve Claude config working directory: %w", err)
		}
	}
	absolute, err := filepath.Abs(filepath.Join(base, root))
	if err != nil {
		return "", fmt.Errorf("resolve Claude config root %q from %q: %w", root, base, err)
	}
	return filepath.Clean(absolute), nil
}

func decodeInstalledPluginsFile(data []byte) (installedPluginsFile, error) {
	fields, err := decodeUniqueJSONObject(data)
	if err != nil {
		return installedPluginsFile{}, err
	}
	var file installedPluginsFile
	if version, ok := fields["version"]; ok {
		if err := json.Unmarshal(version, &file.Version); err != nil {
			return installedPluginsFile{}, fmt.Errorf("version: %w", err)
		}
	}
	if plugins, ok := fields["plugins"]; ok {
		file.Plugins = plugins
	}
	return file, nil
}

func decodeUniqueJSONObject(data []byte) (map[string]json.RawMessage, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	opening, err := decoder.Token()
	if err != nil {
		return nil, err
	}
	if opening != json.Delim('{') {
		return nil, fmt.Errorf("JSON object is required")
	}
	result := make(map[string]json.RawMessage)
	for decoder.More() {
		keyToken, err := decoder.Token()
		if err != nil {
			return nil, err
		}
		key, ok := keyToken.(string)
		if !ok {
			return nil, fmt.Errorf("JSON object key is not a string")
		}
		if _, duplicate := result[key]; duplicate {
			return nil, fmt.Errorf("duplicate JSON object key %q", key)
		}
		var raw json.RawMessage
		if err := decoder.Decode(&raw); err != nil {
			return nil, err
		}
		result[key] = raw
	}
	closing, err := decoder.Token()
	if err != nil {
		return nil, err
	}
	if closing != json.Delim('}') {
		return nil, fmt.Errorf("invalid JSON object closing token")
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return nil, fmt.Errorf("multiple JSON values are not allowed")
		}
		return nil, err
	}
	return result, nil
}

func rejectDuplicateJSONKeys(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	if err := consumeUniqueJSONValue(decoder, 0); err != nil {
		return err
	}
	if _, err := decoder.Token(); err != io.EOF {
		if err == nil {
			return fmt.Errorf("multiple JSON values are not allowed")
		}
		return err
	}
	return nil
}

func consumeUniqueJSONValue(decoder *json.Decoder, depth int) error {
	if depth > 128 {
		return fmt.Errorf("JSON nesting exceeds 128 levels")
	}
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delimiter, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delimiter {
	case '{':
		seen := make(map[string]struct{})
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return err
			}
			key, ok := keyToken.(string)
			if !ok {
				return fmt.Errorf("JSON object key is not a string")
			}
			if _, duplicate := seen[key]; duplicate {
				return fmt.Errorf("duplicate JSON object key %q", key)
			}
			seen[key] = struct{}{}
			if err := consumeUniqueJSONValue(decoder, depth+1); err != nil {
				return err
			}
		}
		closing, err := decoder.Token()
		if err != nil {
			return err
		}
		if closing != json.Delim('}') {
			return fmt.Errorf("invalid JSON object closing token")
		}
	case '[':
		for decoder.More() {
			if err := consumeUniqueJSONValue(decoder, depth+1); err != nil {
				return err
			}
		}
		closing, err := decoder.Token()
		if err != nil {
			return err
		}
		if closing != json.Delim(']') {
			return fmt.Errorf("invalid JSON array closing token")
		}
	default:
		return fmt.Errorf("unexpected JSON delimiter %q", delimiter)
	}
	return nil
}

func installedRecordScope(scope HostScope) (HostScope, error) {
	switch scope {
	case HostScopeProject, HostScopeUser, HostScopeLocal, HostScopeManaged:
		return scope, nil
	default:
		return "", fmt.Errorf("unsupported or missing scope %q", scope)
	}
}

func canonicalPath(value string) (string, error) {
	if value == "" {
		return "", nil
	}
	absolute, err := filepath.Abs(value)
	if err != nil {
		return "", err
	}
	canonical, err := filepath.EvalSymlinks(absolute)
	if err == nil {
		return filepath.Clean(canonical), nil
	}
	if !os.IsNotExist(err) {
		return "", err
	}
	return filepath.Clean(absolute), nil
}

func installedInventoryRelationScopes(relations []ScopedRelation) map[string]map[HostScope]struct{} {
	if len(relations) == 0 {
		return nil
	}
	result := make(map[string]map[HostScope]struct{}, len(relations))
	for _, relation := range relations {
		scope, ok := hostScopeForRelation(relation)
		if !ok {
			continue
		}
		key := string(relation.ExpectedRelation().SubjectKey())
		if result[key] == nil {
			result[key] = make(map[HostScope]struct{})
		}
		result[key][scope] = struct{}{}
	}
	return result
}

func sameCanonicalPath(left string, right string) bool {
	if left == right {
		return true
	}
	leftInfo, leftErr := os.Stat(left)
	rightInfo, rightErr := os.Stat(right)
	return leftErr == nil && rightErr == nil && os.SameFile(leftInfo, rightInfo)
}
