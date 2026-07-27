package clipresent

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	listworkflow "github.com/isty2e/daem/internal/workflow/list"
)

func TestPrintListPathsUsesTargetScopeResourceTree(t *testing.T) {
	manifestPath, inventory := listPathPresentationFixture(t)

	var output bytes.Buffer
	PrintListPathsWithOptions(&output, manifestPath, inventory, HumanOptions{})
	text := output.String()
	for _, fragment := range []string{
		"manifest: " + manifestPath,
		"codex\n  project\n    instructions\n",
		"      write: AGENTS.md [selected, default]",
		"    skills\n",
		"      write: .agents/skills [default]",
		"  global\n    instructions\n",
		"      write: ~/.codex/skills [selected]",
		"    MCP servers\n",
		"      config: ~/.codex/config.toml",
		"    extensions (codex-plugin)\n",
		"      install: codex.plugin-carrier.install",
	} {
		if !strings.Contains(text, fragment) {
			t.Fatalf("human path inventory missing %q:\n%s", fragment, text)
		}
	}
	if strings.Contains(text, "managed-path") || strings.Contains(text, "delegated-profile") {
		t.Fatalf("default human output exposed internal classifications:\n%s", text)
	}

	output.Reset()
	PrintListPathsWithOptions(&output, manifestPath, inventory, HumanOptions{Verbose: true})
	verbose := output.String()
	if !strings.Contains(verbose, "realization=managed-path source=profile selection=manifest-explicit requested=true") {
		t.Fatalf("verbose path inventory omitted exact selection evidence:\n%s", verbose)
	}
}

func TestPrintListPathsJSONPreservesFlatVariantContract(t *testing.T) {
	manifestPath, inventory := listPathPresentationFixture(t)

	var output bytes.Buffer
	if err := PrintListPathsJSON(&output, manifestPath, inventory); err != nil {
		t.Fatalf("PrintListPathsJSON returned error: %v", err)
	}
	var payload struct {
		SchemaVersion int                      `json:"schema_version"`
		Command       string                   `json:"command"`
		ManifestPath  string                   `json:"manifest_path"`
		LocationCount int                      `json:"location_count"`
		Locations     []map[string]interface{} `json:"locations"`
	}
	if err := json.Unmarshal(output.Bytes(), &payload); err != nil {
		t.Fatalf("JSON output is invalid: %v\n%s", err, output.String())
	}
	if payload.SchemaVersion != 1 || payload.Command != "list paths" ||
		payload.ManifestPath != manifestPath ||
		payload.LocationCount != len(payload.Locations) {
		t.Fatalf("JSON envelope = %#v", payload)
	}

	explicitFound := false
	unsupportedFound := false
	for _, row := range payload.Locations {
		switch row["kind"] {
		case "path":
			if _, present := row["path"]; !present {
				t.Fatalf("path row omitted path: %#v", row)
			}
			for _, forbidden := range []string{"route", "operation", "reason", "detail"} {
				if _, present := row[forbidden]; present {
					t.Fatalf("path row carries %s: %#v", forbidden, row)
				}
			}
		case "route":
			if _, present := row["route"]; !present {
				t.Fatalf("route row omitted route: %#v", row)
			}
			if _, present := row["operation"]; !present {
				t.Fatalf("route row omitted operation: %#v", row)
			}
			for _, forbidden := range []string{"path", "reason", "detail"} {
				if _, present := row[forbidden]; present {
					t.Fatalf("route row carries %s: %#v", forbidden, row)
				}
			}
		case "unsupported":
			if _, present := row["reason"]; !present {
				t.Fatalf("unsupported row omitted reason: %#v", row)
			}
			for _, forbidden := range []string{"path", "route", "operation"} {
				if _, present := row[forbidden]; present {
					t.Fatalf("unsupported row carries %s: %#v", forbidden, row)
				}
			}
		default:
			t.Fatalf("unknown location row kind: %#v", row)
		}
		if row["target"] == "codex" &&
			row["scope"] == "global" &&
			row["resource"] == "skill" &&
			row["path"] == "~/.codex/skills" &&
			row["role"] == "write" {
			explicitFound = row["selected"] == true &&
				row["selection_source"] == "manifest-explicit" &&
				row["kind"] == "path"
		}
		if row["kind"] == "unsupported" {
			unsupportedFound = true
		}
	}
	if !explicitFound || !unsupportedFound {
		t.Fatalf("JSON rows missing explicit path or unsupported variant:\n%s", output.String())
	}
}

func listPathPresentationFixture(t *testing.T) (string, listworkflow.LocationInventory) {
	t.Helper()
	manifestPath := filepath.Join(t.TempDir(), "daem.toml")
	content := []byte(`
version = 1
targets = ["codex"]

[instructions.project]
source = "AGENTS.md"

[[skill]]
name = "review"
source = { git = "https://example.test/skills.git", path = "skills/review", ref = "main" }
scope = "global"

[skill.target.codex]
install_to = "~/.codex/skills"
`)
	if err := os.WriteFile(manifestPath, content, 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := listworkflow.RunPaths(context.Background(), listworkflow.Input{ManifestPath: manifestPath})
	if err != nil {
		t.Fatalf("RunPaths returned error: %v", err)
	}
	return manifestPath, result.Inventory
}
