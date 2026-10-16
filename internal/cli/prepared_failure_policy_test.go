package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	daempaths "github.com/isty2e/daem/internal/paths"
)

func TestPreparedImportKeepsCommandOwnedUsageClassification(t *testing.T) {
	for _, relativeData := range []bool{false, true} {
		t.Run(fmt.Sprint(relativeData), func(t *testing.T) {
			manifestHintWorkspace(t)
			args := []string{"import", "--json"}
			if relativeData {
				t.Setenv("XDG_DATA_HOME", "relative-data")
				args = append(args, "--target", "codex")
			}
			var stdout, stderr bytes.Buffer
			code := runWithoutTerminal(args, &stdout, &stderr)
			var payload struct {
				Command string `json:"command"`
				Error   struct {
					Kind string `json:"kind"`
				} `json:"error"`
			}
			if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil || code != 2 || stderr.Len() != 0 || payload.Command != "import" || payload.Error.Kind != "usage" {
				t.Fatalf("workflow-owned usage classification changed: code=%d decode=%v payload=%#v stderr=%q", code, err, payload, stderr.String())
			}
		})
	}
}

func TestPreparedImportKeepsAllBasicRepairActionsWithoutLiteralValues(t *testing.T) {
	root, cwd := manifestHintWorkspace(t)
	const withheld = "SYNTHETIC_IMPORT_LITERAL_CANARY"
	var config strings.Builder
	config.WriteString(`{"mcpServers":{`)
	for index := range 6 {
		if index != 0 {
			config.WriteByte(',')
		}
		fmt.Fprintf(&config, `"server%d":{"type":"stdio","command":"example","env":{"API_TOKEN":%q}}`, index, withheld)
	}
	config.WriteString(`}}`)
	if err := os.WriteFile(filepath.Join(cwd, ".mcp.json"), []byte(config.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	before := snapshotApplyInputFiles(t, root)
	var stdout, stderr bytes.Buffer
	args := []string{"import", "--target", "claude-code", "--scope", "project", "--dry-run", "--json", "--manifest", filepath.Join(cwd, "daem.toml")}
	if code := runWithoutTerminal(args, &stdout, &stderr); code != 1 {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	diagnostic := refusalDiagnosticText(t, "import", &stdout, &stderr, true)
	if strings.Contains(diagnostic, withheld) {
		t.Fatal("import failure exposed a rejected literal")
	}
	for index := range 6 {
		if !strings.Contains(diagnostic, fmt.Sprintf("#/mcpServers/server%d", index)) {
			t.Fatalf("lost action-required item %d: %s", index, diagnostic)
		}
	}
	var payload struct {
		Guidance []struct {
			Kind, Message string
			Argv          []string
		} `json:"guidance"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	actions := 0
	for _, guidance := range payload.Guidance {
		if guidance.Kind == "instruction" && strings.Contains(guidance.Message, "symbolic environment references") {
			actions++
		}
	}
	if actions != 6 {
		t.Fatalf("repair actions=%d, want every admitted action", actions)
	}
	if after := snapshotApplyInputFiles(t, root); !reflect.DeepEqual(before, after) {
		t.Fatal("rejected import changed workspace files")
	}
}

func TestPreparedRecoverPlanningFailureKeepsNeutralFenceGuidance(t *testing.T) {
	for _, mode := range []string{"--dry-run", "--yes"} {
		t.Run(mode, func(t *testing.T) {
			root, cwd := manifestHintWorkspace(t)
			manifest := filepath.Join(cwd, "daem.toml")
			writeHintManifest(t, manifest)
			paths, err := daempaths.Resolve(manifest)
			if err != nil {
				t.Fatal(err)
			}
			residue := filepath.Join(paths.StateDir, ".daem-tmp-"+strings.Repeat("b", 32))
			if err := os.MkdirAll(residue, 0o700); err != nil {
				t.Fatal(err)
			}
			var stdout, stderr bytes.Buffer
			if code := runWithoutTerminal([]string{"recover", mode, "--json", "--manifest", manifest}, &stdout, &stderr); code != 1 {
				t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
			}
			diagnostic := refusalDiagnosticText(t, "recover", &stdout, &stderr, true)
			if strings.Contains(diagnostic, root) || !strings.Contains(diagnostic, "no recoverable journal operation") || !strings.Contains(diagnostic, "abandoned_residue") || !strings.Contains(diagnostic, "do not delete reserved names by prefix") {
				t.Fatalf("neutral fence diagnostic lost privacy or remediation: %s", diagnostic)
			}
			if _, err := os.Stat(residue); err != nil {
				t.Fatalf("refused recovery changed residue: %v", err)
			}
		})
	}
}
