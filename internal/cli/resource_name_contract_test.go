package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/workflow/authoring"
)

func TestInventoryResourceNamesAreAcceptedByRemove(t *testing.T) {
	for _, test := range authoringResourceNameFixtures() {
		t.Run(test.kind, func(t *testing.T) {
			root, cwd := manifestHintWorkspace(t)
			manifestPath := filepath.Join(cwd, "daem.toml")
			if err := os.WriteFile(manifestPath, []byte(test.manifest), 0o600); err != nil {
				t.Fatal(err)
			}
			before := snapshotApplyInputFiles(t, root)
			var stdout, stderr bytes.Buffer
			if code := runWithoutTerminal([]string{"list", "resources", "--manifest", manifestPath, "--json"}, &stdout, &stderr); code != 0 || stderr.Len() != 0 {
				t.Fatalf("list code=%d stdout=%q stderr=%q", code, &stdout, &stderr)
			}
			var inventory struct {
				SchemaVersion int `json:"schema_version"`
				Resources     []struct {
					Kind string `json:"kind"`
					Key  string `json:"key"`
				} `json:"resources"`
			}
			if err := json.Unmarshal(stdout.Bytes(), &inventory); err != nil {
				t.Fatal(err)
			}
			if inventory.SchemaVersion != 3 || len(inventory.Resources) != 1 || inventory.Resources[0].Kind != test.kind || inventory.Resources[0].Key != "mcp_server" {
				t.Fatalf("inventory lost the command kind or literal key: %+v", inventory)
			}
			row := inventory.Resources[0]
			stdout.Reset()
			stderr.Reset()
			if code := runWithoutTerminal([]string{"remove", row.Kind, row.Key, "--manifest", manifestPath, "--dry-run", "--json"}, &stdout, &stderr); code != 0 || stderr.Len() != 0 {
				t.Fatalf("remove listed resource code=%d stdout=%q stderr=%q", code, &stdout, &stderr)
			}
			var result struct {
				SchemaVersion int `json:"schema_version"`
				Changes       []struct {
					ResourceID string `json:"resource_id"`
					Resource   struct {
						Kind string
						Name string
					} `json:"resource"`
				} `json:"changes"`
			}
			if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if result.SchemaVersion != 7 || len(result.Changes) != 1 || result.Changes[0].ResourceID != test.kind+"/mcp_server" || result.Changes[0].Resource.Kind != test.kind || result.Changes[0].Resource.Name != row.Key {
				t.Fatalf("remove changed listed identity: %+v", result)
			}
			if !reflect.DeepEqual(before, snapshotApplyInputFiles(t, root)) {
				t.Fatal("list or remove preview mutated fixture files")
			}
		})
	}
}

func TestRemoveFailuresProjectRealProducerKindsAndRetainHints(t *testing.T) {
	for _, test := range authoringResourceNameFixtures() {
		for _, commonJSON := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/json=%t", test.kind, commonJSON), func(t *testing.T) {
				root, cwd := manifestHintWorkspace(t)
				manifestPath := filepath.Join(cwd, "daem.toml")
				if err := os.WriteFile(manifestPath, []byte(test.manifest), 0o600); err != nil {
					t.Fatal(err)
				}
				before := snapshotApplyInputFiles(t, root)
				args := []string{"remove", test.kind, "mcp_serve", "--manifest", manifestPath, "--dry-run"}
				if commonJSON {
					args = append(args, "--json")
				}
				var stdout, stderr bytes.Buffer
				if code := runWithoutTerminal(args, &stdout, &stderr); code != 1 {
					t.Fatalf("remove code=%d stdout=%q stderr=%q", code, &stdout, &stderr)
				}
				text := refusalDiagnosticText(t, "remove "+test.kind, &stdout, &stderr, commonJSON)
				if !strings.Contains(text, test.kind+` resource "mcp_serve" not found`) {
					t.Fatalf("primary cause uses a different resource vocabulary: %q", text)
				}
				var argv []string
				if commonJSON {
					var result struct {
						Guidance []struct {
							Kind string
							Argv []string
						}
					}
					if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
						t.Fatal(err)
					}
					for _, hint := range result.Guidance {
						if hint.Kind == "command" && len(hint.Argv) > 2 && hint.Argv[1] == "remove" {
							argv = slices.Clone(hint.Argv[1:])
						}
					}
				} else {
					argv = manifestHintArgv(t, stderr.String(), "hint: available selection: ")
				}
				if len(argv) < 3 || !slices.Equal(argv[:3], []string{"remove", test.kind, "mcp_server"}) || !slices.Contains(argv, "--target") || !slices.Contains(argv, "--scope") {
					t.Fatalf("hint lost the literal key or selection context: %q", argv)
				}
				stdout.Reset()
				stderr.Reset()
				if code := runWithoutTerminal(append(argv, "--dry-run", "--json"), &stdout, &stderr); code != 0 || stderr.Len() != 0 {
					t.Fatalf("hint preview code=%d stdout=%q stderr=%q", code, &stdout, &stderr)
				}
				if !reflect.DeepEqual(before, snapshotApplyInputFiles(t, root)) {
					t.Fatal("failed remove or hint preview mutated fixture files")
				}
			})
		}
	}
}

func TestAuthoringSelectionDiagnosticRetainsPhaseAndOriginalError(t *testing.T) {
	missing := &authoring.ResourceSelectionNotFoundError{Kind: entity.KindMCPServer, Name: "mcp_server"}
	for _, test := range []struct {
		phase  authoring.OperationPhase
		prefix string
	}{
		{authoring.OperationPhaseBuildManifestChange, ""},
		{authoring.OperationPhaseBuildLockfile, ""},
		{authoring.OperationPhaseCommit, "write authoring transaction: "},
	} {
		err := authoring.OperationError{Phase: test.phase, Err: fmt.Errorf("wrapped: %w", missing)}
		originalCause := err.Err
		diagnostic := authoringFailureDiagnostic("remove", "", fmt.Errorf("outer: %w", err))
		var retained *authoring.ResourceSelectionNotFoundError
		if diagnostic.Message() != "remove failed: "+test.prefix+`mcp-server resource "mcp_server" not found` ||
			err.Err != originalCause || err.Phase != test.phase || !errors.As(err, &retained) || retained != missing {
			t.Fatalf("diagnostic=%q original=%+v", diagnostic.Message(), err)
		}
	}
}

func TestAmbiguousMCPRemovalProjectsBothSelectorFailurePaths(t *testing.T) {
	const manifest = `version=1
targets=["claude-code","codex"]
[[mcp_server]]
name="mcp_server"
targets=["claude-code"]
transport="stdio"
command="node"
args=["server.js"]
[[mcp_server]]
name="mcp_server"
targets=["codex"]
transport="stdio"
command="node"
args=["server.js"]
`
	for _, scoped := range []bool{false, true} {
		for _, commonJSON := range []bool{false, true} {
			t.Run(fmt.Sprintf("scope=%t/json=%t", scoped, commonJSON), func(t *testing.T) {
				root, cwd := manifestHintWorkspace(t)
				path := filepath.Join(cwd, "daem.toml")
				if err := os.WriteFile(path, []byte(manifest), 0o600); err != nil {
					t.Fatal(err)
				}
				before := snapshotApplyInputFiles(t, root)
				args := []string{"remove", "mcp-server", "mcp_server", "--manifest", path, "--dry-run"}
				if scoped {
					args = append(args, "--scope", "project")
				}
				if commonJSON {
					args = append(args, "--json")
				}
				var stdout, stderr bytes.Buffer
				if code := runWithoutTerminal(args, &stdout, &stderr); code != 1 {
					t.Fatalf("ambiguous removal code=%d stdout=%q stderr=%q", code, &stdout, &stderr)
				}
				text := refusalDiagnosticText(t, "remove mcp-server", &stdout, &stderr, commonJSON)
				if !strings.Contains(text, `mcp-server resource "mcp_server" is ambiguous`) || !strings.Contains(text, "--target/--scope") {
					t.Fatalf("ambiguous selection lost public name or narrowing advice: %q", text)
				}
				if !reflect.DeepEqual(before, snapshotApplyInputFiles(t, root)) {
					t.Fatal("ambiguous removal changed fixture files")
				}
			})
		}
	}
}

func authoringResourceNameFixtures() []struct {
	kind     string
	manifest string
} {
	return []struct {
		kind     string
		manifest string
	}{
		{"instruction", "version=1\ntargets=[\"codex\"]\n[instructions.mcp_server]\nsource=\"source.md\"\n"},
		{"skill", "version=1\ntargets=[\"codex\"]\n[[skill]]\nid=\"mcp_server\"\nname=\"installed-name\"\nsource={path=\"skills/mcp_server\",mode=\"vendor\"}\n"},
		{"hook", "version=1\ntargets=[\"codex\"]\n[[hook]]\nname=\"mcp_server\"\nevent=\"SessionStart\"\ncommand=\"echo safe\"\n"},
		{"mcp-server", "version=1\ntargets=[\"claude-code\"]\n[[mcp_server]]\nname=\"mcp_server\"\ntransport=\"stdio\"\ncommand=\"node\"\nargs=[\"server.js\"]\n"},
		{"extension", "version=1\ntargets=[\"pi\"]\n[[extension]]\nid=\"mcp_server\"\ncarrier=\"pi-package\"\ntargets=[\"pi\"]\nscope=\"project\"\nsource={host_source=\"npm:@acme/tools@1.2.3\"}\n"},
	}
}
