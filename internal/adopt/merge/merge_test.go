package merge

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/adopt"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/target"
)

type mergeTestInput struct {
	Merge           bool
	OriginalContent []byte
	Sources         []adopt.Source
	Skills          []adopt.Skill
	Hooks           []adopt.Hook
	MCPServers      []adopt.MCPServer
}

func requireContains(t *testing.T, content string, fragment string) {
	t.Helper()
	if !strings.Contains(content, fragment) {
		t.Fatalf("content = %q, want fragment %q", content, fragment)
	}
}

func TestIntoManifestReportsSameNameInstructionConflictWithoutDroppingPlan(t *testing.T) {
	plan := mergeTestInput{
		Merge: true,
		OriginalContent: []byte(`version = 1
targets = ["codex"]

[instructions.codex_project]
source = "other.md"
targets = ["codex"]
scope = "project"
`),
		Sources: []adopt.Source{{
			ResourceName: "codex_project",
			Target:       target.TargetCodex,
			Scope:        target.ScopeProject,
			SourcePath:   "daem.d/instructions/codex-project.md",
		}},
	}

	merged, err := mergeTestPlan(t, plan)
	if err != nil {
		t.Fatal(err)
	}
	if !merged.HasMergeConflicts() {
		t.Fatalf("merge results = %#v, want conflict", merged.MergeResults())
	}
	if len(merged.Sources()) != 0 {
		t.Fatalf("sources = %#v, want conflicting source removed from writable additions", merged.Sources())
	}
	if string(merged.ManifestContent()) != string(plan.OriginalContent) {
		t.Fatalf("manifest content changed on conflict")
	}
}

func TestIntoManifestMergesMissingInstructionTargetThroughDeclarationPatch(t *testing.T) {
	plan := mergeTestInput{
		Merge: true,
		OriginalContent: []byte(`version = 1
targets = ["codex", "claude-code"]

[instructions.codex_project]
source = "daem.d/instructions/codex-project.md"
targets = ["codex"]
scope = "project"
`),
		Sources: []adopt.Source{{
			ResourceName: "codex_project",
			Target:       target.TargetClaudeCode,
			Scope:        target.ScopeProject,
			SourcePath:   "daem.d/instructions/codex-project.md",
		}},
	}

	merged, err := mergeTestPlan(t, plan)
	if err != nil {
		t.Fatal(err)
	}
	if merged.HasMergeConflicts() {
		t.Fatalf("merge results = %#v, want no conflict", merged.MergeResults())
	}
	if len(merged.Sources()) != 0 {
		t.Fatalf("sources = %#v, want target merge instead of new source", merged.Sources())
	}
	if !strings.Contains(string(merged.ManifestContent()), `targets = ["codex", "claude-code"]`) {
		t.Fatalf("manifest content =\n%s", merged.ManifestContent())
	}
}

func TestIntoManifestReportsInstructionRenderingConflict(t *testing.T) {
	plan := mergeTestInput{
		Merge: true,
		OriginalContent: []byte(`version = 1
targets = ["antigravity-cli"]

[instructions.antigravity_cli_project_gemini]
source = "daem.d/instructions/antigravity-cli-project-gemini.md"
targets = ["antigravity-cli"]
scope = "project"

[instructions.antigravity_cli_project_gemini.target.antigravity-cli]
render_to = "OTHER.md"
`),
		Sources: []adopt.Source{{
			ResourceName: "antigravity_cli_project_gemini",
			Target:       target.TargetAntigravityCLI,
			Scope:        target.ScopeProject,
			SourcePath:   "daem.d/instructions/antigravity-cli-project-gemini.md",
			RenderTo:     "GEMINI.md",
		}},
	}

	merged, err := mergeTestPlan(t, plan)
	if err != nil {
		t.Fatal(err)
	}
	if !merged.HasMergeConflicts() {
		t.Fatalf("merge results = %#v, want rendering conflict", merged.MergeResults())
	}
	if len(merged.Sources()) != 0 {
		t.Fatalf("sources = %#v, want conflicting source removed from writable additions", merged.Sources())
	}
	if string(merged.ManifestContent()) != string(plan.OriginalContent) {
		t.Fatalf("manifest content changed on rendering conflict")
	}
	if got := merged.MergeResults()[0].Detail; !strings.Contains(got, "different source, scope, or rendering") {
		t.Fatalf("detail = %q, want rendering conflict detail", got)
	}
}

func TestScanExistingDeclarationsHandlesNestedAndCommentedTables(t *testing.T) {
	content := []byte(`version = 1
targets = ["codex", "claude-code"]

[instructions."project.daily"]
source = "daem.d/instructions/project-daily.md"
targets = ["codex"]

[instructions."project.daily".target."codex"] # user-authored comment
render_to = "AGENTS.md"

[[skill]] # user-authored comment
name = "review"
targets = ["codex"]
scope = "project"

[skill.source]
path = "daem.d/skills/review"
mode = "vendor"

[[hook]]
name = "lint"
event = "PreToolUse"
matcher = "Write"
command = "make lint"
targets = ["codex"]

[[hook.target_override]] # user-authored comment
target = "codex"
matcher = "Edit"
`)

	existing, err := scanExistingDeclarations(content)
	if err != nil {
		t.Fatalf("scanExistingDeclarations returned error: %v", err)
	}
	if len(existing.Instructions) != 1 || len(existing.Skills) != 1 || len(existing.Hooks) != 1 {
		t.Fatalf("existing = %#v, want one instruction, skill, and hook", existing)
	}
	if existing.Skills[0].Skill.Source.Path != "daem.d/skills/review" {
		t.Fatalf("skill source = %#v, want nested source", existing.Skills[0].Skill.Source)
	}
	if len(existing.Hooks[0].Hook.TargetOverrides) != 1 {
		t.Fatalf("hook overrides = %#v, want nested override", existing.Hooks[0].Hook.TargetOverrides)
	}
}

func TestIntoManifestMergesSkillTargetThroughNestedSourceBlock(t *testing.T) {
	plan := mergeTestInput{
		Merge: true,
		OriginalContent: []byte(`version = 1
targets = ["codex", "claude-code"]

[[skill]] # user-authored comment
name = "review"
targets = ["codex"]
scope = "project"

[skill.source]
path = "daem.d/skills/review"
mode = "vendor"
`),
		Skills: []adopt.Skill{{
			ResourceName: "review",
			InstallName:  "review",
			Target:       target.TargetClaudeCode,
			Targets:      []target.Target{target.TargetClaudeCode},
			Scope:        target.ScopeProject,
			SourcePath:   "daem.d/skills/review",
		}},
	}

	merged, err := mergeTestPlan(t, plan)
	if err != nil {
		t.Fatal(err)
	}
	if merged.HasMergeConflicts() {
		t.Fatalf("merge results = %#v, want no conflict", merged.MergeResults())
	}
	if len(merged.Skills()) != 0 {
		t.Fatalf("skills = %#v, want target merge instead of new skill", merged.Skills())
	}
	requireContains(t, string(merged.ManifestContent()), `targets = ["codex", "claude-code"]`)
	requireContains(t, string(merged.ManifestContent()), `[skill.source]`)
}

func TestIntoManifestMergesHookTargetWithoutReencodingExistingBlock(t *testing.T) {
	plan := mergeTestInput{
		Merge: true,
		OriginalContent: []byte("version = 1\r\n" +
			"targets = [\"codex\", \"claude-code\"]\r\n" +
			"\r\n" +
			"[[hook]] # keep header\r\n" +
			"name = 'lint'\r\n" +
			"event = 'PreToolUse'\r\n" +
			"type = 'command'\r\n" +
			"command = 'make lint' # keep command\r\n" +
			"targets = ['codex']\r\n" +
			"scope = 'project'"),
		Hooks: []adopt.Hook{{
			ResourceName: "lint",
			Target:       target.TargetClaudeCode,
			Scope:        target.ScopeProject,
			Event:        "PreToolUse",
			Command:      "make lint",
			Condition:    "always",
		}},
	}

	merged, err := mergeTestPlan(t, plan)
	if err != nil {
		t.Fatal(err)
	}
	if merged.HasMergeConflicts() {
		t.Fatalf("merge results = %#v, want no conflict", merged.MergeResults())
	}
	got := string(merged.ManifestContent())
	for _, want := range []string{
		"[[hook]] # keep header\r\n",
		"command = 'make lint' # keep command\r\n",
		`targets = ["codex", "claude-code"]`,
		`target = "claude-code"`,
		`if = "always"`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("content = %q, want %q", got, want)
		}
	}
	if strings.Contains(strings.ReplaceAll(got, "\r\n", ""), "\n") {
		t.Fatalf("content contains mixed line endings: %q", got)
	}
	if strings.HasSuffix(got, "\n") {
		t.Fatalf("terminal newline was added: %q", got)
	}
}

func TestIntoManifestReportsSameDestinationSkillConflict(t *testing.T) {
	plan := mergeTestInput{
		Merge: true,
		OriginalContent: []byte(`version = 1
targets = ["codex"]

[[skill]]
id = "existing"
name = "review"
source = { path = "daem.d/skills/review/sha256-old", mode = "vendor" }
targets = ["codex"]
scope = "project"
`),
		Skills: []adopt.Skill{{
			ResourceName: "incoming",
			InstallName:  "review",
			Target:       target.TargetCodex,
			Targets:      []target.Target{target.TargetCodex},
			Scope:        target.ScopeProject,
			SourcePath:   "daem.d/skills/review/sha256-new",
		}},
	}

	merged, err := mergeTestPlan(t, plan)
	if err != nil {
		t.Fatal(err)
	}
	if !merged.HasMergeConflicts() {
		t.Fatalf("merge results = %#v, want conflict", merged.MergeResults())
	}
	if got := merged.MergeResults()[0].Detail; !strings.Contains(got, "already used by skill id") {
		t.Fatalf("detail = %q", got)
	}
}

func TestIntoManifestAppendsMCPServer(t *testing.T) {
	plan := mergeTestInput{
		Merge: true,
		OriginalContent: []byte(`version = 1
targets = ["claude-code"]
`),
		MCPServers: []adopt.MCPServer{{
			ResourceName: "context7",
			Target:       target.TargetClaudeCode,
			Scope:        target.ScopeProject,
			Command:      "npx",
			Args:         []string{"-y", "@upstash/context7-mcp"},
			Env:          map[string]string{"TOKEN": "TOKEN"},
		}},
	}

	merged, err := mergeTestPlan(t, plan)
	if err != nil {
		t.Fatal(err)
	}
	if merged.HasMergeConflicts() {
		t.Fatalf("merge results = %#v, want no conflict", merged.MergeResults())
	}
	if len(merged.MCPServers()) != 1 {
		t.Fatalf("mcp servers = %#v, want appended server", merged.MCPServers())
	}
	requireContains(t, string(merged.ManifestContent()), `[[mcp_server]]`)
	requireContains(t, string(merged.ManifestContent()), `name = "context7"`)
	requireContains(t, string(merged.ManifestContent()), `command = "npx"`)
	requireContains(t, string(merged.ManifestContent()), `[mcp_server.env.TOKEN]`)
	requireContains(t, string(merged.ManifestContent()), `from_env = "TOKEN"`)
}

func TestIntoManifestNoopsEquivalentMCPServer(t *testing.T) {
	plan := mergeTestInput{
		Merge: true,
		OriginalContent: []byte(`version = 1
targets = ["claude-code"]

[[mcp_server]]
name = "context7"
targets = ["claude-code"]
scope = "project"
transport = "stdio"
command = "npx"
args = ["-y", "@upstash/context7-mcp"]
env = { TOKEN = { from_env = "TOKEN" } }
`),
		MCPServers: []adopt.MCPServer{{
			ResourceName: "context7",
			Target:       target.TargetClaudeCode,
			Scope:        target.ScopeProject,
			Command:      "npx",
			Args:         []string{"-y", "@upstash/context7-mcp"},
			Env:          map[string]string{"TOKEN": "TOKEN"},
		}},
	}

	merged, err := mergeTestPlan(t, plan)
	if err != nil {
		t.Fatal(err)
	}
	if merged.HasMergeConflicts() {
		t.Fatalf("merge results = %#v, want no conflict", merged.MergeResults())
	}
	if len(merged.MCPServers()) != 0 {
		t.Fatalf("mcp servers = %#v, want no writable additions", merged.MCPServers())
	}
	if string(merged.ManifestContent()) != string(plan.OriginalContent) {
		t.Fatalf("manifest changed on noop")
	}
	if got := merged.MergeResults()[0].Status; got != adopt.MergeStatusNoop {
		t.Fatalf("status = %s, want noop", got)
	}
}

func TestIntoManifestReportsSameNameMCPServerConflict(t *testing.T) {
	plan := mergeTestInput{
		Merge: true,
		OriginalContent: []byte(`version = 1
targets = ["claude-code"]

[[mcp_server]]
name = "context7"
targets = ["claude-code"]
scope = "project"
transport = "stdio"
command = "node"
args = ["server.js"]
`),
		MCPServers: []adopt.MCPServer{{
			ResourceName: "context7",
			Target:       target.TargetClaudeCode,
			Scope:        target.ScopeProject,
			Command:      "npx",
			Args:         []string{"-y", "@upstash/context7-mcp"},
		}},
	}

	merged, err := mergeTestPlan(t, plan)
	if err != nil {
		t.Fatal(err)
	}
	if !merged.HasMergeConflicts() {
		t.Fatalf("merge results = %#v, want conflict", merged.MergeResults())
	}
	if len(merged.MCPServers()) != 0 {
		t.Fatalf("mcp servers = %#v, want conflicting server removed from writable additions", merged.MCPServers())
	}
	if string(merged.ManifestContent()) != string(plan.OriginalContent) {
		t.Fatalf("manifest content changed on conflict")
	}
	if got := merged.MergeResults()[0].Detail; !strings.Contains(got, "different standalone projection shape") {
		t.Fatalf("detail = %q", got)
	}
}

func TestIntoManifestReportsSameNameMCPTargetScopeConflict(t *testing.T) {
	plan := mergeTestInput{
		Merge: true,
		OriginalContent: []byte(`version = 1
targets = ["opencode"]

[[mcp_server]]
name = "context7"
targets = ["opencode"]
scope = "project"
transport = "stdio"
command = "npx"
args = ["-y", "@upstash/context7-mcp"]
`),
		MCPServers: []adopt.MCPServer{{
			ResourceName: "context7",
			Target:       target.TargetClaudeCode,
			Scope:        target.ScopeProject,
			Command:      "npx",
			Args:         []string{"-y", "@upstash/context7-mcp"},
		}},
	}

	merged, err := mergeTestPlan(t, plan)
	if err != nil {
		t.Fatal(err)
	}
	if !merged.HasMergeConflicts() {
		t.Fatalf("merge results = %#v, want target/scope conflict", merged.MergeResults())
	}
	if len(merged.MCPServers()) != 0 {
		t.Fatalf("mcp servers = %#v, want conflicting server removed from writable additions", merged.MCPServers())
	}
	if string(merged.ManifestContent()) != string(plan.OriginalContent) {
		t.Fatalf("manifest content changed on conflict")
	}
	if got := merged.MergeResults()[0].Detail; !strings.Contains(got, "different standalone projection shape") {
		t.Fatalf("detail = %q", got)
	}
}

func mergeTestPlan(t *testing.T, input mergeTestInput) (adopt.Plan, error) {
	t.Helper()
	if !input.Merge {
		t.Fatal("merge test input must select merge")
	}
	root := t.TempDir()
	output := filepath.Join(root, "daem.toml")
	sourceDirectory, err := adopt.NewSourceDirectory(output, filepath.Join(root, "daem.d"))
	if err != nil {
		t.Fatal(err)
	}
	request, err := adopt.NewRequest(
		adopt.SupportedTargets(),
		[]target.Scope{target.ScopeProject, target.ScopeGlobal},
		output,
		sourceDirectory,
		true,
	)
	if err != nil {
		t.Fatal(err)
	}

	for index := range input.Sources {
		if input.Sources[index].LivePath == "" {
			input.Sources[index].LivePath = filepath.Join(root, "live-instructions")
		}
		if len(input.Sources[index].Content) == 0 {
			input.Sources[index].Content = []byte("imported instructions\n")
		}
	}
	for index := range input.Skills {
		if len(input.Skills[index].Targets) == 0 {
			input.Skills[index].Targets = []target.Target{input.Skills[index].Target}
		}
		if input.Skills[index].LivePath == "" {
			input.Skills[index].LivePath = filepath.Join(root, "live-skill")
		}
		if input.Skills[index].ReadPath == "" {
			input.Skills[index].ReadPath = input.Skills[index].LivePath
		}
		if input.Skills[index].ContentHash == "" {
			input.Skills[index].ContentHash = artifact.HashFileContent([]byte("merge test skill"))
		}
	}
	for index := range input.Hooks {
		if input.Hooks[index].LivePath == "" {
			input.Hooks[index].LivePath = filepath.Join(root, "live-hooks")
		}
	}
	for index := range input.MCPServers {
		if input.MCPServers[index].LivePath == "" {
			input.MCPServers[index].LivePath = filepath.Join(root, "live-mcp")
		}
	}
	candidates, err := adopt.NewCandidateSet(
		input.Sources,
		input.Skills,
		input.Hooks,
		input.MCPServers,
		nil,
		nil,
	)
	if err != nil {
		return adopt.Plan{}, err
	}
	return IntoManifest(request, input.OriginalContent, candidates)
}
