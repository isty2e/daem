package codec

import (
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/declaration"
)

func TestHookSameIdentityTreatsImplicitCommandTypeAsCanonicalDefault(t *testing.T) {
	left := declaration.Hook{Name: "lint", Event: " before ", Command: " run ", Scope: " project "}
	right := left
	right.Type = "command"
	right.Event = "before"
	right.Command = "run"
	right.Scope = "project"
	if !sameHookIdentity(left, right) {
		t.Fatal("implicit command type did not match explicit canonical type")
	}
}

func TestHookApplyAddDelegatesTargetAdmissionAndPreservesBytes(t *testing.T) {
	existing := declaration.Hook{Name: "lint", Event: "before", Command: "run", Targets: []string{"codex"}, Scope: "project"}
	incoming := existing
	incoming.Targets = []string{"claude-code"}
	original := []byte("# keep\n" + RenderHookBlock(existing))
	called := false

	change, err := ApplyHookAdd(original, declaration.ManifestHeader{}, incoming, func(existing declaration.Hook, _ declaration.Hook, merged []string, _ declaration.ManifestHeader) (declaration.Hook, error) {
		called = true
		existing.Targets = append([]string(nil), merged...)
		return existing, nil
	})
	if err != nil {
		t.Fatalf("ApplyHookAdd: %v", err)
	}
	if !called {
		t.Fatal("target admission callback was not called")
	}
	got := string(change.Content)
	if !strings.HasPrefix(got, "# keep\n") || !strings.Contains(got, `targets = ["codex", "claude-code"]`) {
		t.Fatalf("unexpected merged document:\n%s", got)
	}
}

func TestHookTargetUpdatePreservesUntouchedBytesAndLineEndings(t *testing.T) {
	existing := declaration.Hook{
		Name: "lint", Event: "PreToolUse", Command: "make lint", Targets: []string{"codex"}, Scope: "project",
		TargetOverrides: []declaration.HookTargetOverride{{Target: "codex", Matcher: "Write"}},
	}
	updated := existing
	updated.Targets = []string{"codex", "claude-code"}
	updated.TargetOverrides = append(
		append([]declaration.HookTargetOverride(nil), existing.TargetOverrides...),
		declaration.HookTargetOverride{Target: "claude-code", Condition: "always"},
	)
	block := "[[hook]] # keep header\r\n" +
		"name = 'lint'\r\n" +
		"event = 'PreToolUse'\r\n" +
		"command = 'make lint' # keep command\r\n" +
		"targets = ['codex']\r\n" +
		"scope = 'project'\r\n" +
		"\r\n" +
		"[[hook.target_override]] # keep override\r\n" +
		"target = 'codex'\r\n" +
		"matcher = 'Write'"

	got, err := UpdateHookTargets(block, existing, updated)
	if err != nil {
		t.Fatalf("UpdateHookTargets returned error: %v", err)
	}
	for _, want := range []string{
		"[[hook]] # keep header\r\n",
		"command = 'make lint' # keep command\r\n",
		"[[hook.target_override]] # keep override\r\n",
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

func TestHookTargetUpdateRemovesOnlyDeselectedOverrideTable(t *testing.T) {
	existing := declaration.Hook{
		Name: "lint", Event: "PreToolUse", Command: "make lint", Targets: []string{"codex", "claude-code"},
		TargetOverrides: []declaration.HookTargetOverride{
			{Target: "codex", Matcher: "Write"},
			{Target: "claude-code", Condition: "always"},
		},
	}
	updated := existing
	updated.Targets = []string{"claude-code"}
	updated.TargetOverrides = existing.TargetOverrides[1:]
	block := `[[hook]]
name = "lint"
event = "PreToolUse"
command = "make lint" # keep
targets = ["codex", "claude-code"]

[[hook.target_override]] # remove
target = "codex"
matcher = "Write"

[[hook.target_override]] # keep
target = "claude-code"
if = "always"
`

	got, err := UpdateHookTargets(block, existing, updated)
	if err != nil {
		t.Fatalf("UpdateHookTargets returned error: %v", err)
	}
	if strings.Contains(got, `target = "codex"`) || strings.Contains(got, "# remove") {
		t.Fatalf("removed override remains: %q", got)
	}
	for _, want := range []string{
		`targets = ["claude-code"]`,
		`command = "make lint" # keep`,
		"[[hook.target_override]] # keep",
		`target = "claude-code"`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("content = %q, want %q", got, want)
		}
	}
}

func TestHookFilterOverridesPreservesDeclarationOrder(t *testing.T) {
	overrides := []declaration.HookTargetOverride{{Target: "codex"}, {Target: "claude-code"}, {Target: "opencode"}}
	filtered := FilterHookOverrides(overrides, []string{"opencode", "codex"})
	if len(filtered) != 2 || filtered[0].Target != "codex" || filtered[1].Target != "opencode" {
		t.Fatalf("FilterHookOverrides = %#v", filtered)
	}
}

func TestHookApplyAddRejectsMissingAdmissionPolicy(t *testing.T) {
	_, err := ApplyHookAdd(nil, declaration.ManifestHeader{}, declaration.Hook{Name: "lint"}, nil)
	if err == nil || !strings.Contains(err.Error(), "hook target merge policy is required") {
		t.Fatalf("ApplyHookAdd error = %v", err)
	}
}

func TestHookScanBlocksFindsHookTables(t *testing.T) {
	original := []byte(`[[hook]]
name = "lint"
event = "PreToolUse"
matcher = "Write"
command = "make lint"
targets = ["codex", "claude-code"]
scope = "project"

[[hook.target_override]]
target = "codex"
matcher = "Edit"
`)

	blocks, err := ScanHookBlocks(original)
	if err != nil {
		t.Fatalf("ScanHookBlocks() error = %v", err)
	}
	if len(blocks) != 1 {
		t.Fatalf("ScanHookBlocks() returned %d blocks, want 1", len(blocks))
	}
	if blocks[0].Hook.Name != "lint" {
		t.Fatalf("hook name = %q, want lint", blocks[0].Hook.Name)
	}
}

func TestHookScanBlocksKeepsInlineCommentedHookOverrideAndFindsNextHook(t *testing.T) {
	original := []byte(`[[hook]]
name = "lint"
event = "PreToolUse"
matcher = "Write"
command = "make lint"
targets = ["codex"]

[[hook.target_override]] # user-authored comment
target = "codex"
matcher = "Edit"

[[hook]]
name = "fmt"
event = "PreToolUse"
matcher = "Write"
command = "make fmt"
targets = ["codex"]
`)

	blocks, err := ScanHookBlocks(original)
	if err != nil {
		t.Fatalf("ScanHookBlocks() error = %v", err)
	}
	if len(blocks) != 2 {
		t.Fatalf("ScanHookBlocks() returned %d blocks, want 2", len(blocks))
	}
	if len(blocks[0].Hook.TargetOverrides) != 1 {
		t.Fatalf("first hook overrides = %#v, want one override", blocks[0].Hook.TargetOverrides)
	}
	if blocks[1].Hook.Name != "fmt" {
		t.Fatalf("second hook name = %q, want fmt", blocks[1].Hook.Name)
	}
	firstBlock := string(original[blocks[0].Start:blocks[0].End])
	if strings.Contains(firstBlock, `name = "fmt"`) {
		t.Fatalf("first hook block included following hook: %q", firstBlock)
	}
}

func TestHookSharedWireRowKeepsStrictnessOperationLocal(t *testing.T) {
	content := []byte(`version = 1
targets = ["codex"]

[[hook]]
name = "lint"
event = "PreToolUse"
command = "make lint"
future = "preserve"

[[hook.target_override]]
target = "codex"
matcher = "Write"
future = "preserve"
`)

	if _, err := declaration.DecodeManifest(content); err == nil ||
		!strings.Contains(err.Error(), "hook.future") {
		t.Fatalf("DecodeManifest error = %v, want strict unknown-field rejection", err)
	}
	blocks, err := ScanHookBlocks(content)
	if err != nil {
		t.Fatalf("ScanHookBlocks returned error: %v", err)
	}
	if len(blocks) != 1 || len(blocks[0].Hook.TargetOverrides) != 1 {
		t.Fatalf("blocks = %#v, want partial hook projection", blocks)
	}
	rawBlock := string(content[blocks[0].Start:blocks[0].End])
	if strings.Count(rawBlock, `future = "preserve"`) != 2 {
		t.Fatalf("raw block = %q, want both unowned fields preserved in range", rawBlock)
	}
}

func TestHookRenderBlockWritesHookSyntax(t *testing.T) {
	rendered := RenderHookBlock(declaration.Hook{
		Name:    "fmt",
		Event:   "PreToolUse",
		Command: "make fmt",
		Targets: []string{"codex"},
		TargetOverrides: []declaration.HookTargetOverride{{
			Target:  "codex",
			Matcher: "Write",
		}},
	})

	requireHookContains(t, rendered, `[[hook]]`)
	requireHookContains(t, rendered, `[[hook.target_override]]`)
	requireHookContains(t, rendered, `matcher = "Write"`)
}

func requireHookContains(t *testing.T, content string, fragment string) {
	t.Helper()
	if !strings.Contains(content, fragment) {
		t.Fatalf("content = %q, want fragment %q", content, fragment)
	}
}
