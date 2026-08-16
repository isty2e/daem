package pipackage

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/isty2e/daem/internal/filesnapshot"
	pihostpath "github.com/isty2e/daem/internal/output/hostpath/pi"
	piconfig "github.com/isty2e/daem/internal/realization/configrelation/pi"
	"github.com/isty2e/daem/internal/target"
)

const (
	// MaximumSettingsBytes is the observation and mutation limit for Pi settings.
	MaximumSettingsBytes = 4 << 20
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
	exists       bool
	content      []byte
	document     piconfig.Document
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
	documentEntries := inventory.document.Entries()
	entries := make([]Entry, 0, len(documentEntries))
	for index, documentEntry := range documentEntries {
		source := documentEntry.Source()
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

// EntriesAdmitted applies admit to every raw source before any identity or
// normalization work runs, then returns Entries. Import flows own admission
// policy; observation keeps Entries permissive so stored rows remain
// observable evidence.
func (inventory Inventory) EntriesAdmitted(admit func(source string) error) ([]Entry, error) {
	for index, documentEntry := range inventory.document.Entries() {
		if err := admit(documentEntry.Source()); err != nil {
			return nil, fmt.Errorf("admit Pi settings package source[%d]: %w", index, err)
		}
	}
	return inventory.Entries()
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
		MaximumSettingsBytes,
	)
	if err != nil {
		return Inventory{}, fmt.Errorf("read Pi %s package settings %q: %w", input.Scope, settingsPath, err)
	}

	var document piconfig.Document
	if exists {
		document, err = piconfig.Parse(content)
		if err != nil {
			return Inventory{}, fmt.Errorf("decode Pi %s package settings %q: %w", input.Scope, settingsPath, err)
		}
	}
	return Inventory{
		scope:        input.Scope,
		settingsPath: settingsPath,
		settingsBase: filepath.Dir(settingsPath),
		revision:     settingsRevision(content),
		exists:       exists,
		content:      append([]byte(nil), content...),
		document:     document,
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

func validateSourceText(source string) (string, error) {
	if err := piconfig.ValidatePackageSource(source); err != nil {
		return "", err
	}
	return source, nil
}
