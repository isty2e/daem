package clipresent

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	adoptmodel "github.com/isty2e/daem/internal/adopt"
	"github.com/isty2e/daem/internal/target"
)

func TestImportSkipPresentationCompactsBenignRowsAndShowsEveryAction(t *testing.T) {
	t.Parallel()

	skipped := make([]adoptmodel.Skipped, 0, 94)
	for index := range 50 {
		skipped = append(skipped, adoptmodel.Skipped{
			Target:   target.TargetCodex,
			Scope:    target.ScopeGlobal,
			LivePath: fmt.Sprintf("duplicate-%03d", index),
			Reason:   "duplicate_skill_name",
		})
	}
	for index := range 40 {
		skipped = append(skipped, adoptmodel.Skipped{
			Target:   target.TargetClaudeCode,
			Scope:    target.ScopeProject,
			LivePath: fmt.Sprintf("missing-%03d", index),
			Reason:   "missing",
		})
	}
	for index := range 3 {
		skipped = append(skipped, adoptmodel.Skipped{
			Target:   target.TargetCodex,
			Scope:    target.ScopeGlobal,
			LivePath: fmt.Sprintf("unsupported-%03d", index),
			Reason:   "unsupported_mcp_managed_field",
		})
	}
	skipped = append(skipped, adoptmodel.Skipped{
		Target:   target.TargetClaudeCode,
		Scope:    target.ScopeProject,
		LivePath: ".mcp.json#/mcpServers/secret",
		Reason:   "secret_literal_forbidden",
	})

	presented := importSkippedFromAdoption(skipped)
	plan := ImportPlan{
		Label:     "import",
		DryRun:    true,
		Summary:   ImportSummary{Skipped: len(skipped)},
		Skipped:   presented,
		SourceDir: "sources",
	}

	var output bytes.Buffer
	PrintImportPlanWithOptions(&output, plan, HumanOptions{})
	text := output.String()
	for _, expected := range []string{
		"skipped: action_required=1 unsupported=3 informational=90",
		"action required:",
		`skip live=".mcp.json#/mcpServers/secret" reason=secret_literal_forbidden target=claude-code scope=project`,
		"next: replace literal secrets with symbolic environment references or leave this row unmanaged",
		"unsupported:",
		"target=codex reason=unsupported_mcp_managed_field count=3",
		"informational:",
		"target=claude-code reason=missing count=40",
		"target=codex reason=duplicate_skill_name count=50",
		"skipped detail: rerun with --verbose to inspect every skipped path",
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("output = %q, want %q", text, expected)
		}
	}
	for _, forbidden := range []string{"duplicate-000", "missing-000", "unsupported-000"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("output = %q, unexpectedly contains compacted path %q", text, forbidden)
		}
	}

	output.Reset()
	PrintImportPlanWithOptions(&output, plan, HumanOptions{Verbose: true})
	verbose := output.String()
	for _, expected := range []string{"duplicate-000", "missing-000", "unsupported-000", ".mcp.json#/mcpServers/secret"} {
		if !strings.Contains(verbose, expected) {
			t.Fatalf("verbose output = %q, want exact path %q", verbose, expected)
		}
	}
	if strings.Contains(verbose, "skipped detail: rerun with --verbose") {
		t.Fatalf("verbose output = %q, want no verbose pointer", verbose)
	}
}

func TestImportSkipPresentationShowsConflictRoutesAndStableDetail(t *testing.T) {
	t.Parallel()

	const first = "/home/user/.agents/skills/review"
	const second = "/home/user/.codex/skills/review"
	skipped := adoptmodel.Skipped{
		Target:   target.TargetCodex,
		Scope:    target.ScopeGlobal,
		LivePath: second,
		Reason:   "conflicting_skill_name",
		Detail:   "conflicts_with=" + first,
	}

	var output bytes.Buffer
	PrintImportSkippedActions(&output, []adoptmodel.Skipped{skipped})
	for _, want := range []string{
		`skip live="` + second + `" reason=conflicting_skill_name target=codex scope=global`,
		`detail="conflicts_with=` + first + `"`,
		"next: keep one skill definition, make the definitions identical, or rename one skill",
	} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("output = %q, want %q", output.String(), want)
		}
	}

	rows := importPlanJSONSkipped([]adoptmodel.Skipped{skipped})
	if len(rows) != 1 || rows[0].Reason != "conflicting_skill_name" ||
		rows[0].Detail != "conflicts_with="+first ||
		rows[0].ActionHint != "resolve_conflict" {
		t.Fatalf("JSON rows = %#v, want exact conflict code, detail, and action", rows)
	}
}

func TestImportJSONSkippedIncludesStableClassification(t *testing.T) {
	t.Parallel()

	rows := importPlanJSONSkipped([]adoptmodel.Skipped{
		{
			Target:   target.TargetClaudeCode,
			Scope:    target.ScopeProject,
			LivePath: ".mcp.json#/mcpServers/secret",
			Reason:   "secret_literal_forbidden",
		},
		{
			Target:   target.TargetCodex,
			Scope:    target.ScopeGlobal,
			LivePath: "missing",
			Reason:   "missing",
		},
	})
	if len(rows) != 2 {
		t.Fatalf("rows = %#v", rows)
	}
	if rows[0].Target != "claude-code" || rows[0].Scope != "project" ||
		rows[0].Category != "action_required" || rows[0].ActionHint != "use_symbolic_environment_reference" {
		t.Fatalf("actionable row = %#v", rows[0])
	}
	if rows[1].Target != "codex" || rows[1].Scope != "global" ||
		rows[1].Category != "informational" || rows[1].ActionHint != "" {
		t.Fatalf("informational row = %#v", rows[1])
	}
	actionableJSON, err := json.Marshal(rows[0])
	if err != nil {
		t.Fatal(err)
	}
	informationalJSON, err := json.Marshal(rows[1])
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(actionableJSON, []byte(`"action_hint":"use_symbolic_environment_reference"`)) ||
		bytes.Contains(informationalJSON, []byte(`"action_hint"`)) {
		t.Fatalf("actionable=%s informational=%s", actionableJSON, informationalJSON)
	}
}
