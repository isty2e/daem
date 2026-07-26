package codec

import (
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/declaration"
)

func TestHookSameIdentityTreatsImplicitCommandTypeAsCanonicalDefault(t *testing.T) {
	left := Hook{Name: "lint", Event: " before ", Command: " run ", Scope: " project "}
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
	existing := Hook{Name: "lint", Event: "before", Command: "run", Targets: []string{"codex"}, Scope: "project"}
	incoming := existing
	incoming.Targets = []string{"claude-code"}
	original := []byte("# keep\n" + RenderHookBlock(existing))
	called := false

	change, err := ApplyHookAdd(original, declaration.ManifestHeader{}, incoming, func(existing Hook, _ Hook, merged []string, _ declaration.ManifestHeader) (Hook, error) {
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

func TestHookFilterOverridesPreservesDeclarationOrder(t *testing.T) {
	overrides := []HookTargetOverride{{Target: "codex"}, {Target: "claude-code"}, {Target: "opencode"}}
	filtered := FilterHookOverrides(overrides, []string{"opencode", "codex"})
	if len(filtered) != 2 || filtered[0].Target != "codex" || filtered[1].Target != "opencode" {
		t.Fatalf("FilterHookOverrides = %#v", filtered)
	}
}

func TestHookApplyAddRejectsMissingAdmissionPolicy(t *testing.T) {
	_, err := ApplyHookAdd(nil, declaration.ManifestHeader{}, Hook{Name: "lint"}, nil)
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

func TestHookRenderBlockWritesHookSyntax(t *testing.T) {
	rendered := RenderHookBlock(Hook{
		Name:    "fmt",
		Event:   "PreToolUse",
		Command: "make fmt",
		Targets: []string{"codex"},
		TargetOverrides: []HookTargetOverride{{
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
