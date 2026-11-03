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
	codexhostpath "github.com/isty2e/daem/internal/output/hostpath/codex"
	pihostpath "github.com/isty2e/daem/internal/output/hostpath/pi"
)

func captureCLIFixture(t *testing.T, content string) (root, manifest, host string, args []string) {
	t.Helper()
	root = t.TempDir()
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
	args = []string{"import", "host-config", "chosen", "--manifest", manifest, "--target", "pi", "--scope", "project", "--host-path", "settings.json"}
	return
}

func runCaptureCLI(t *testing.T, args []string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := RunWithOptions(args, RunOptions{Context: t.Context(), Stdout: &stdout, Stderr: &stderr})
	return code, stdout.String(), stderr.String()
}

func captureFingerprint(t *testing.T, args []string) string {
	t.Helper()
	code, stdout, stderr := runCaptureCLI(t, append(append([]string{}, args...), "--dry-run", "--json"))
	if code != 0 {
		t.Fatalf("preview: code=%d out=%s err=%s", code, stdout, stderr)
	}
	var payload struct {
		Fingerprint string `json:"fingerprint"`
	}
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil || len(payload.Fingerprint) != 64 {
		t.Fatal("invalid approval preview", stdout, err)
	}
	return payload.Fingerprint
}

func TestImportHostConfigLiteralStorageAndMaskedSelectedDisclosure(t *testing.T) {
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
	manifestBytes, err := os.ReadFile(manifest)
	if err != nil {
		t.Fatal(err)
	}
	environment, err := declarationmanifest.Decode(manifestBytes)
	if err != nil || len(environment.HostConfigs()) != 1 {
		t.Fatal("invalid resulting declaration", err)
	}
	local, _ := environment.HostConfigs()[0].Source().Local()
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

func TestImportHostConfigFingerprintUsesActualMaskedValues(t *testing.T) {
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

func TestImportHostConfigUpdateReleasesMissingLeafWithoutAcquiringSiblings(t *testing.T) {
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
	local, _ := environment.HostConfigs()[0].Source().Local()
	if err := os.WriteFile(host, []byte(`{"nested":{"new":9},"sibling":2}`), 0o600); err != nil {
		t.Fatal(err)
	}
	update := []string{"import", "host-config", "chosen", "--manifest", manifest, "--update-source"}
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

func TestImportHostConfigRefusesOutputFailureBeforeConfirmationOrWrites(t *testing.T) {
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

func TestImportHostConfigRejectsMixedRequestGrammarAndUnapprovedWrites(t *testing.T) {
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

func TestImportHostConfigInitialUsesAdmittedRootsAndGrammar(t *testing.T) {
	for _, selected := range []string{"pi", "codex"} {
		for _, scope := range []string{"project", "global"} {
			t.Run(selected+"/"+scope, func(t *testing.T) {
				root, manifest, _, _ := captureCLIFixture(t, `{}`)
				hostRoot := filepath.Join(root, "."+selected)
				if scope == "global" {
					hostRoot = filepath.Join(root, "native-home")
					if selected == "pi" {
						t.Setenv(pihostpath.AgentRootEnvironmentVariable, hostRoot)
					} else {
						t.Setenv(codexhostpath.HomeEnvironmentVariable, hostRoot)
					}
				}
				path, content := "settings.json", `{"capture":{"value":true},"unselected":1}`
				if selected == "codex" {
					path, content = "config.toml", "# host comment\nunselected = 1\n[capture]\nvalue = true\n"
				}
				host := filepath.Join(hostRoot, path)
				if err := os.MkdirAll(hostRoot, 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(host, []byte(content), 0o600); err != nil {
					t.Fatal(err)
				}
				args := []string{"import", "host-config", "chosen", "--manifest", manifest, "--target", selected, "--scope", scope, "--host-path", path, "--key", "/capture"}
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
				config := environment.HostConfigs()[0]
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

func TestImportHostConfigWholeInitialPreservesBytesAndOmitsRawDisclosure(t *testing.T) {
	root, manifest, _, _ := captureCLIFixture(t, `{}`)
	host := filepath.Join(root, ".codex", "whole.jsonc")
	content := "/* private-host-comment */\n{ \"api_key\" : \"whole-literal-private\" }\n"
	if err := os.MkdirAll(filepath.Dir(host), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(host, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	args := []string{"import", "host-config", "whole", "--manifest", manifest, "--target", "codex", "--scope", "project", "--host-path", "whole.jsonc", "--ownership", "document"}
	code, out, errText := runCaptureCLI(t, append(append([]string{}, args...), "--dry-run", "--diff"))
	if code != 0 || strings.Contains(out+errText, "whole-literal-private") || strings.Contains(out+errText, "private-host-comment") {
		t.Fatal("whole preview leaked raw contents", code, out, errText)
	}
	fingerprint := captureFingerprint(t, args)
	if code, out, err := runCaptureCLI(t, append(args, "--yes", "--expect-plan", fingerprint)); code != 0 {
		t.Fatal(code, out, err)
	}
	stored, err := os.ReadFile(filepath.Join(root, "daem.d", "host-config", "whole.jsonc"))
	if err != nil || string(stored) != content {
		t.Fatal("whole capture reserialized bytes", err)
	}
}
