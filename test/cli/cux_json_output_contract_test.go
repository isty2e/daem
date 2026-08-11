package cli_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/contractversion"
	"github.com/isty2e/daem/test/testkit"
)

func TestCUXInitJSONIsOneSchemaVersionedDocument(t *testing.T) {
	root := t.TempDir()
	testkit.WithWorkingDirectory(t, root)
	payload := runCUXJSON(t, []string{"init", "--dry-run", "--json"})
	assertCUXJSONHeader(t, payload, contractversion.InitJSON, "init", "dry-run")
	if payload["action"] != "create" || payload["content"] == "" || payload["manifest_path"] == "" {
		t.Fatalf("payload = %#v, want exact init plan", payload)
	}
}

func TestCUXListResourcesJSONPreservesAllRows(t *testing.T) {
	root := t.TempDir()
	manifestPath := filepath.Join(root, "daem.toml")
	testkit.WriteFile(t, root, "daem.toml", `version = 1
targets = ["codex"]

[[skill]]
name = "oracle"
source = { path = "skills/oracle", mode = "vendor" }
targets = ["codex"]
`)
	payload := runCUXJSON(t, []string{"list", "resources", "--manifest", manifestPath, "--json"})
	assertCUXJSONHeader(t, payload, contractversion.ResourceInventoryJSON, "list resources", "")
	if payload["resource_count"] != float64(1) {
		t.Fatalf("payload = %#v, want one resource", payload)
	}
	resources, ok := payload["resources"].([]any)
	if !ok || len(resources) != 1 {
		t.Fatalf("resources = %#v, want one exhaustive row", payload["resources"])
	}
}

func TestCUXListOutputsJSONUsesInventoryRoute(t *testing.T) {
	root := t.TempDir()
	manifestPath := filepath.Join(root, "daem.toml")
	testkit.WriteFile(t, root, "daem.toml", "version = 1\ntargets = [\"codex\"]\n")
	payload := runCUXJSON(t, []string{"list", "outputs", "--manifest", manifestPath, "--json"})
	assertCUXJSONHeader(t, payload, contractversion.OutputInventoryJSON, "list outputs", "")
	if managed, ok := payload["managed"].([]any); !ok || len(managed) != 0 {
		t.Fatalf("payload = %#v, want empty managed inventory array", payload)
	}
	if unmanaged, ok := payload["unmanaged"].([]any); !ok || len(unmanaged) != 0 {
		t.Fatalf("payload = %#v, want empty unmanaged inventory array", payload)
	}
	if blocked, ok := payload["blocked"].([]any); !ok || len(blocked) != 0 || payload["blocked_count"] != float64(0) {
		t.Fatalf("payload = %#v, want empty blocked inventory array and count", payload)
	}
}

func TestCUXRecoverYesJSONReportsWriteResult(t *testing.T) {
	root := t.TempDir()
	manifestPath := filepath.Join(root, "daem.toml")
	paths, currentState, _, _, _ := captureCLIRecoveryUpdateJournal(t, manifestPath)
	testkit.WriteStatefile(t, paths.StatefilePath, currentState)
	testkit.WriteFile(t, root, "AGENTS.md", "new instructions\n")

	payload := runCUXJSON(t, []string{"recover", "--manifest", manifestPath, "--yes", "--json"})
	assertCUXJSONHeader(t, payload, contractversion.RecoveryJSON, "recover", "write")
	if payload["phase"] != "completed" ||
		payload["has_errors"] != false ||
		payload["operation_id"] != "20260621T120000.000000000Z-apply" ||
		payload["action_count"] != float64(0) ||
		payload["cleanup_obligation_count"] != float64(0) {
		t.Fatalf("payload = %#v, want successful recovery result", payload)
	}
	for _, stale := range []string{"authority_kind", "operation_dir", "classification"} {
		if _, present := payload[stale]; present {
			t.Fatalf("terminal recovery payload retained %q: %#v", stale, payload)
		}
	}
}

func runCUXJSON(t *testing.T, args []string) map[string]any {
	t.Helper()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunCLI(args, &stdout, &stderr)
	if exitCode != 0 || stderr.Len() != 0 {
		t.Fatalf("args=%q exitCode=%d stdout=%q stderr=%q", args, exitCode, stdout.String(), stderr.String())
	}
	decoder := json.NewDecoder(bytes.NewReader(stdout.Bytes()))
	var payload map[string]any
	if err := decoder.Decode(&payload); err != nil {
		t.Fatalf("decode JSON: %v\n%s", err, stdout.Bytes())
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		t.Fatalf("stdout must end after one JSON value, trailing=%#v err=%v: %s", trailing, err, stdout.Bytes())
	}
	return payload
}

func assertCUXJSONHeader(t *testing.T, payload map[string]any, version int, command string, mode string) {
	t.Helper()
	if payload["schema_version"] != float64(version) || payload["command"] != command {
		t.Fatalf("payload header = %#v", payload)
	}
	if mode != "" && payload["mode"] != mode {
		t.Fatalf("payload mode = %#v, want %q", payload["mode"], mode)
	}
}
