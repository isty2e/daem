package opencodeorder_test

import (
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/assurance/observe/relation/opencodeorder"
	opencodeconfig "github.com/isty2e/daem/internal/realization/configrelation/opencode"
	"github.com/isty2e/daem/internal/target"
)

func TestNewInventoryValidatesSuppliedCandidateSelection(t *testing.T) {
	directory := t.TempDir()
	doc := func(kind opencodeconfig.ConfigKind, name string, exists bool) opencodeorder.Document {
		t.Helper()
		var content []byte
		if exists {
			content = []byte(`{}`)
		}
		document, err := opencodeorder.NewDocument(kind, filepath.Join(directory, name), content, exists)
		if err != nil {
			t.Fatal(err)
		}
		return document
	}
	json := doc(opencodeconfig.ConfigServer, "opencode.json", true)
	jsonc := doc(opencodeconfig.ConfigServer, "opencode.jsonc", true)
	missingJSON := doc(opencodeconfig.ConfigServer, "opencode.json", false)
	missingJSONC := doc(opencodeconfig.ConfigServer, "opencode.jsonc", false)
	tui := doc(opencodeconfig.ConfigTUI, "tui.json", false)
	for name, documents := range map[string][]opencodeorder.Document{
		"missing family":               {json},
		"wrong family order":           {tui, json},
		"wrong candidate order":        {jsonc, json, tui},
		"duplicate candidate":          {json, json, tui},
		"absent with existing sibling": {missingJSON, jsonc, tui},
		"absent non-default":           {missingJSONC, tui},
		"after final family":           {json, tui, jsonc},
		"uninitialized document":       {{}, tui},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := opencodeorder.NewInventory(target.ScopeProject, directory, documents); err == nil {
				t.Fatal("invalid inventory was accepted")
			}
		})
	}
	for _, dir := range []string{"relative", directory + "/.", filepath.Join(directory, "other")} {
		if _, err := opencodeorder.NewInventory(target.ScopeProject, dir, []opencodeorder.Document{json, tui}); err == nil {
			t.Fatalf("wrong inventory directory %q accepted", dir)
		}
	}
	for _, documents := range [][]opencodeorder.Document{{json, jsonc, tui}, {jsonc, tui}, {missingJSON, tui}} {
		inventory, err := opencodeorder.NewInventory(target.ScopeProject, directory, documents)
		if err != nil {
			t.Fatal(err)
		}
		path := documents[0].Path()
		documents[0] = opencodeorder.Document{}
		inventory.Documents()[0] = opencodeorder.Document{}
		if got := inventory.Documents()[0].Path(); got != path {
			t.Fatalf("inventory path changed from %q to %q", path, got)
		}
	}
}
