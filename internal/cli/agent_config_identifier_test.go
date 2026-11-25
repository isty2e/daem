package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"

	"github.com/isty2e/daem/internal/output"
)

func TestCaptureMachineIdentifiersRoundTrip(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	root = filepath.Join(root, `reader's\workspace`)
	long := strings.Repeat("k", 4300)
	content := `{"ext":{"a/b":"a\\b"},"":1,"` + long + `":2,"api_key":"private-fixture-value"}`
	manifest, _, _ := captureCLIFixtureAt(t, root, content)
	const name = `config\prefs`
	const spec = "version=1\ntargets=['pi']\n[[agent_config]]\nid='" + name + "'\nscope='project'\npath='settings.json'\nsource='./credentials.json'\n"
	if err := os.WriteFile(manifest, []byte(spec), 0o600); err != nil {
		t.Fatal(err)
	}
	referent := filepath.Join(root, `real\prefs.json`)
	source := filepath.Join(root, "credentials.json")
	if err := os.WriteFile(referent, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(referent, source); err != nil {
		t.Fatal(err)
	}

	args := []string{"import", "agent-config", name, "--manifest", manifest, "--update-source"}
	code, text, stderr := runCaptureCLI(t, append(slices.Clone(args), "--dry-run", "--json"))
	if code != 0 || stderr != "" || strings.Contains(text, "private-fixture-value") {
		t.Fatalf("preview: exit %d: %s %s", code, text, stderr)
	}
	var result struct {
		SchemaVersion                 int `json:"schema_version"`
		Declaration, Source, Referent string
		Keys                          []struct {
			KeyPath []string `json:"key_path"`
			Path    json.RawMessage
			After   string
		}
		References []struct{ Resource, Path string } `json:"known_references"`
		NextSteps  []string                          `json:"next_steps"`
	}
	if err := json.Unmarshal([]byte(text), &result); err != nil {
		t.Fatal(err)
	}
	if result.SchemaVersion != 2 || result.Declaration != name || result.Source != source || result.Referent != referent || len(result.References) != 1 || result.References[0].Path != source || result.References[0].Resource != `agent_config:config%5Cprefs` {
		t.Fatalf("wrong named file, referent, or reader: %s", text)
	}
	want := map[string]bool{"/ext/a~1b": true, "/": true, "/" + long: true, "/api_key": true}
	for _, row := range result.Keys {
		key, err := output.NewKeyPath(row.KeyPath...)
		if err != nil || !want[key.String()] || row.Path != nil {
			t.Fatalf("unexpected key %q: %v", row.KeyPath, err)
		}
		delete(want, key.String())
		if key.String() == "/ext/a~1b" {
			var value string
			if err := json.Unmarshal([]byte(row.After), &value); err != nil || value != `a\b` {
				t.Fatalf("host snippet %q: %v", row.After, err)
			}
			args[2] = result.Declaration
			if code, _, stderr := runCaptureCLI(t, append(slices.Clone(args), "--dry-run", "--key", key.String())); code != 0 {
				t.Fatal("returned name and key could not select the same setting:", stderr)
			}
		}
	}
	if len(want) != 0 {
		t.Fatalf("missing %d capture keys", len(want))
	}
	code, human, stderr := runCaptureCLI(t, append(args, "--dry-run", "--diff"))
	if code != 0 || strings.Contains(human+stderr, "private-fixture-value") || len(result.NextSteps) != 3 {
		t.Fatal("human capture preview or next steps missing", code, stderr)
	}
	for _, step := range result.NextSteps {
		if !strings.Contains(human, "  next: "+step+"\n") {
			t.Fatalf("human and JSON steps disagree: %q", step)
		}
	}
}

func humanAgentConfigFingerprint(t *testing.T, args []string) string {
	t.Helper()
	code, text, stderr := runCaptureCLI(t, append(slices.Clone(args), "--dry-run"))
	if code != 0 {
		t.Fatalf("human preview exit %d: %s %s", code, text, stderr)
	}
	for line := range strings.SplitSeq(text, "\n") {
		if value, ok := strings.CutPrefix(line, "fingerprint: "); ok && value != "" {
			return value
		}
	}
	t.Fatal("missing approval fingerprint")
	return ""
}

func requireWithheldAgentConfigResult(t *testing.T, args []string, field, hidden string) {
	t.Helper()
	fingerprint := humanAgentConfigFingerprint(t, args)
	for _, flags := range [][]string{{"--dry-run", "--json"}, {"--yes", "--expect-plan", fingerprint, "--json"}} {
		code, stdout, stderr := runCaptureCLI(t, append(slices.Clone(args), flags...))
		var result struct {
			Type  string `json:"result_type"`
			Error struct{ Kind, Message string }
		}
		if err := json.Unmarshal([]byte(stdout), &result); err != nil || code != 1 || stderr != "" || result.Type != "cli_error" || result.Error.Kind != "operation" || !strings.Contains(result.Error.Message, field) || strings.Contains(stdout+stderr, hidden) {
			t.Fatalf("expected only the field %q in an operation refusal, exit %d: %s %s", field, code, stdout, stderr)
		}
	}
}

func TestProtectedCaptureIdentifiersLeaveFilesAndRecoveryEvidenceUntouched(t *testing.T) {
	const hidden = "synthetic-private-label"
	root := filepath.Join(t.TempDir(), "password="+hidden)
	manifest, host, args := captureCLIFixtureAt(t, root, `{"chosen":1}`)
	args = append(args, "--key", "/chosen")
	source := filepath.Join(root, "daem.d", "agent-config", "chosen.json")
	requireWithheldAgentConfigResult(t, args, "source", hidden)
	for _, name := range []string{manifest, source} {
		if _, err := os.Stat(name); !os.IsNotExist(err) {
			t.Fatalf("refused capture changed %s: %v", filepath.Base(name), err)
		}
	}
	fingerprint := humanAgentConfigFingerprint(t, args)
	if code, text, stderr := runCaptureCLI(t, append(slices.Clone(args), "--yes", "--expect-plan", fingerprint)); code != 0 || strings.Contains(text+stderr, hidden) {
		t.Fatalf("human capture: exit %d: %s %s", code, text, stderr)
	}
	after, err := os.ReadFile(manifest)
	if err != nil {
		t.Fatal(err)
	}
	values, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	marker := interruptedCapture(t, manifest, source, nil, after, nil, values, false)
	beforeMarker, err := os.ReadFile(marker)
	if err != nil {
		t.Fatal(err)
	}
	requireWithheldAgentConfigResult(t, args, "manifest", hidden)
	for name, want := range map[string][]byte{marker: beforeMarker, source: values, host: []byte(`{"chosen":1}`)} {
		got, err := os.ReadFile(name)
		if err != nil || !bytes.Equal(got, want) {
			t.Fatalf("refused recovery changed %s: %v", filepath.Base(name), err)
		}
	}
	if _, err := os.Stat(manifest); !os.IsNotExist(err) {
		t.Fatal("refused recovery published the manifest", err)
	}
}

func TestProtectedReplacementRouteLeavesHostUntouched(t *testing.T) {
	const hidden = "synthetic-private-label"
	root := filepath.Join(t.TempDir(), "password="+hidden)
	manifest, host, _ := captureCLIFixtureAt(t, root, `{"model":"current"}`)
	const spec = "version=1\ntargets=['pi']\n[[agent_config]]\nid='prefs'\nscope='project'\npath='settings.json'\nsource='./prefs.json'\n"
	if err := os.WriteFile(manifest, []byte(spec), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "prefs.json"), []byte(`{"model":"desired"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if code, out, stderr := runCaptureCLI(t, []string{"lock", "--manifest", manifest}); code != 0 {
		t.Fatal(code, out, stderr)
	}
	requireWithheldAgentConfigResult(t, []string{"replace", "agent-config", "prefs", "--manifest", manifest}, "keys[0].file", hidden)
	if got, err := os.ReadFile(host); err != nil || string(got) != `{"model":"current"}` {
		t.Fatal("refused replacement changed the host", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".daem", "state.json")); !os.IsNotExist(err) {
		t.Fatal("refused replacement published state", err)
	}
}

func TestCaptureNonUTF8SourceAliasRefusesJSONBeforeWriting(t *testing.T) {
	root, manifest, _, _ := captureCLIFixture(t, `{"chosen":"new"}`)
	alias := filepath.Join(t.TempDir(), "alias-\xff")
	if err := os.Symlink(root, alias); err != nil {
		if errors.Is(err, syscall.EILSEQ) {
			t.Skip("filesystem requires UTF-8 names")
		}
		t.Fatal(err)
	}
	const spec = "version=1\ntargets=['pi']\n[[agent_config]]\nid='prefs'\nscope='project'\npath='settings.json'\nsource='./original.json'\n"
	if err := os.WriteFile(manifest, []byte(spec), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "original.json"), []byte(`{"chosen":"old"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	args := []string{"import", "agent-config", "prefs", "--manifest", filepath.Join(alias, "daem.toml"), "--update-source"}
	requireWithheldAgentConfigResult(t, args, "source", "\uFFFD")
	if got, err := os.ReadFile(manifest); err != nil || string(got) != spec {
		t.Fatal("refusal changed the declaration", err)
	}
	if got, err := os.ReadFile(filepath.Join(root, "original.json")); err != nil || string(got) != `{"chosen":"old"}` {
		t.Fatal("refusal replaced the selected source", err)
	}
}
