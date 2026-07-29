package opencodeplugin

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/isty2e/daem/internal/assurance/observe/filesnapshot"
	opencodeconfig "github.com/isty2e/daem/internal/realization/configrelation/opencode"
	"github.com/isty2e/daem/internal/target"
)

const maximumConfigBytes = 4 << 20

// InventoryInput selects one OpenCode project or global config layer.
type InventoryInput struct {
	ManifestRoot string
	ConfigRoot   string
	Scope        target.Scope
}

// Entry is one exact OpenCode plugin row in physical document order.
type Entry struct {
	source           string
	hostLoadIdentity string
}

// Source returns the exact host-native spelling stored in the selected file.
func (entry Entry) Source() string { return entry.source }

// HostLoadIdentity returns the package or canonical file identity OpenCode
// uses for later-layer replacement.
func (entry Entry) HostLoadIdentity() string { return entry.hostLoadIdentity }

// Document is one immutable selected OpenCode config observation.
type Document struct {
	kind     opencodeconfig.ConfigKind
	path     string
	exists   bool
	revision string
	entries  []Entry
}

// Kind returns the selected server or TUI config family.
func (document Document) Kind() opencodeconfig.ConfigKind { return document.kind }

// Path returns the selected config authority path.
func (document Document) Path() string { return document.path }

// Exists reports whether the selected config file existed.
func (document Document) Exists() bool { return document.exists }

// Revision returns a content digest suitable for compare-and-swap evidence.
func (document Document) Revision() string { return document.revision }

// Entries returns a defensive copy in physical config order.
func (document Document) Entries() []Entry {
	return append([]Entry(nil), document.entries...)
}

// ExactSourceCount returns the number of exact rows in this selected document.
func (document Document) ExactSourceCount(source string) int {
	count := 0
	for _, entry := range document.entries {
		if entry.source == source {
			count++
		}
	}
	return count
}

// Inventory is one immutable project- or global-scope OpenCode observation.
type Inventory struct {
	scope     target.Scope
	directory string
	documents []Document
}

// Scope returns the selected target scope.
func (inventory Inventory) Scope() target.Scope { return inventory.scope }

// Directory returns the selected OpenCode config directory.
func (inventory Inventory) Directory() string { return inventory.directory }

// Documents returns the selected server and TUI observations in stable order.
func (inventory Inventory) Documents() []Document {
	return append([]Document(nil), inventory.documents...)
}

// ReadInventory selects and parses the OpenCode server and TUI documents once.
// Missing selected files are fresh empty observations; malformed or unstable
// selected files fail closed.
func ReadInventory(input InventoryInput) (Inventory, error) {
	scope, err := target.ParseScope(string(input.Scope))
	if err != nil {
		return Inventory{}, fmt.Errorf("OpenCode plugin inventory scope: %w", err)
	}
	directory, err := configDirectory(input, scope)
	if err != nil {
		return Inventory{}, err
	}

	documents := make([]Document, 0, 2)
	for _, kind := range []opencodeconfig.ConfigKind{
		opencodeconfig.ConfigServer,
		opencodeconfig.ConfigTUI,
	} {
		path, err := selectConfigPath(directory, kind)
		if err != nil {
			return Inventory{}, err
		}
		content, exists, err := filesnapshot.ReadRegularFile(path, maximumConfigBytes)
		if err != nil {
			return Inventory{}, fmt.Errorf("read OpenCode %s config %q: %w", kind, path, err)
		}
		document := Document{
			kind:     kind,
			path:     path,
			exists:   exists,
			revision: contentRevision(content),
		}
		if exists {
			parsed, err := opencodeconfig.ParseAt(content, path)
			if err != nil {
				return Inventory{}, fmt.Errorf("observe OpenCode %s config %q: %w", kind, path, err)
			}
			rows := parsed.Entries()
			document.entries = make([]Entry, 0, len(rows))
			for _, row := range rows {
				document.entries = append(document.entries, Entry{
					source:           row.Source(),
					hostLoadIdentity: row.HostLoadIdentity(),
				})
			}
		}
		documents = append(documents, document)
	}

	return Inventory{
		scope:     scope,
		directory: directory,
		documents: documents,
	}, nil
}

func configDirectory(input InventoryInput, scope target.Scope) (string, error) {
	globalRoot := input.ConfigRoot
	if scope == target.ScopeGlobal && globalRoot == "" {
		xdgConfigHome := os.Getenv("XDG_CONFIG_HOME")
		var home string
		if xdgConfigHome == "" {
			var err error
			home, err = os.UserHomeDir()
			if err != nil {
				return "", fmt.Errorf("resolve OpenCode user home: %w", err)
			}
		}
		var err error
		globalRoot, err = opencodeconfig.DefaultGlobalConfigRoot(
			xdgConfigHome,
			home,
		)
		if err != nil {
			return "", err
		}
	}
	return opencodeconfig.ConfigDirectory(input.ManifestRoot, globalRoot, scope)
}

func selectConfigPath(
	directory string,
	kind opencodeconfig.ConfigKind,
) (string, error) {
	name, err := opencodeconfig.SelectName(kind, func(name string) (bool, error) {
		_, err := os.Lstat(filepath.Join(directory, name))
		switch {
		case err == nil:
			return true, nil
		case errors.Is(err, os.ErrNotExist):
			return false, nil
		default:
			return false, err
		}
	})
	if err != nil {
		return "", err
	}
	return filepath.Join(directory, name), nil
}

func contentRevision(content []byte) string {
	digest := sha256.Sum256(content)
	return "sha256:" + hex.EncodeToString(digest[:])
}
