package pipackage

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/isty2e/daem/internal/encoding/jsonstrict"
	"github.com/isty2e/daem/internal/target"
)

const (
	maximumSettingsBytes = 4 << 20
	maximumSettingsDepth = 32
)

// SettingsInput selects exactly one Pi package settings layer.
type SettingsInput struct {
	ConfigRoot  string
	WorkDir     string
	ProjectRoot string
	Scope       target.Scope
}

// Inventory is one immutable, scope-specific Pi package settings observation.
type Inventory struct {
	scope        target.Scope
	settingsPath string
	settingsBase string
	sources      []string
}

// SettingsPath returns the exact passive authority path consumed by the read.
func (inventory Inventory) SettingsPath() string { return inventory.settingsPath }

// ReadSettings reads only the selected settings file. A missing file is fresh
// empty evidence; malformed, unstable, symlinked, or unreadable files are
// errors and must never become evidence of absence.
func ReadSettings(input SettingsInput) (Inventory, error) {
	settingsPath, err := SettingsPath(input)
	if err != nil {
		return Inventory{}, err
	}
	content, exists, err := readStableRegularFile(settingsPath)
	if err != nil {
		return Inventory{}, fmt.Errorf("read Pi %s package settings %q: %w", input.Scope, settingsPath, err)
	}

	sources := []string(nil)
	if exists {
		sources, err = decodePackageSources(content)
		if err != nil {
			return Inventory{}, fmt.Errorf("decode Pi %s package settings %q: %w", input.Scope, settingsPath, err)
		}
	}
	return Inventory{
		scope:        input.Scope,
		settingsPath: settingsPath,
		settingsBase: filepath.Dir(settingsPath),
		sources:      append([]string(nil), sources...),
	}, nil
}

// SettingsPath resolves the exact settings layer used by Pi package commands.
func SettingsPath(input SettingsInput) (string, error) {
	scope, err := target.ParseScope(string(input.Scope))
	if err != nil {
		return "", fmt.Errorf("Pi settings scope: %w", err)
	}
	switch scope {
	case target.ScopeProject:
		root, err := cleanAbsoluteRoot("Pi project root", input.ProjectRoot)
		if err != nil {
			return "", err
		}
		return filepath.Join(root, ".pi", "settings.json"), nil
	case target.ScopeGlobal:
		root, err := piAgentRoot(input.ConfigRoot, input.WorkDir)
		if err != nil {
			return "", err
		}
		return filepath.Join(root, "settings.json"), nil
	default:
		return "", fmt.Errorf("Pi package settings scope %q is not observable", scope)
	}
}

func piAgentRoot(configRoot string, workDir string) (string, error) {
	root := configRoot
	if root == "" {
		root = os.Getenv("PI_CODING_AGENT_DIR")
	}
	if root == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve Pi config home: %w", err)
		}
		root = filepath.Join(home, ".pi", "agent")
	}
	if root == "~" || strings.HasPrefix(root, "~"+string(os.PathSeparator)) {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("expand Pi config home: %w", err)
		}
		if root == "~" {
			root = home
		} else {
			root = filepath.Join(home, strings.TrimPrefix(root, "~"+string(os.PathSeparator)))
		}
	}
	if !filepath.IsAbs(root) {
		base := workDir
		if base == "" {
			var err error
			base, err = os.Getwd()
			if err != nil {
				return "", fmt.Errorf("resolve Pi config working directory: %w", err)
			}
		}
		root = filepath.Join(base, root)
	}
	return cleanAbsoluteRoot("Pi config root", root)
}

func cleanAbsoluteRoot(label string, root string) (string, error) {
	if strings.TrimSpace(root) == "" || strings.TrimSpace(root) != root {
		return "", fmt.Errorf("%s must be non-empty and trimmed", label)
	}
	if strings.ContainsRune(root, '\x00') {
		return "", fmt.Errorf("%s must not contain a NUL byte", label)
	}
	if !filepath.IsAbs(root) {
		return "", fmt.Errorf("%s must be absolute", label)
	}
	return filepath.Clean(root), nil
}

func readStableRegularFile(path string) ([]byte, bool, error) {
	before, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if before.Mode()&os.ModeSymlink != 0 {
		return nil, false, fmt.Errorf("settings file must not be a symlink")
	}
	if !before.Mode().IsRegular() {
		return nil, false, fmt.Errorf("settings path must be a regular file")
	}
	if before.Size() > maximumSettingsBytes {
		return nil, false, fmt.Errorf("settings file exceeds %d bytes", maximumSettingsBytes)
	}

	file, err := os.Open(path)
	if err != nil {
		return nil, false, err
	}
	defer file.Close()

	opened, err := file.Stat()
	if err != nil {
		return nil, false, err
	}
	if !os.SameFile(before, opened) {
		return nil, false, fmt.Errorf("settings file changed while opening")
	}
	if before.Size() != opened.Size() || !before.ModTime().Equal(opened.ModTime()) {
		return nil, false, fmt.Errorf("settings file changed while opening")
	}
	content, err := io.ReadAll(io.LimitReader(file, maximumSettingsBytes+1))
	if err != nil {
		return nil, false, err
	}
	if len(content) > maximumSettingsBytes {
		return nil, false, fmt.Errorf("settings file exceeds %d bytes", maximumSettingsBytes)
	}

	afterOpen, err := file.Stat()
	if err != nil {
		return nil, false, err
	}
	afterPath, err := os.Lstat(path)
	if err != nil {
		return nil, false, fmt.Errorf("reinspect settings file: %w", err)
	}
	if afterPath.Mode()&os.ModeSymlink != 0 ||
		!os.SameFile(opened, afterOpen) ||
		!os.SameFile(opened, afterPath) ||
		opened.Size() != afterOpen.Size() ||
		!opened.ModTime().Equal(afterOpen.ModTime()) {
		return nil, false, fmt.Errorf("settings file changed while reading")
	}
	return content, true, nil
}

func decodePackageSources(content []byte) ([]string, error) {
	if err := jsonstrict.Validate(content, "Pi settings", maximumSettingsDepth); err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.UseNumber()
	var document map[string]json.RawMessage
	if err := decoder.Decode(&document); err != nil {
		return nil, err
	}
	rawPackages, present := document["packages"]
	if !present {
		return nil, nil
	}
	if bytes.Equal(bytes.TrimSpace(rawPackages), []byte("null")) {
		return nil, fmt.Errorf("packages must be an array when present")
	}
	var entries []json.RawMessage
	if err := json.Unmarshal(rawPackages, &entries); err != nil {
		return nil, fmt.Errorf("packages: %w", err)
	}

	sources := make([]string, 0, len(entries))
	for index, raw := range entries {
		source, err := decodePackageSource(raw)
		if err != nil {
			return nil, fmt.Errorf("packages[%d]: %w", index, err)
		}
		sources = append(sources, source)
	}
	return sources, nil
}

func decodePackageSource(raw json.RawMessage) (string, error) {
	var source string
	if err := json.Unmarshal(raw, &source); err == nil {
		return validateSourceText(source)
	}

	var entry map[string]json.RawMessage
	if err := json.Unmarshal(raw, &entry); err != nil {
		return "", fmt.Errorf("must be a string or object with a source string")
	}
	rawSource, present := entry["source"]
	if !present {
		return "", fmt.Errorf("object source is required")
	}
	if err := json.Unmarshal(rawSource, &source); err != nil {
		return "", fmt.Errorf("object source must be a string")
	}
	return validateSourceText(source)
}

func validateSourceText(source string) (string, error) {
	if strings.TrimSpace(source) == "" || strings.TrimSpace(source) != source {
		return "", fmt.Errorf("source must be non-empty and trimmed")
	}
	for _, character := range source {
		if character < ' ' || character == 0x7f {
			return "", fmt.Errorf("source must not contain control characters")
		}
	}
	return source, nil
}
