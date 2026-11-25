package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	declarationmanifest "github.com/isty2e/daem/internal/declaration/manifest"
	"github.com/isty2e/daem/internal/desired/agentconfig"
	codexhostpath "github.com/isty2e/daem/internal/output/hostpath/codex"
	nativehostpath "github.com/isty2e/daem/internal/output/hostpath/native"
	pihostpath "github.com/isty2e/daem/internal/output/hostpath/pi"
	"github.com/isty2e/daem/internal/realization/profile"
	"github.com/isty2e/daem/internal/realization/profile/configuration"
	"github.com/isty2e/daem/internal/target"
)

func captureCLIFixture(t *testing.T, content string) (root, manifest, host string, args []string) {
	t.Helper()
	root = t.TempDir()
	manifest, host, args = captureCLIFixtureAt(t, root, content)
	return
}

func captureCLIFixtureAt(t *testing.T, root, content string) (manifest, host string, args []string) {
	t.Helper()
	t.Setenv("HOME", filepath.Join(root, "home"))
	for _, name := range []string{"XDG_DATA_HOME", "XDG_STATE_HOME", "XDG_CONFIG_HOME", "XDG_CACHE_HOME"} {
		t.Setenv(name, filepath.Join(root, name))
	}
	manifest, host = filepath.Join(root, "daem.toml"), filepath.Join(root, ".pi", "settings.json")
	if err := os.MkdirAll(filepath.Dir(host), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(host, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	args = []string{"import", "agent-config", "chosen", "--manifest", manifest, "--target", "pi", "--scope", "project", "--config-path", "settings.json"}
	return
}

func captureHostScopes(selected string) []string {
	if selected == "antigravity-cli" {
		return []string{"global"}
	}
	return []string{"project", "global"}
}

func TestImportAgentConfigInlineCollection(t *testing.T) {
	root, manifest, host, initial := captureCLIFixture(t, `{"new":1}`)
	initial = append(initial, "--merge", "--key", "/new")
	const header = "version=1\ntargets=['pi']\n"
	if err := os.WriteFile(manifest, []byte(header+"agent_config=[]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	captureFingerprint(t, initial)

	if err := os.WriteFile(filepath.Join(filepath.Dir(host), "existing.json"), []byte(`{"chosen":2}`), 0o600); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(root, "source.json")
	if err := os.WriteFile(source, []byte(`{"chosen":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	const existing = header + "agent_config=[{id='existing',scope='project',path='existing.json',source='source.json'}]\n"
	if err := os.WriteFile(manifest, []byte(existing), 0o600); err != nil {
		t.Fatal(err)
	}
	if code, _, _ := runCaptureCLI(t, append(initial, "--dry-run")); code == 0 {
		t.Fatal("initial capture appended a block to a nonempty inline collection")
	}

	update := []string{"import", "agent-config", "existing", "--manifest", manifest, "--update-source"}
	switchArgs := append(update, "--source", filepath.Join(root, "export.json"), "--dry-run")
	if code, _, stderr := runCaptureCLI(t, switchArgs); code == 0 || !strings.Contains(stderr, "use [[agent_config]] blocks") {
		t.Fatalf("source switch: expected block conversion guidance, got exit %d: %s", code, stderr)
	}
	fingerprint := captureFingerprint(t, update)
	if code, out, err := runCaptureCLI(t, append(update, "--yes", "--expect-plan", fingerprint)); code != 0 {
		t.Fatal(code, out, err)
	}
	if after, err := os.ReadFile(manifest); err != nil || string(after) != existing {
		t.Fatal("source-only update rewrote the inline manifest", err)
	}
	var result struct{ Chosen int }
	content, err := os.ReadFile(source)
	if err != nil || json.Unmarshal(content, &result) != nil || result.Chosen != 2 {
		t.Fatal("source-only update did not capture the selected value", string(content), err)
	}
}

func captureHostDocument(t *testing.T, manifest, selected, scope, path, ownership string) (string, string) {
	t.Helper()
	root := filepath.Dir(manifest)
	if selected == "opencode" && scope == "project" {
		path = ".opencode/" + path
	}
	if scope == "global" {
		switch selected {
		case "pi":
			t.Setenv(pihostpath.AgentRootEnvironmentVariable, filepath.Join(root, "native-home"))
		case "codex":
			t.Setenv(codexhostpath.HomeEnvironmentVariable, filepath.Join(root, "native-home"))
		case "claude-code":
			t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(root, "native-home"))
		}
	}
	selectedTarget, err := target.ParseTarget(selected)
	if err != nil {
		t.Fatal(err)
	}
	selectedScope, err := target.ParseScope(scope)
	if err != nil {
		t.Fatal(err)
	}
	placement, err := profile.ResolveAgentConfigPlacement(configuration.PlacementInput{
		Target: selectedTarget, Scope: selectedScope, Path: path, Ownership: agentconfig.Ownership(ownership),
	})
	if err != nil {
		t.Fatal(err)
	}
	locations, err := nativehostpath.CaptureLocations(root)
	if err != nil {
		t.Fatal(err)
	}
	if placement.Document().RootReference().Identity() != configuration.ProjectRootIdentity {
		location, err := locations.Resolve(placement.Document().RootReference())
		if err != nil {
			t.Fatal(err)
		}
		root = location.Path()
	}
	child, directory := placement.Document().Coordinate().DirectoryChild()
	if !directory {
		t.Fatal("generic capture fixture requires a directory-child document")
	}
	host := filepath.Join(root, filepath.FromSlash(child))
	if err := os.MkdirAll(filepath.Dir(host), 0o700); err != nil {
		t.Fatal(err)
	}
	return host, path
}

func runCaptureCLI(t *testing.T, args []string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := RunWithOptions(args, RunOptions{Context: t.Context(), Stdout: &stdout, Stderr: &stderr})
	return code, stdout.String(), stderr.String()
}

type importAgentConfigResult struct {
	SchemaVersion                                   int `json:"schema_version"`
	Command, Operation, Mode, Fingerprint, Recovery string
}

func capturePreview(t *testing.T, args []string) importAgentConfigResult {
	t.Helper()
	code, stdout, stderr := runCaptureCLI(t, append(append([]string{}, args...), "--dry-run", "--json"))
	if code != 0 {
		t.Fatalf("preview: code=%d out=%s err=%s", code, stdout, stderr)
	}
	var result importAgentConfigResult
	if err := json.Unmarshal([]byte(stdout), &result); err != nil || result.Command != "import agent-config" || result.Mode != "dry-run" || len(result.Fingerprint) != 64 {
		t.Fatal("invalid approval preview", result, err)
	}
	return result
}

func captureFingerprint(t *testing.T, args []string) string {
	t.Helper()
	return capturePreview(t, args).Fingerprint
}

func requireImportRefusal(t *testing.T, args []string) {
	t.Helper()
	code, stdout, stderr := runCaptureCLI(t, append(args, "--json"))
	var refusal struct {
		ResultType string `json:"result_type"`
	}
	if err := json.Unmarshal([]byte(stdout), &refusal); err != nil || code != 1 || refusal.ResultType != "cli_error" {
		t.Fatalf("expected import refusal, got exit %d: %s %s", code, stdout, stderr)
	}
}

func TestImportAgentConfigLiteralStorageAndMaskedSelectedDisclosure(t *testing.T) {
	content := `{"selected":{"api_key":"literal-private-value","normal":true},"rows":[{"password":"array-private-value"}],"unselected":"host-sibling-private"}`
	root, manifest, host, args := captureCLIFixture(t, content)
	args = append(args, "--key", "/selected", "--key", "/rows")
	for _, flags := range [][]string{{"--dry-run", "--diff"}, {"--dry-run", "--json"}} {
		code, stdout, stderr := runCaptureCLI(t, append(append([]string{}, args...), flags...))
		if code != 0 {
			t.Fatalf("preview failed: %d %s %s", code, stdout, stderr)
		}
		for _, secret := range []string{"literal-private-value", "array-private-value", "host-sibling-private"} {
			if strings.Contains(stdout+stderr, secret) {
				t.Fatalf("capture leaked %q: %s %s", secret, stdout, stderr)
			}
		}
		if !strings.Contains(stdout, "REDACTED") || !strings.Contains(stdout, "plaintext") {
			t.Fatal("disclosure missing masking/risk")
		}
	}
	if _, err := os.Stat(manifest); !os.IsNotExist(err) {
		t.Fatal("preview wrote manifest", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".daem")); !os.IsNotExist(err) {
		t.Fatal("preview created metadata", err)
	}
	fingerprint := captureFingerprint(t, args)
	code, stdout, stderr := runCaptureCLI(t, append(args, "--yes", "--expect-plan", fingerprint, "--json"))
	if code != 0 {
		t.Fatalf("write: %d %s %s", code, stdout, stderr)
	}
	if strings.Contains(stdout+stderr, "literal-private-value") || strings.Contains(stdout+stderr, "array-private-value") {
		t.Fatal("write output leaked literals")
	}
	var result importAgentConfigResult
	if err := json.Unmarshal([]byte(stdout), &result); err != nil || result.SchemaVersion != 3 || result.Operation != "capture" || result.Mode != "captured" || result.Fingerprint != fingerprint {
		t.Fatal("wrong capture result", result, err)
	}
	manifestBytes, err := os.ReadFile(manifest)
	if err != nil {
		t.Fatal(err)
	}
	environment, err := declarationmanifest.Decode(manifestBytes)
	if err != nil || len(environment.AgentConfigs()) != 1 {
		t.Fatal("invalid resulting declaration", err)
	}
	local, _ := environment.AgentConfigs()[0].Source().Local()
	stored, err := os.ReadFile(local.Path())
	if err != nil || !bytes.Contains(stored, []byte("literal-private-value")) || !bytes.Contains(stored, []byte("array-private-value")) || bytes.Contains(stored, []byte("host-sibling-private")) {
		t.Fatal("capture masked storage or acquired a host sibling", string(stored), err)
	}
	actual, err := os.ReadFile(host)
	if err != nil || string(actual) != content {
		t.Fatal("capture changed host", err)
	}
	for _, name := range []string{"daem.lock.toml", ".daem/state.json"} {
		if _, err := os.Stat(filepath.Join(root, name)); !os.IsNotExist(err) {
			t.Fatal("capture wrote runtime artifacts", name, err)
		}
	}
}

func TestImportAgentConfigFingerprintUsesActualMaskedValues(t *testing.T) {
	_, manifest, host, args := captureCLIFixture(t, `{"api_key":"one-private-value"}`)
	args = append(args, "--key", "/api_key")
	approved := captureFingerprint(t, args)
	if err := os.WriteFile(host, []byte(`{"api_key":"another-private-value"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if next := captureFingerprint(t, args); next == approved {
		t.Fatal("masked-value change retained approval")
	}
	code, _, _ := runCaptureCLI(t, append(args, "--yes", "--expect-plan", approved))
	if code != 1 {
		t.Fatal("stale actual-data approval accepted", code)
	}
	if _, err := os.Stat(manifest); !os.IsNotExist(err) {
		t.Fatal("stale approval wrote manifest", err)
	}
}

func TestImportAgentConfigUpdateReleasesMissingLeafWithoutAcquiringSiblings(t *testing.T) {
	_, manifest, host, args := captureCLIFixture(t, `{"nested":{"leaf":1},"sibling":2}`)
	args = append(args, "--key", "/nested")
	fingerprint := captureFingerprint(t, args)
	if code, out, err := runCaptureCLI(t, append(args, "--yes", "--expect-plan", fingerprint)); code != 0 {
		t.Fatal(code, out, err)
	}
	before, err := os.ReadFile(manifest)
	if err != nil {
		t.Fatal(err)
	}
	environment, err := declarationmanifest.Decode(before)
	if err != nil {
		t.Fatal(err)
	}
	local, _ := environment.AgentConfigs()[0].Source().Local()
	if err := os.WriteFile(host, []byte(`{"nested":{"new":9},"sibling":2}`), 0o600); err != nil {
		t.Fatal(err)
	}
	update := []string{"import", "agent-config", "chosen", "--manifest", manifest, "--update-source"}
	fingerprint = captureFingerprint(t, update)
	if code, out, err := runCaptureCLI(t, append(update, "--yes", "--expect-plan", fingerprint)); code != 0 {
		t.Fatal(code, out, err)
	}
	stored, err := os.ReadFile(local.Path())
	if err != nil {
		t.Fatal(err)
	}
	var actual map[string]json.RawMessage
	if err := json.Unmarshal(stored, &actual); err != nil || len(actual) != 0 {
		t.Fatal("last-child release acquired an empty object or siblings", string(stored), err)
	}
	after, err := os.ReadFile(manifest)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("in-place capture changed declaration", err)
	}
}

type captureBrokenOutput struct{}

func (captureBrokenOutput) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

func TestImportAgentConfigRefusesOutputFailureBeforeConfirmationOrWrites(t *testing.T) {
	_, manifest, _, args := captureCLIFixture(t, `{"chosen":1}`)
	args = append(args, "--key", "/chosen")
	prompted := false
	options := interactiveRunOptions(strings.NewReader("yes\n"), captureBrokenOutput{}, io.Discard)
	options.ReadConfirmationLine = func(context.Context, io.Reader, int) (string, error) { prompted = true; return "yes", nil }
	if code := RunWithOptions(args, options); code != 1 || prompted {
		t.Fatal("output failure allowed confirmation", code, prompted)
	}
	if _, err := os.Stat(manifest); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("output failure wrote manifest", err)
	}
}

func TestImportAgentConfigPreviewAfterLostWriteResult(t *testing.T) {
	root, manifest, host, initial := captureCLIFixture(t, `{"chosen":1}`)
	initial = append(initial, "--key", "/chosen")
	source := filepath.Join(root, "daem.d", "agent-config", "chosen.json")
	loseResult := func(args []string, want int) {
		t.Helper()
		preview := capturePreview(t, args)
		if preview.Operation != "capture" {
			t.Fatalf("expected capture before approving, got %q", preview.Operation)
		}
		code := RunWithOptions(append(args, "--yes", "--expect-plan", preview.Fingerprint, "--json"), RunOptions{Context: t.Context(), Stdout: captureBrokenOutput{}, Stderr: io.Discard})
		if code != 1 {
			t.Fatalf("lost write result: exit %d", code)
		}
		var stored struct{ Chosen int }
		data, err := os.ReadFile(source)
		if err != nil || json.Unmarshal(data, &stored) != nil || stored.Chosen != want {
			t.Fatalf("source after lost result = %s, want chosen=%d: %v", data, want, err)
		}
	}

	loseResult(initial, 1)
	requireImportRefusal(t, append(initial, "--dry-run"))

	if err := os.WriteFile(host, []byte(`{"chosen":2}`), 0o600); err != nil {
		t.Fatal(err)
	}
	update := []string{"import", "agent-config", "chosen", "--manifest", manifest, "--update-source"}
	loseResult(update, 2)
	if preview := capturePreview(t, update); preview.Operation != "capture" {
		t.Fatal("repeat update preview did not offer a fresh capture", preview)
	}
}

func TestImportAgentConfigRejectsMixedRequestGrammarAndUnapprovedWrites(t *testing.T) {
	_, manifest, _, args := captureCLIFixture(t, `{"chosen":1}`)
	for _, suffix := range [][]string{{"--key", "/chosen"}, {"--key", "/chosen", "--yes"}, {"--key", "/chosen", "--source", "new.json", "--dry-run"}, {"--update-source", "--dry-run"}, {"--ownership", "document", "--key", "/chosen", "--dry-run"}} {
		if code, _, _ := runCaptureCLI(t, append(append([]string{}, args...), suffix...)); code != 2 {
			t.Fatal("invalid grammar/approval not a usage refusal", suffix, code)
		}
	}
	if _, err := os.Stat(manifest); !os.IsNotExist(err) {
		t.Fatal("usage refusal wrote manifest", err)
	}
}

func TestImportAgentConfigInitialUsesAdmittedRootsAndGrammar(t *testing.T) {
	for _, selected := range []string{"pi", "codex", "claude-code", "opencode", "antigravity-cli"} {
		for _, scope := range captureHostScopes(selected) {
			t.Run(selected+"/"+scope, func(t *testing.T) {
				_, manifest, _, _ := captureCLIFixture(t, `{}`)
				path, content := "settings.json", `{"capture":{"value":true},"unselected":1}`
				if selected == "codex" {
					path, content = "config.toml", "# host comment\nunselected = 1\n[capture]\nvalue = true\n"
				}
				host, path := captureHostDocument(t, manifest, selected, scope, path, "keys")
				if err := os.WriteFile(host, []byte(content), 0o600); err != nil {
					t.Fatal(err)
				}
				args := []string{"import", "agent-config", "chosen", "--manifest", manifest, "--target", selected, "--scope", scope, "--config-path", path, "--key", "/capture"}
				fingerprint := captureFingerprint(t, args)
				if code, out, err := runCaptureCLI(t, append(args, "--yes", "--expect-plan", fingerprint)); code != 0 {
					t.Fatal(code, out, err)
				}
				after, err := os.ReadFile(manifest)
				if err != nil {
					t.Fatal(err)
				}
				environment, err := declarationmanifest.Decode(after)
				if err != nil {
					t.Fatal(err)
				}
				config := environment.AgentConfigs()[0]
				if string(config.Target()) != selected || string(config.Scope()) != scope {
					t.Fatal("capture changed selected placement")
				}
				local, _ := config.Source().Local()
				stored, err := os.ReadFile(local.Path())
				if err != nil || bytes.Contains(stored, []byte("unselected")) || bytes.Contains(stored, []byte("host comment")) || !bytes.Contains(stored, []byte("true")) {
					t.Fatal("initial native grammar/selection failed", string(stored), err)
				}
				current, err := os.ReadFile(host)
				if err != nil || string(current) != content {
					t.Fatal("capture changed host", err)
				}
			})
		}
	}
}

func TestImportAgentConfigWholeInitialPreservesBytesAndOmitsRawDisclosure(t *testing.T) {
	root, manifest, _, _ := captureCLIFixture(t, `{}`)
	host := filepath.Join(root, ".codex", "whole.jsonc")
	content := "/* private-host-comment */\n{ \"api_key\" : \"whole-literal-private\" }\n"
	if err := os.MkdirAll(filepath.Dir(host), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(host, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	args := []string{"import", "agent-config", "whole", "--manifest", manifest, "--target", "codex", "--scope", "project", "--config-path", "whole.jsonc", "--ownership", "document"}
	code, out, errText := runCaptureCLI(t, append(append([]string{}, args...), "--dry-run", "--diff"))
	if code != 0 || strings.Contains(out+errText, "whole-literal-private") || strings.Contains(out+errText, "private-host-comment") {
		t.Fatal("whole preview leaked raw contents", code, out, errText)
	}
	code, out, errText = runCaptureCLI(t, append(append([]string{}, args...), "--dry-run", "--json"))
	var preview struct {
		importAgentConfigResult
		Keys []json.RawMessage
	}
	if err := json.Unmarshal([]byte(out), &preview); err != nil || code != 0 || preview.Operation != "capture" || preview.Keys == nil || len(preview.Keys) != 0 || strings.Contains(out+errText, "whole-literal-private") {
		t.Fatalf("expected a whole-document preview with an empty key array: %s %s", out, errText)
	}
	if code, out, err := runCaptureCLI(t, append(args, "--yes", "--expect-plan", preview.Fingerprint)); code != 0 {
		t.Fatal(code, out, err)
	}
	stored, err := os.ReadFile(filepath.Join(root, "daem.d", "agent-config", "whole.jsonc"))
	if err != nil || string(stored) != content {
		t.Fatal("whole capture reserialized bytes", err)
	}
}
