package cli

import (
	"bytes"
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	declarationcodec "github.com/isty2e/daem/internal/declaration/codec"
)

func TestMCPEnvFromAuthorsNamesAndKeepsCompleteRepeatSemantics(t *testing.T) {
	manifest := mcpEnvManifest(t)
	const source = "DAEM_ENV_FROM_TEST_SOURCE"
	const value = "runtime_value_not_for_metadata_sentinel"
	t.Setenv(source, value)
	args := []string{"--env-from", "childName=" + source, "--env-from", "SECOND=" + source}
	code, output, diagnostic := runMCPEnvAdd(manifest, args...)
	if code != 0 {
		t.Fatalf("add failed: code=%d stdout=%s stderr=%s", code, output, diagnostic)
	}
	before := mcpEnvMetadata(t, manifest)
	blocks, err := declarationcodec.ScanMCPServerBlocks(before[0])
	if err != nil || len(blocks) != 1 {
		t.Fatalf("authored blocks: %v, %v", blocks, err)
	}
	want := map[string]declarationcodec.MCPEnvReference{
		"childName": {FromEnv: source},
		"SECOND":    {FromEnv: source},
	}
	if !maps.Equal(blocks[0].Server.Env, want) {
		t.Fatalf("env references = %#v, want %#v", blocks[0].Server.Env, want)
	}
	for _, content := range [][]byte{before[0], before[1], []byte(output), []byte(diagnostic)} {
		if bytes.Contains(content, []byte(value)) {
			t.Fatal("runtime environment value entered authoring output or metadata")
		}
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(manifest), ".mcp.json")); !os.IsNotExist(err) {
		t.Fatalf("authoring changed host configuration: %v", err)
	}

	code, output, diagnostic = runMCPEnvAdd(manifest, "--env-from", "SECOND="+source, "--env-from", "childName="+source)
	if code != 0 || !bytes.Equal(before[0], mcpEnvMetadata(t, manifest)[0]) {
		t.Fatalf("reordered complete repeat changed declaration: code=%d stdout=%s stderr=%s", code, output, diagnostic)
	}
	for _, changed := range [][]string{
		{"--env-from", "childName=OTHER_SOURCE", "--env-from", "SECOND=" + source},
		{"--env-from", "childName=" + source},
		{"--env-from", "childName=" + source, "--env-from", "SECOND=" + source, "--env-from", "THIRD=" + source},
		nil,
	} {
		prior := mcpEnvMetadata(t, manifest)
		code, _, _ := runMCPEnvAdd(manifest, changed...)
		if code != 1 {
			t.Fatalf("changed complete request code=%d, want operation conflict", code)
		}
		if after := mcpEnvMetadata(t, manifest); !bytes.Equal(prior[0], after[0]) || !bytes.Equal(prior[1], after[1]) {
			t.Fatal("conflicting request changed manifest or lock")
		}
	}
}

func TestMCPEnvFromRefusalsDoNotReflectInputOrChangeMetadata(t *testing.T) {
	manifest := mcpEnvManifest(t)
	if code, output, diagnostic := runMCPEnvAdd(manifest); code != 0 {
		t.Fatalf("baseline add failed: %d %s %s", code, output, diagnostic)
	}
	before := mcpEnvMetadata(t, manifest)
	const marker = "private_env_from_marker_lowercase"
	cases := []struct {
		name string
		args []string
	}{
		{"lowercase source", []string{"--env-from", "CHILD=" + marker}},
		{"github token shape", []string{"--env-from", "CHILD=ghp_" + marker}},
		{"github pat shape", []string{"--env-from", "CHILD=github_pat_" + marker}},
		{"service token shape", []string{"--env-from", "CHILD=sk_live_" + marker}},
		{"hub token shape", []string{"--env-from", "CHILD=hf_" + marker}},
		{"inline source", []string{"--env-from=CHILD=" + marker}},
		{"later invalid source", []string{"--env-from", "FIRST=UPPER_SOURCE", "--env-from", "CHILD=" + marker}},
		{"missing separator", []string{"--env-from", marker}},
		{"source whitespace", []string{"--env-from", "CHILD= " + marker}},
		{"single dash", []string{"-env-from=CHILD=" + marker}},
		{"malformed prefix", []string{"---env-from=CHILD=" + marker}},
		{"malformed name", []string{"--=CHILD=" + marker}},
		{"target captures inline token", []string{"--target", "--env-from=CHILD=" + marker}},
		{"scope captures inline token", []string{"--scope", "--env-from=CHILD=" + marker}},
		{"target captures spaced flag", []string{"--target", "--env-from", "CHILD=" + marker}},
		{"boolean binding", []string{"--dry-run=" + marker}},
	}
	for _, test := range cases {
		for _, mode := range []string{"write", "diff", "json", "json dry run"} {
			t.Run(test.name+"/"+mode, func(t *testing.T) {
				args := slices.Clone(test.args)
				switch mode {
				case "diff":
					args = append(args, "--dry-run", "--diff")
				case "json":
					args = append(args, "--json")
				case "json dry run":
					args = append(args, "--json", "--dry-run")
				}
				code, output, diagnostic := runMCPEnvAdd(manifest, args...)
				if code != 2 {
					t.Fatalf("code=%d, want usage refusal; stdout=%s stderr=%s", code, output, diagnostic)
				}
				if strings.Contains(output+diagnostic, marker) {
					t.Fatal("refusal reflected the submitted marker")
				}
				if strings.HasPrefix(mode, "json") {
					var failure struct {
						ResultType string `json:"result_type"`
						Error      struct{ Kind, Message string }
					}
					if err := json.Unmarshal([]byte(output), &failure); err != nil || failure.ResultType != "cli_error" || failure.Error.Kind != "usage" || diagnostic != "" {
						t.Fatalf("invalid usage envelope: %v, %s, %s", err, output, diagnostic)
					}
				} else if output != "" || diagnostic == "" {
					t.Fatalf("invalid human refusal streams: %q %q", output, diagnostic)
				}
				after := mcpEnvMetadata(t, manifest)
				if !bytes.Equal(before[0], after[0]) || !bytes.Equal(before[1], after[1]) {
					t.Fatal("refused intake changed manifest or lock")
				}
			})
		}
	}
}

func TestMCPEnvFromKeepsLiteralArgAndUppercaseIdentifierBoundaries(t *testing.T) {
	manifest := mcpEnvManifest(t)
	literal := "--env-from=TOKEN=literal_arg_marker_lowercase"
	code, output, diagnostic := runMCPEnvAdd(manifest, "--arg", literal, "--env-from", "TOKEN=UPPER_IDENTIFIER_TEST")
	if code != 0 {
		t.Fatalf("literal argv/uppercase reference rejected: %d %s %s", code, output, diagnostic)
	}
	blocks, err := declarationcodec.ScanMCPServerBlocks(mcpEnvMetadata(t, manifest)[0])
	if err != nil || len(blocks) != 1 || !slices.Equal(blocks[0].Server.Args, []string{literal}) || blocks[0].Server.Env["TOKEN"].FromEnv != "UPPER_IDENTIFIER_TEST" {
		t.Fatalf("literal data or identifier changed: %#v, %v", blocks, err)
	}
}

func TestAuthoringOptionErrorsOmitAssignedValuesAcrossCommands(t *testing.T) {
	const marker = "shared_option_value_marker"
	for _, prefix := range []string{"-unknown=", "---unknown=", "--="} {
		for _, jsonMode := range []bool{false, true} {
			args := []string{"add", "instruction", "guide", "source.md", prefix + marker}
			if jsonMode {
				args = append(args, "--json")
			}
			var stdout, stderr bytes.Buffer
			if code := runWithoutTerminal(args, &stdout, &stderr); code != 2 || strings.Contains(stdout.String()+stderr.String(), marker) {
				t.Fatalf("assigned option value reflected: code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
			}
			if jsonMode && !json.Valid(stdout.Bytes()) {
				t.Fatalf("malformed option changed format recovery: %s", stdout.String())
			}
		}
	}
}

func TestMCPEnvFromUsesExistingPlacementAdmission(t *testing.T) {
	for _, test := range []struct {
		target, scope, child string
		code                 int
	}{
		{"claude-code", "project", "childName", 0},
		{"claude-code", "global", "childName", 0},
		{"opencode", "global", "childName", 0},
		{"opencode", "project", "childName", 1},
		{"codex", "project", "UPPER_SOURCE", 1},
		{"codex", "global", "UPPER_SOURCE", 0},
		{"codex", "global", "childName", 1},
		{"antigravity-cli", "global", "UPPER_SOURCE", 0},
		{"antigravity-cli", "global", "childName", 1},
	} {
		t.Run(test.target+"/"+test.scope+"/"+test.child, func(t *testing.T) {
			manifest := mcpEnvManifest(t)
			original, err := os.ReadFile(manifest)
			if err != nil {
				t.Fatal(err)
			}
			code, output, diagnostic := runMCPEnvAdd(manifest, "--target", test.target, "--scope", test.scope, "--env-from", test.child+"=UPPER_SOURCE")
			if code != test.code {
				t.Fatalf("placement admission changed: %d, want %d; %s %s", code, test.code, output, diagnostic)
			}
			if code != 0 {
				after, err := os.ReadFile(manifest)
				if err != nil || !bytes.Equal(original, after) {
					t.Fatal("unsupported placement changed manifest")
				}
				if _, err := os.Stat(filepath.Join(filepath.Dir(manifest), "daem.lock.toml")); !os.IsNotExist(err) {
					t.Fatalf("unsupported placement created a lock: %v", err)
				}
			}
		})
	}
}

func TestMCPEnvFromDoesNotRestrictManualMixedCaseSourceNames(t *testing.T) {
	manifest := mcpEnvManifest(t)
	content := `version = 1
targets = ["claude-code"]

[[mcp_server]]
name = "manual"
targets = ["claude-code"]
scope = "project"
transport = "stdio"
command = "my-mcp-server"
env = { childName = { from_env = "Mixed_Source" } }
`
	if err := os.WriteFile(manifest, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := runWithoutTerminal([]string{"lock", "--manifest", manifest}, &stdout, &stderr); code != 0 {
		t.Fatalf("manual mixed-case source rejected: %d %s %s", code, stdout.String(), stderr.String())
	}
	if after := mcpEnvMetadata(t, manifest); !bytes.Equal(after[0], []byte(content)) || !bytes.Contains(after[1], []byte("Mixed_Source")) {
		t.Fatal("locking normalized or removed the manual source name")
	}
}

func mcpEnvManifest(t *testing.T) string {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	path := filepath.Join(t.TempDir(), "daem.toml")
	if err := os.WriteFile(path, []byte("version = 1\ntargets = [\"claude-code\"]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func runMCPEnvAdd(manifest string, options ...string) (int, string, string) {
	args := []string{"add", "mcp-server", "env-test", "my-mcp-server", "--manifest", manifest}
	args = append(args, options...)
	var stdout, stderr bytes.Buffer
	code := runWithoutTerminal(args, &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

func mcpEnvMetadata(t *testing.T, manifest string) [2][]byte {
	t.Helper()
	var content [2][]byte
	for index, path := range []string{manifest, filepath.Join(filepath.Dir(manifest), "daem.lock.toml")} {
		var err error
		content[index], err = os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
	}
	return content
}
