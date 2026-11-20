package opencodeplugin

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/isty2e/daem/internal/assurance/observe/relation/opencodeorder"
	"github.com/isty2e/daem/internal/filesnapshot"
	opencodeconfig "github.com/isty2e/daem/internal/realization/configrelation/opencode"
	"github.com/isty2e/daem/internal/target"
)

// InventoryInput selects one OpenCode project or global config layer.
type InventoryInput struct {
	ManifestRoot string
	ConfigRoot   string
	Scope        target.Scope
}

// ReadInventory reads every OpenCode server and TUI candidate that the host
// loads. A kind with no existing candidate retains its default JSON path as an
// absent observation. Malformed or unstable candidates fail closed.
func ReadInventory(input InventoryInput) (opencodeorder.Inventory, error) {
	scope, err := target.ParseScope(string(input.Scope))
	if err != nil {
		return opencodeorder.Inventory{}, fmt.Errorf("OpenCode plugin inventory scope: %w", err)
	}
	directory, err := configDirectory(input, scope)
	if err != nil {
		return opencodeorder.Inventory{}, err
	}

	documents := make([]opencodeorder.Document, 0, 4)
	for _, kind := range []opencodeconfig.ConfigKind{
		opencodeconfig.ConfigServer,
		opencodeconfig.ConfigTUI,
	} {
		selected, err := readConfigDocuments(directory, kind)
		if err != nil {
			return opencodeorder.Inventory{}, err
		}
		documents = append(documents, selected...)
	}

	return opencodeorder.NewInventory(scope, directory, documents)
}

func readConfigDocuments(
	directory string,
	kind opencodeconfig.ConfigKind,
) ([]opencodeorder.Document, error) {
	names, err := opencodeconfig.CandidateNames(kind)
	if err != nil {
		return nil, err
	}
	documents := make([]opencodeorder.Document, 0, len(names))
	var defaultDocument opencodeorder.Document
	for index, name := range names {
		path := filepath.Join(directory, name)
		content, exists, err := filesnapshot.ReadRegularFile(path, opencodeorder.MaximumConfigBytes)
		if err != nil {
			return nil, fmt.Errorf("read OpenCode %s config %q: %w", kind, path, err)
		}
		document, err := opencodeorder.NewDocument(kind, path, content, exists)
		if err != nil {
			return nil, err
		}
		if index == 0 {
			defaultDocument = document
		}
		if exists {
			documents = append(documents, document)
		}
	}
	if len(documents) == 0 {
		documents = append(documents, defaultDocument)
	}
	return documents, nil
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

// OrderAuthorityPaths returns every config candidate whose presence can select
// a different physical plugin sequence. Callers must protect the complete set
// before a host route can create, remove, or replace one of the candidates.
func OrderAuthorityPaths(input InventoryInput) ([]string, error) {
	scope, err := target.ParseScope(string(input.Scope))
	if err != nil {
		return nil, fmt.Errorf("OpenCode plugin order authority scope: %w", err)
	}
	directory, err := configDirectory(input, scope)
	if err != nil {
		return nil, err
	}
	paths := make([]string, 0, 4)
	for _, kind := range []opencodeconfig.ConfigKind{
		opencodeconfig.ConfigServer,
		opencodeconfig.ConfigTUI,
	} {
		names, err := opencodeconfig.CandidateNames(kind)
		if err != nil {
			return nil, err
		}
		for _, name := range names {
			paths = append(paths, filepath.Join(directory, name))
		}
	}
	return paths, nil
}
