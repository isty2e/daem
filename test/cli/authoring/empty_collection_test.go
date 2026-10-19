package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	clipkg "github.com/isty2e/daem/internal/cli"
	declarationmanifest "github.com/isty2e/daem/internal/declaration/manifest"
	"github.com/isty2e/daem/internal/effect/execute/delegate"
	"github.com/isty2e/daem/internal/platformsupport"
	"github.com/isty2e/daem/internal/subprocess"
	applyworkflow "github.com/isty2e/daem/internal/workflow/apply"
	"github.com/isty2e/daem/test/testkit"
	"github.com/isty2e/daem/test/testkit/clijson"
)

const emptyCollectionManifest = `# retain this heading
version = 1
targets = ["codex"]
instructions = {}
skill_group = []
skill = []
hook = []
mcp_server = []
extension = []
# retain this footer
`

func TestAddFirstResourceToExplicitEmptyCollections(t *testing.T) {
	for _, test := range []struct {
		name   string
		args   []string
		counts [5]int
	}{
		{name: "skill", args: []string{"add", "skill", "source/demo", "--target", "codex"}, counts: [5]int{1}},
		{name: "skill group", args: []string{"add", "skill-group", "source/group", "--member", "one", "--member", "two", "--target", "codex"}, counts: [5]int{2}},
		{name: "instruction", args: []string{"add", "instruction", "guide", "source/guide.md", "--target", "codex"}, counts: [5]int{0, 1}},
		{name: "hook", args: []string{"add", "hook", "demo", "PostToolUse", "must-not-run-daem-test", "--target", "claude-code"}, counts: [5]int{0, 0, 1}},
		{name: "extension", args: []string{"add", "extension", "demo", "npm:@acme/demo@1.0.0", "--target", "pi", "--scope", "project"}, counts: [5]int{0, 0, 0, 1}},
		{name: "MCP", args: []string{"add", "mcp-server", "demo", "must-not-run-daem-test", "--target", "claude-code", "--scope", "project"}, counts: [5]int{0, 0, 0, 0, 1}},
		{name: "MCP with Pi provider", args: []string{"add", "mcp-server", "demo", "must-not-run-daem-test", "--target", "pi", "--scope", "project"}, counts: [5]int{0, 0, 0, 1, 1}},
	} {
		t.Run(test.name, func(t *testing.T) {
			root, run := emptyCollectionAuthoringCLI(t)
			manifest := filepath.Join(root, "daem.toml")
			lock := filepath.Join(root, "daem.lock.toml")
			testkit.WriteFile(t, root, "daem.toml", emptyCollectionManifest)
			testkit.WriteFile(t, root, "source/demo/SKILL.md", "---\nname: demo\ndescription: fixture\n---\n")
			for _, name := range []string{"one", "two"} {
				testkit.WriteFile(t, root, "source/group/"+name+"/SKILL.md", "---\nname: "+name+"\ndescription: fixture\n---\n")
			}
			testkit.WriteFile(t, root, "source/guide.md", "Fixture instructions.\n")

			preview := run(0, append(append([]string{}, test.args...), "--dry-run", "--json")...)
			if report := clijson.DecodeManifestAuthoring(t, preview); report.HasErrors || report.ChangeCount == 0 {
				t.Fatalf("empty collection did not produce an add plan: %s", preview)
			}
			testkit.AssertFileContent(t, manifest, emptyCollectionManifest)
			testkit.AssertPathMissing(t, lock)

			result := run(0, append(append([]string{}, test.args...), "--json")...)
			if clijson.DecodeManifestAuthoring(t, result).HasErrors {
				t.Fatalf("add reported errors: %s", result)
			}
			content := testkit.ReadFile(t, manifest)
			environment, err := declarationmanifest.Decode(content)
			if err != nil {
				t.Fatal(err)
			}
			counts := [5]int{len(environment.Skills()), len(environment.Instructions()), len(environment.Hooks()), len(environment.Extensions()), len(environment.MCPServers())}
			if counts != test.counts || !bytes.HasPrefix(content, []byte("# retain this heading\n")) || !bytes.Contains(content, []byte("# retain this footer\n")) {
				t.Fatalf("resource counts=%v want=%v, manifest=%s", counts, test.counts, content)
			}
			locked := testkit.ReadFile(t, lock)
			run(0, append(append([]string{}, test.args...), "--json")...)
			if !bytes.Equal(content, testkit.ReadFile(t, manifest)) || !bytes.Equal(locked, testkit.ReadFile(t, lock)) {
				t.Fatal("repeated add changed manifest or lock bytes")
			}
			testkit.AssertPathMissing(t, filepath.Join(root, ".agents", "skills"))
			testkit.AssertPathMissing(t, filepath.Join(root, ".mcp.json"))
		})
	}
}

func TestImportProducedEmptyCollectionsAcceptFirstSkill(t *testing.T) {
	root, run := emptyCollectionAuthoringCLI(t)
	settings := `{"packages":["npm:@acme/seed@1.0.0"]}`
	testkit.WriteFile(t, root, ".pi/settings.json", settings)
	run(0, "import", "--target", "pi", "--scope", "project", "--json")
	testkit.WriteFile(t, root, "source/demo/SKILL.md", "---\nname: demo\ndescription: fixture\n---\n")
	run(0, "add", "skill", "source/demo", "--target", "codex", "--json")
	environment, err := declarationmanifest.Decode(testkit.ReadFile(t, filepath.Join(root, "daem.toml")))
	if err != nil || len(environment.Skills()) != 1 || len(environment.Extensions()) != 1 {
		t.Fatalf("import then add lost declarations: err=%v", err)
	}
	testkit.AssertFileContent(t, filepath.Join(root, ".pi", "settings.json"), settings)
}

func TestImportMergeAddsIntoExplicitEmptyCollections(t *testing.T) {
	for _, selected := range []string{"codex", "pi"} {
		t.Run(selected, func(t *testing.T) {
			root, run := emptyCollectionAuthoringCLI(t)
			testkit.WriteFile(t, root, "daem.toml", emptyCollectionManifest)
			testkit.WriteFile(t, root, "AGENTS.md", "Fixture instructions.\n")
			testkit.WriteFile(t, root, ".agents/skills/imported/SKILL.md", "---\nname: imported\ndescription: fixture\n---\n")
			settings := `{"packages":["npm:@acme/zulu@1.0.0","npm:@acme/alpha@1.0.0"]}`
			testkit.WriteFile(t, root, ".pi/settings.json", settings)
			run(0, "import", "--target", selected, "--scope", "project", "--merge", "--json")
			content := testkit.ReadFile(t, filepath.Join(root, "daem.toml"))
			environment, err := declarationmanifest.Decode(content)
			if err != nil {
				t.Fatal(err)
			}
			if selected == "codex" && (len(environment.Instructions()) != 1 || len(environment.Skills()) != 1) {
				t.Fatal("empty collections did not retain the instruction and add the skill")
			}
			if selected == "pi" && (len(environment.Extensions()) != 2 || strings.Index(string(content), "npm:@acme/zulu@1.0.0") >= strings.Index(string(content), "npm:@acme/alpha@1.0.0")) {
				t.Fatalf("empty extension collection lost declarations/order: %s", content)
			}
			testkit.AssertFileContent(t, filepath.Join(root, "AGENTS.md"), "Fixture instructions.\n")
			testkit.AssertFileContent(t, filepath.Join(root, ".pi", "settings.json"), settings)
		})
	}
}

func TestEmptyCollectionRepairDoesNotAdmitInvalidOriginal(t *testing.T) {
	for _, original := range []string{
		"version = 1\ntargets = [\"codex\"]\nskill = []\nskill = []\n",
		"version = 1\ntargets = [\"codex\"]\nskill = {}\n",
	} {
		t.Run(original, func(t *testing.T) {
			root, run := emptyCollectionAuthoringCLI(t)
			manifest := filepath.Join(root, "daem.toml")
			lock := filepath.Join(root, "daem.lock.toml")
			testkit.WriteFile(t, root, "daem.toml", original)
			testkit.WriteFile(t, root, "source/demo/SKILL.md", "---\nname: demo\ndescription: fixture\n---\n")
			testkit.WriteLockfile(t, lock, testkit.ExactSupplyLockfile(t))
			beforeLock := testkit.ReadFile(t, lock)
			failure := run(1, "add", "skill", "source/demo", "--target", "codex", "--json")
			if !json.Valid(failure) {
				t.Fatalf("invalid original lost its JSON failure: %s", failure)
			}
			testkit.AssertFileContent(t, manifest, original)
			if !bytes.Equal(beforeLock, testkit.ReadFile(t, lock)) {
				t.Fatal("invalid original changed lock bytes")
			}
		})
	}
}

func emptyCollectionAuthoringCLI(t *testing.T) (string, func(int, ...string) []byte) {
	t.Helper()
	root := isolatedExtensionImportCLI(t)
	t.Setenv("PATH", "")
	version, err := platformsupport.ParseMacOSProductVersion("26.0")
	if err != nil {
		t.Fatal(err)
	}
	platform, err := platformsupport.NewRuntimeObservation(version)
	if err != nil {
		t.Fatal(err)
	}
	var attempts atomic.Int32
	deny := func(context.Context, subprocess.CommandRequest) subprocess.CommandResult {
		attempts.Add(1)
		return subprocess.CommandResult{}
	}
	options := clipkg.RunOptions{
		PlatformObserver: func(context.Context) (platformsupport.RuntimeObservation, error) { return platform, nil },
		ApplyExecuteOptions: applyworkflow.ExecuteOptions{
			HostRouteExecutor: subprocess.NewCommandExecutor(subprocess.CommandOptions{Runner: deny}),
			DelegateExecutor:  delegate.NewExecutor(delegate.Options{Runner: deny}),
		},
	}
	return root, func(want int, args ...string) []byte {
		t.Helper()
		var stdout, stderr bytes.Buffer
		invocation := options
		invocation.Stdout, invocation.Stderr = &stdout, &stderr
		code := testkit.RunCLIWithOptions(append(args, "--manifest", filepath.Join(root, "daem.toml")), invocation)
		if code != want || attempts.Load() != 0 {
			t.Fatalf("%v exit=%d want=%d stdout=%s stderr=%s host calls=%d", args, code, want, &stdout, &stderr, attempts.Load())
		}
		return bytes.Clone(stdout.Bytes())
	}
}
