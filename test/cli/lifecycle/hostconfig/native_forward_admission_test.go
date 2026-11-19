package cli_test

import (
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/assurance/statefile"
	"github.com/isty2e/daem/internal/encoding/configdocument"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/test/testkit"
	"github.com/isty2e/daem/test/testkit/clijson"
	"github.com/isty2e/daem/test/testkit/nativefixture"
)

func TestNativeForwardRemovalPreservesIndependentMCP(t *testing.T) {
	const mcp = `version=1
targets=['codex']
[[mcp_server]]
name='search'
transport='stdio'
command='echo'
`
	const declaration = `[[agent_config]]
id='preferences'
path='prefs.toml'
source='source.toml'
`
	const mcpValue = "[mcp_servers.search]\ncommand='echo'\n"
	for _, mode := range []string{"disjoint keys", "overlapping keys", "whole file"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			nativefixture.SetRecoveryEnvironment(t, root)
			workspace := filepath.Join(root, "workspace")
			manifest := filepath.Join(workspace, "daem.toml")
			managed := filepath.Join(workspace, ".codex", "config.toml")
			alias := filepath.Join(workspace, ".codex", "prefs.toml")
			run := func(args ...string) string {
				t.Helper()
				return nativefixture.RunLifecycleCommand(t, append(args, "--manifest", manifest)...)
			}
			testkit.WriteFile(t, workspace, "daem.toml", mcp)
			run("lock")
			run("apply", "--yes")
			original := testkit.ReadFile(t, managed)
			source, manifestBody := mcpValue, mcp+declaration
			switch mode {
			case "disjoint keys":
				source = "model='managed'\n"
			case "whole file":
				manifestBody += "ownership='document'\n"
				source = string(original)
			}
			testkit.WriteFile(t, workspace, "source.toml", source)
			testkit.WriteFile(t, workspace, "daem.toml", manifestBody)
			run("lock")
			run("apply", "--yes")

			// Move the MCP route, not the native declaration's recorded
			// referent. Moving that referent would fail earlier on custody.
			if mode != "whole file" {
				combined := string(original)
				if mode == "disjoint keys" {
					combined = source + combined
				}
				testkit.WriteFile(t, filepath.Dir(alias), filepath.Base(alias), combined)
			}
			if err := os.Remove(managed); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(filepath.Base(alias), managed); err != nil {
				t.Fatal(err)
			}
			testkit.WriteFile(t, workspace, "daem.toml", mcp)
			run("lock")
			paths, err := daempaths.Resolve(manifest)
			if err != nil {
				t.Fatal(err)
			}
			fixture := nativefixture.RecoveryFixture{Manifest: manifest, Paths: paths, Destination: alias}
			if mode != "disjoint keys" {
				before := nativefixture.SnapshotRecoveryEvidence(t, fixture)
				for _, action := range []string{"--dry-run", "--yes"} {
					code, stdout, stderr := nativefixture.InvokeRecoveryCommand(t, fixture, "none", "apply", action, "--json")
					if code != 1 {
						t.Fatalf("overlapping removal %s was not refused: %d %s %s", action, code, stdout, stderr)
					}
					var message string
					if action == "--dry-run" {
						failure := clijson.DecodeFailure(t, stdout, "apply", "operation")
						message = failure.Error.Message
						unmanage := []string{"daem", "unmanage", "agent-config", "preferences", "--dry-run"}
						if !slices.ContainsFunc(failure.Guidance, func(guidance clijson.FailureGuidance) bool { return slices.Equal(unmanage, guidance.Argv) }) {
							t.Fatal("refusal did not explain how to preserve the shared file")
						}
					} else {
						plan := clijson.DecodeApplyResult(t, []byte(stdout))
						if !plan.HasErrors || len(plan.Errors) != 1 || plan.Errors[0].Code != "apply_refused" {
							t.Fatalf("overlapping removal did not refuse before effects: %v", plan.Errors)
						}
						message = plan.Errors[0].Message
					}
					if !strings.Contains(message, "changes at ") || !strings.Contains(message, "config.toml") || !strings.Contains(message, "search") {
						t.Fatalf("refusal did not identify the retained MCP output: %s", message)
					}
					if !maps.Equal(before, nativefixture.SnapshotRecoveryEvidence(t, fixture)) {
						t.Fatal("refused removal changed file, state, registry or journal")
					}
					if target, err := os.Readlink(managed); err != nil || target != filepath.Base(alias) {
						t.Fatal("refusal changed the MCP route", err)
					}
				}
				run("unmanage", "agent-config", "preferences")
			} else {
				run("apply", "--yes")
				nativefixture.RequireSharedValues(t, configdocument.TOML, testkit.ReadFile(t, managed), source, false)
			}
			nativefixture.RequireSharedValues(t, configdocument.TOML, testkit.ReadFile(t, managed), mcpValue, true)
			state, err := statefile.DecodePersistedFacts(t.Context(), testkit.ReadFile(t, paths.StatefilePath))
			if err != nil || len(state.ManagedAggregates()) != 1 || len(state.ManagedPaths()) != 0 {
				t.Fatal("retiring native management did not preserve MCP-only ownership", err)
			}
			testkit.AssertNoRecoveryArtifacts(t, workspace)
		})
	}
}

func TestNativeReplacementOnlyBindsSelectedDocument(t *testing.T) {
	root := t.TempDir()
	nativefixture.SetRecoveryEnvironment(t, root)
	workspace := filepath.Join(root, "workspace")
	manifest := filepath.Join(workspace, "daem.toml")
	selected := filepath.Join(workspace, ".codex", "prefs.toml")
	other := filepath.Join(workspace, ".codex", "other.toml")
	testkit.WriteFile(t, workspace, "daem.toml", `version=1
targets=['codex']
[[agent_config]]
id='preferences'
path='prefs.toml'
source='preferences.toml'
[[agent_config]]
id='other'
path='other.toml'
source='other.toml'
`)
	testkit.WriteFile(t, workspace, "preferences.toml", "model='managed'\nnotify=true\n")
	testkit.WriteFile(t, workspace, "other.toml", "model='separate'\n")
	nativefixture.RunLifecycleCommand(t, "lock", "--manifest", manifest)
	nativefixture.RunLifecycleCommand(t, "apply", "--yes", "--manifest", manifest)
	testkit.WriteFile(t, filepath.Dir(selected), filepath.Base(selected), "model='local'\nnotify=true\n")
	paths, err := daempaths.Resolve(manifest)
	if err != nil {
		t.Fatal(err)
	}
	fixture := nativefixture.RecoveryFixture{Manifest: manifest, Paths: paths, Destination: selected}
	fingerprint := nativeReplacementFingerprint(t, fixture, "/model")
	if err := os.Chmod(other, 0o640); err != nil {
		t.Fatal(err)
	}
	if got := nativeReplacementFingerprint(t, fixture, "/model"); got != fingerprint {
		t.Fatal("mode change in a separate document invalidated selected-key approval")
	}
	if err := os.Remove(other); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(other, 0o700); err != nil {
		t.Fatal(err)
	}
	_, stdout, _ := nativefixture.RunRecoveryCommand(t, fixture, "none", "replace", "agent-config", "preferences", "--key", "/model", "--dry-run", "--json")
	var disclosure struct {
		Fingerprint string            `json:"fingerprint"`
		Keys        []json.RawMessage `json:"keys"`
	}
	if err := json.Unmarshal([]byte(stdout), &disclosure); err != nil || disclosure.Fingerprint != fingerprint || len(disclosure.Keys) != 1 {
		t.Fatalf("separate directory affected the approved selection: %v %s", err, stdout)
	}
	nativefixture.RunRecoveryCommand(t, fixture, "none", "replace", "agent-config", "preferences", "--key", "/model", "--yes", "--expect-plan", fingerprint)
	nativefixture.RequireSharedValues(t, configdocument.TOML, testkit.ReadFile(t, selected), "model='managed'\nnotify=true\n", true)
	if info, err := os.Stat(other); err != nil || !info.IsDir() {
		t.Fatal("selected replacement changed the separate directory", err)
	}
}

func TestNativeReplacementUsesResolvedDocumentPermissions(t *testing.T) {
	for _, location := range []string{"MCP role", "global location"} {
		t.Run(location, func(t *testing.T) {
			root := t.TempDir()
			nativefixture.SetRecoveryEnvironment(t, root)
			workspace := filepath.Join(root, "workspace")
			manifest := filepath.Join(workspace, "daem.toml")
			destination := filepath.Join(workspace, ".codex", "prefs.toml")
			testkit.WriteFile(t, workspace, "daem.toml", `version=1
targets=['codex']
[[agent_config]]
id='preferences'
path='prefs.toml'
source='preferences.toml'
`)
			source, key := "model='managed'\n", "/model"
			if location == "MCP role" {
				source, key = "[mcp_servers.search]\ncommand='echo'\n", "/mcp_servers/search/command"
			}
			testkit.WriteFile(t, workspace, "preferences.toml", source)
			nativefixture.RunLifecycleCommand(t, "lock", "--manifest", manifest)
			nativefixture.RunLifecycleCommand(t, "apply", "--yes", "--manifest", manifest)
			link := filepath.Join(filepath.Dir(destination), "config.toml")
			expected, repair := "the same file is codex", "dedicated resource for reserved entries"
			if location == "global location" {
				link = filepath.Join(os.Getenv("CODEX_HOME"), "config.toml")
				expected, repair = "project claims do not grant global write authority", "outside global host configuration"
			}
			if err := os.Symlink(destination, link); err != nil {
				t.Fatal(err)
			}
			paths, err := daempaths.Resolve(manifest)
			if err != nil {
				t.Fatal(err)
			}
			fixture := nativefixture.RecoveryFixture{Manifest: manifest, Paths: paths, Destination: destination}
			before := nativefixture.SnapshotRecoveryEvidence(t, fixture)
			code, stdout, stderr := nativefixture.InvokeRecoveryCommand(t, fixture, "none", "replace", "agent-config", "preferences", "--key", key, "--dry-run", "--json")
			if code != 1 {
				t.Fatalf("replacement issued approval for a forbidden %s: %d %s %s", location, code, stdout, stderr)
			}
			failure := clijson.DecodeFailure(t, stdout, "replace agent-config", "operation")
			if !strings.Contains(failure.Error.Message, expected) || !slices.ContainsFunc(failure.Guidance, func(guidance clijson.FailureGuidance) bool { return strings.Contains(guidance.Message, repair) }) {
				t.Fatalf("wrong refusal for %s: %+v", location, failure)
			}
			if !maps.Equal(before, nativefixture.SnapshotRecoveryEvidence(t, fixture)) {
				t.Fatal("replacement changed host or persisted state on refusal")
			}
		})
	}
}

func TestNativeExactAdoptionCannotClaimUnselectedOrdinaryFile(t *testing.T) {
	root := t.TempDir()
	nativefixture.SetRecoveryEnvironment(t, root)
	workspace := filepath.Join(root, "workspace")
	manifest := filepath.Join(workspace, "daem.toml")
	const base = `version=1
targets=['codex','claude-code']
[instructions.project]
source='instruction.md'
targets=['claude-code']
[instructions.project.target.claude-code]
render_to='CLAUDE.md'
mode='copy'
`
	const content = "model='managed'\n"
	testkit.WriteFile(t, workspace, "instruction.md", content)
	testkit.WriteFile(t, workspace, "daem.toml", base)
	nativefixture.RunLifecycleCommand(t, "lock", "--manifest", manifest)
	nativefixture.RunLifecycleCommand(t, "apply", "--yes", "--manifest", manifest)
	ordinary := filepath.Join(workspace, "CLAUDE.md")
	testkit.AssertFileContent(t, ordinary, content)
	if err := os.Mkdir(filepath.Join(workspace, ".codex"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../CLAUDE.md", filepath.Join(workspace, ".codex", "prefs.toml")); err != nil {
		t.Fatal(err)
	}
	testkit.WriteFile(t, workspace, "preferences.toml", content)
	testkit.WriteFile(t, workspace, "daem.toml", base+`
[[agent_config]]
id='preferences'
targets=['codex']
path='prefs.toml'
source='preferences.toml'
`)
	nativefixture.RunLifecycleCommand(t, "lock", "--manifest", manifest)
	paths, err := daempaths.Resolve(manifest)
	if err != nil {
		t.Fatal(err)
	}
	fixture := nativefixture.RecoveryFixture{Manifest: manifest, Paths: paths, Destination: ordinary}
	before := nativefixture.SnapshotRecoveryEvidence(t, fixture)
	code, stdout, stderr := nativefixture.InvokeRecoveryCommand(t, fixture, "none", "apply", "--target", "codex", "--manage-existing", "--dry-run", "--json")
	if code != 1 {
		t.Fatalf("exact-match adoption acquired an unselected ordinary file: %d %s %s", code, stdout, stderr)
	}
	failure := clijson.DecodeFailure(t, stdout, "apply", "operation")
	if !strings.Contains(failure.Error.Message, "overlap managed output") || !strings.Contains(failure.Error.Message, "CLAUDE.md") || !strings.Contains(failure.Error.Message, "claude-code") {
		t.Fatalf("adoption did not identify the protected managed file: %+v", failure)
	}
	if !maps.Equal(before, nativefixture.SnapshotRecoveryEvidence(t, fixture)) {
		t.Fatal("refused acquisition changed file or persisted ownership")
	}
}
