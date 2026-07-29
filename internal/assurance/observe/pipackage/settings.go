package pipackage

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/isty2e/daem/internal/assurance/observe/filesnapshot"
	"github.com/isty2e/daem/internal/encoding/jsonstrict"
	pihostpath "github.com/isty2e/daem/internal/output/hostpath/pi"
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
	revision     string
	sources      []string
}

// SettingsPath returns the exact passive authority path consumed by the read.
func (inventory Inventory) SettingsPath() string { return inventory.settingsPath }

// Scope returns the selected Pi settings scope.
func (inventory Inventory) Scope() target.Scope { return inventory.scope }

// Revision returns a content digest for the selected settings observation.
func (inventory Inventory) Revision() string { return inventory.revision }

// Entry is one exact stored Pi package row plus its parsed host identity.
type Entry struct {
	source           string
	hostLoadIdentity string
	localIdentity    string
}

// Source returns the exact spelling stored in the selected settings file.
func (entry Entry) Source() string { return entry.source }

// HostLoadIdentity returns the source-class-qualified identity used for
// duplicate and order analysis.
func (entry Entry) HostLoadIdentity() string { return entry.hostLoadIdentity }

// LocalIdentity returns the lexical absolute local source identity when the
// row is local.
func (entry Entry) LocalIdentity() (string, bool) {
	return entry.localIdentity, entry.localIdentity != ""
}

// Entries returns exact package rows in physical settings order.
func (inventory Inventory) Entries() ([]Entry, error) {
	entries := make([]Entry, 0, len(inventory.sources))
	for index, source := range inventory.sources {
		identity, err := sourceIdentityForSettings(
			source,
			inventory.settingsBase,
			inventory.scope,
		)
		if err != nil {
			return nil, fmt.Errorf("parse Pi settings package source[%d]: %w", index, err)
		}
		entry := Entry{
			source:           source,
			hostLoadIdentity: identity.hostLoadIdentity(inventory.scope),
		}
		if identity.kind == sourceKindLocal {
			entry.localIdentity = identity.key
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

// ReadSettings reads only the selected settings file. A missing file is fresh
// empty evidence; malformed, unstable, symlinked, or unreadable files are
// errors and must never become evidence of absence.
func ReadSettings(input SettingsInput) (Inventory, error) {
	settingsPath, err := SettingsPath(input)
	if err != nil {
		return Inventory{}, err
	}
	content, exists, err := filesnapshot.ReadRegularFile(
		settingsPath,
		maximumSettingsBytes,
	)
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
		revision:     settingsRevision(content),
		sources:      append([]string(nil), sources...),
	}, nil
}

func settingsRevision(content []byte) string {
	digest := sha256.Sum256(content)
	return "sha256:" + hex.EncodeToString(digest[:])
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
		root, err := pihostpath.ResolveAgentRoot(pihostpath.AgentRootInput{
			ExplicitRoot: input.ConfigRoot,
			WorkDir:      input.WorkDir,
		})
		if err != nil {
			return "", err
		}
		return filepath.Join(root, "settings.json"), nil
	default:
		return "", fmt.Errorf("Pi package settings scope %q is not observable", scope)
	}
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
