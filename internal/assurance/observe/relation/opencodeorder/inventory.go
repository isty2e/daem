package opencodeorder

import (
	"fmt"
	"path/filepath"
	"slices"

	opencodeconfig "github.com/isty2e/daem/internal/realization/configrelation/opencode"
	"github.com/isty2e/daem/internal/target"
)

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

// Documents returns every loaded server and TUI candidate in stable order.
// When a kind has no loaded candidate, its default JSON path is represented as
// one absent document.
func (inventory Inventory) Documents() []Document {
	return append([]Document(nil), inventory.documents...)
}

// NewInventory admits the host's complete ordered candidate selection for one layer.
func NewInventory(scope target.Scope, directory string, documents []Document) (Inventory, error) {
	if _, err := target.ParseScope(string(scope)); err != nil {
		return Inventory{}, fmt.Errorf("OpenCode plugin inventory scope: %w", err)
	}
	if !filepath.IsAbs(directory) || filepath.Clean(directory) != directory {
		return Inventory{}, fmt.Errorf("OpenCode plugin order directory is not canonical")
	}
	inventory := Inventory{scope: scope, directory: directory, documents: documents}
	if err := validateOrderInventory(inventory); err != nil {
		return Inventory{}, err
	}
	inventory.documents = slices.Clone(documents)
	return inventory, nil
}

func validateOrderInventory(inventory Inventory) error {
	index := 0
	for _, kind := range []opencodeconfig.ConfigKind{
		opencodeconfig.ConfigServer,
		opencodeconfig.ConfigTUI,
	} {
		names, err := opencodeconfig.CandidateNames(kind)
		if err != nil {
			return err
		}
		seen := 0
		for index < len(inventory.documents) && inventory.documents[index].kind == kind {
			document := inventory.documents[index]
			if seen >= len(names) ||
				document.path != filepath.Join(inventory.directory, names[seen]) {
				if seen == 0 && len(names) > 1 &&
					document.path == filepath.Join(inventory.directory, names[1]) {
					seen = 1
				} else {
					return fmt.Errorf(
						"OpenCode plugin order inventory has non-canonical %s candidate order",
						kind,
					)
				}
			}
			if !document.exists && (seen != 0 || index+1 < len(inventory.documents) &&
				inventory.documents[index+1].kind == kind) {
				return fmt.Errorf(
					"OpenCode plugin order inventory has non-canonical absent %s candidate",
					kind,
				)
			}
			seen++
			index++
		}
		if seen == 0 {
			return fmt.Errorf("OpenCode plugin order inventory has no %s candidate", kind)
		}
	}
	if index != len(inventory.documents) {
		return fmt.Errorf("OpenCode plugin order inventory has unexpected candidate documents")
	}
	return nil
}
