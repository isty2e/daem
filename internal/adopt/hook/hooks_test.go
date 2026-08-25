package hook

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/adopt"
	"github.com/isty2e/daem/internal/encoding/hookdocument"
	"github.com/isty2e/daem/internal/encoding/jsonstrict"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/target"
)

func TestCandidatesImportsRepresentableCodexHook(t *testing.T) {
	tempDir := t.TempDir()
	oldWorkingDirectory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(tempDir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(oldWorkingDirectory); err != nil {
			t.Fatalf("restore working directory: %v", err)
		}
	})
	if err := os.MkdirAll(".codex", 0o700); err != nil {
		t.Fatal(err)
	}
	content := []byte(`{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"echo ok","timeout":3,"statusMessage":"checking"}]}]}}`)
	if err := os.WriteFile(filepath.Join(".codex", "hooks.json"), content, 0o600); err != nil {
		t.Fatal(err)
	}

	hooks, skipped, err := Candidates(context.Background(), target.TargetCodex, target.ScopeProject)
	if err != nil {
		t.Fatal(err)
	}
	if len(skipped) != 0 {
		t.Fatalf("skipped = %#v, want none", skipped)
	}
	if len(hooks) != 1 {
		t.Fatalf("hooks = %#v, want one", hooks)
	}
	hook := hooks[0]
	if hook.Event != "PreToolUse" || hook.Matcher != "Bash" || hook.Command != "echo ok" {
		t.Fatalf("hook = %#v", hook)
	}
	if hook.ResourceName != "codex_project_pretooluse_1_1" {
		t.Fatalf("resource name = %q, want readable stable name", hook.ResourceName)
	}
	if hook.Target != target.TargetCodex || hook.Scope != target.ScopeProject {
		t.Fatalf("target/scope = %s/%s", hook.Target, hook.Scope)
	}
}

func TestCandidatesSkipsCodexHookShapeThatSurfaceCannotRepresent(t *testing.T) {
	tempDir := t.TempDir()
	oldWorkingDirectory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(tempDir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(oldWorkingDirectory); err != nil {
			t.Fatalf("restore working directory: %v", err)
		}
	})
	if err := os.MkdirAll(".codex", 0o700); err != nil {
		t.Fatal(err)
	}
	content := []byte(`{"hooks":{"Stop":[{"matcher":"Bash","hooks":[{"type":"command","command":"echo no"}]}]}}`)
	if err := os.WriteFile(filepath.Join(".codex", "hooks.json"), content, 0o600); err != nil {
		t.Fatal(err)
	}

	hooks, skipped, err := Candidates(context.Background(), target.TargetCodex, target.ScopeProject)
	if err != nil {
		t.Fatal(err)
	}
	if len(hooks) != 0 {
		t.Fatalf("hooks = %#v, want none", hooks)
	}
	if len(skipped) != 1 || skipped[0].Reason == "" {
		t.Fatalf("skipped = %#v, want one reason", skipped)
	}
}

func TestHookDestinationPathUsesCanonicalOutputGrammar(t *testing.T) {
	projectDestination, err := output.Parse(".codex/hooks.json")
	if err != nil {
		t.Fatal(err)
	}
	projectPath, err := hookDestinationPath(projectDestination, target.ScopeProject)
	if err != nil {
		t.Fatalf("hookDestinationPath(project) error = %v", err)
	}
	if want := filepath.FromSlash(".codex/hooks.json"); projectPath != want {
		t.Fatalf("hookDestinationPath(project) = %q, want %q", projectPath, want)
	}

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	homeDestination, err := output.Parse("~/.claude/settings.json")
	if err != nil {
		t.Fatal(err)
	}
	homePath, err := hookDestinationPath(homeDestination, target.ScopeGlobal)
	if err != nil {
		t.Fatalf("hookDestinationPath(home) error = %v", err)
	}
	if want := filepath.Join(home, ".claude", "settings.json"); homePath != want {
		t.Fatalf("hookDestinationPath(home) = %q, want %q", homePath, want)
	}

	if _, err := hookDestinationPath(homeDestination, target.ScopeProject); err == nil {
		t.Fatal("hookDestinationPath(home, project scope) succeeded, want scope error")
	}
}

func TestImportHooksProjectionRejectsAmbiguousDocumentWithoutPartialHooks(t *testing.T) {
	validHook := `{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"echo ok"}]}]}`
	for _, test := range []struct {
		name    string
		content string
		reason  adopt.SkipReason
	}{
		{name: "duplicate root", content: `{"hooks":` + validHook + `,"hooks":{}}`, reason: importHookSkipDuplicateJSONKey},
		{name: "duplicate nested", content: `{"meta":{"x":1,"x":2},"hooks":` + validHook + `}`, reason: importHookSkipDuplicateJSONKey},
		{name: "excessive depth", content: strings.Repeat("[", 66) + "0" + strings.Repeat("]", 66), reason: importHookSkipJSONDepth},
		{name: "JSON comments not admitted", content: `{"hooks":{} // comment\n}`, reason: "malformed_json"},
	} {
		t.Run(test.name, func(t *testing.T) {
			hooks, skipped := parseImportHooks(
				[]byte(test.content),
				target.TargetCodex,
				target.ScopeProject,
				".codex/hooks.json",
			)
			if len(hooks) != 0 || len(skipped) != 1 || skipped[0].Reason != test.reason {
				t.Fatalf("parseImportHooks = (%#v, %#v), want one %q skip", hooks, skipped, test.reason)
			}
		})
	}
}

func TestImportHooksProjectionDoesNotInterpretJSONNumbers(t *testing.T) {
	content := []byte(`{"metadata":1e1000,"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"echo ok","ignored":1e1000}]}]}}`)
	hooks, skipped := parseImportHooks(
		content,
		target.TargetCodex,
		target.ScopeProject,
		".codex/hooks.json",
	)
	if len(hooks) != 0 || len(skipped) != 1 ||
		skipped[0].Reason != "unsupported_handler_field" ||
		!strings.Contains(skipped[0].Detail, "field=ignored") {
		t.Fatalf("parseImportHooks = (%#v, %#v), want semantic skip after syntax-only number scan", hooks, skipped)
	}
}

func TestParseImportHooksClassifiesOnlyTypedReasonCode(t *testing.T) {
	t.Parallel()

	hooks, skipped := parseImportHooks(
		[]byte(`{"hooks":{"changed_during_read":[{"hooks":[{"type":"command","command":"true","async":true}]}]}}`),
		target.TargetClaudeCode,
		target.ScopeProject,
		".claude/settings.json",
	)
	if len(hooks) != 0 || len(skipped) != 1 {
		t.Fatalf("parseImportHooks = (%#v, %#v), want one skip", hooks, skipped)
	}
	if skipped[0].Reason != "unsupported_async" ||
		skipped[0].Category() != adopt.SkipCategoryUnsupported ||
		skipped[0].ActionHint() != "" ||
		!strings.Contains(skipped[0].Detail, "event=changed_during_read") {
		t.Fatalf("skip = %#v, want unsupported typed reason independent of event detail", skipped[0])
	}
}

func TestCandidatesRejectsUnsafeHookFileShapes(t *testing.T) {
	root := t.TempDir()
	withHookWorkingDirectory(t, root)
	if err := os.MkdirAll(".codex", 0o700); err != nil {
		t.Fatal(err)
	}
	livePath := filepath.Join(".codex", "hooks.json")

	assertSkip := func(reason adopt.SkipReason) {
		t.Helper()
		hooks, skipped, err := Candidates(context.Background(), target.TargetCodex, target.ScopeProject)
		if err != nil {
			t.Fatalf("Candidates returned error: %v", err)
		}
		if len(hooks) != 0 || !hasHookSkip(skipped, ".codex/hooks.json", reason) {
			t.Fatalf("Candidates = (%#v, %#v), want %q skip", hooks, skipped, reason)
		}
	}

	if err := os.Mkdir(livePath, 0o700); err != nil {
		t.Fatal(err)
	}
	assertSkip(importHookSkipNotRegular)
	if err := os.Remove(livePath); err != nil {
		t.Fatal(err)
	}

	targetPath := filepath.Join(root, "actual-hooks.json")
	if err := os.WriteFile(targetPath, []byte(`{"hooks":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(targetPath, livePath); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	assertSkip(importHookSkipSymlink)
	if err := os.Remove(livePath); err != nil {
		t.Fatal(err)
	}

	file, err := os.OpenFile(livePath, os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(hookdocument.MaximumBytes + 1); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	assertSkip(importHookSkipTooLarge)
}

func TestCandidatesClassifiesCodexInlineConfigStructureLimit(t *testing.T) {
	root := t.TempDir()
	withHookWorkingDirectory(t, root)
	if err := os.MkdirAll(".codex", 0o700); err != nil {
		t.Fatal(err)
	}
	content := "root = " + strings.Repeat("{ k = ", 64) + "1" + strings.Repeat(" }", 64) + "\n"
	if err := os.WriteFile(filepath.Join(".codex", "config.toml"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	hooks, skipped, err := Candidates(context.Background(), target.TargetCodex, target.ScopeProject)
	if err != nil {
		t.Fatalf("Candidates returned error: %v", err)
	}
	if len(hooks) != 0 || !hasHookSkip(skipped, ".codex/config.toml", "inline_config_structure_limit") {
		t.Fatalf("Candidates = (%#v, %#v), want inline config structure-limit skip", hooks, skipped)
	}
}

func TestCandidatesStopsWhenHookImportContextIsCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	hooks, skipped, err := Candidates(ctx, target.TargetCodex, target.ScopeProject)
	if !errors.Is(err, context.Canceled) || hooks != nil || skipped != nil {
		t.Fatalf("Candidates = (%#v, %#v, %v), want context cancellation", hooks, skipped, err)
	}
}

func TestParseImportHooksCollapsesAggregateBudgetFailures(t *testing.T) {
	t.Parallel()

	tooLongEvent := strings.Repeat("e", hookdocument.MaximumEventBytes+1)
	assertHookImportBudgetFailure(t, `{"hooks":{"`+tooLongEvent+`":[]}}`)

	events := make([]string, hookdocument.MaximumEvents+1)
	for index := range events {
		events[index] = fmt.Sprintf(`"event-%d":[]`, index)
	}
	assertHookImportBudgetFailure(t, `{"hooks":{`+strings.Join(events, ",")+`}}`)

	groups := make([]string, hookdocument.MaximumGroups+1)
	for index := range groups {
		groups[index] = `{"hooks":[]}`
	}
	assertHookImportBudgetFailure(
		t,
		`{"hooks":{"AnyEvent":[`+strings.Join(groups, ",")+`]}}`,
	)

	handlers := make([]string, hookdocument.MaximumHandlers+1)
	for index := range handlers {
		handlers[index] = `{"type":"command","command":"true"}`
	}
	assertHookImportBudgetFailure(
		t,
		`{"hooks":{"AnyEvent":[{"hooks":[`+strings.Join(handlers, ",")+`]}]}}`,
	)

	assertHookImportBudgetFailure(
		t,
		`{"hooks":{"   ":[`+strings.Join(groups[:hookdocument.MaximumGroups], ",")+`],"Valid":[{"hooks":[{"type":"command","command":"true"}]}]}}`,
	)
	assertHookImportBudgetFailure(
		t,
		`{"hooks":{"   ":[{"hooks":[`+strings.Join(handlers[:hookdocument.MaximumHandlers], ",")+`]}],"Valid":[{"hooks":[{"type":"command","command":"true"}]}]}}`,
	)
}

func TestScanImportHookStructuralBudgetStopsAtEachLimit(t *testing.T) {
	const entries = 200_000
	events := make([]string, entries)
	for index := range events {
		events[index] = fmt.Sprintf(`"event-%d":[]`, index)
	}
	handlers := strings.Repeat(`{},`, entries-1) + `{}`
	for _, test := range []struct {
		name    string
		content string
	}{
		{name: "events", content: `{"hooks":{` + strings.Join(events, ",") + `}}`},
		{name: "groups", content: `{"hooks":{"AnyEvent":[` + handlers + `]}}`},
		{name: "handlers", content: `{"hooks":{"AnyEvent":[{"hooks":[` + handlers + `]}]}}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := hookdocument.Validate([]byte(test.content))
			if !errors.Is(err, hookdocument.ErrStructuralBudgetExceeded) {
				t.Fatalf("hookdocument.Validate error = %v, want structural budget error", err)
			}
		})
	}

	content := `{"hooks":{"AnyEvent":[{"hooks":[` + handlers + `]}]}}`
	var imported []adopt.Hook
	var skipped []adopt.Skipped
	allocations := testing.AllocsPerRun(3, func() {
		imported, skipped = parseImportHooks(
			[]byte(content),
			target.TargetClaudeCode,
			target.ScopeProject,
			".claude/settings.json",
		)
	})
	if len(imported) != 0 || len(skipped) != 1 || skipped[0].Reason != importHookSkipBudgetExceeded {
		t.Fatalf("allocation probe = (%#v, %#v), want one structural budget skip", imported, skipped)
	}
	if allocations >= 100_000 {
		t.Fatalf("structural scan allocations = %.0f, want fewer than 100000", allocations)
	}
}

func TestScanImportHookStructuralBudgetAdmitsExactLimits(t *testing.T) {
	events := make([]string, hookdocument.MaximumEvents)
	for index := range events {
		events[index] = fmt.Sprintf(`"event-%d":[]`, index)
	}
	groups := strings.Repeat(`{},`, hookdocument.MaximumGroups-1) + `{}`
	handlers := strings.Repeat(`{},`, hookdocument.MaximumHandlers-1) + `{}`
	for _, test := range []struct {
		name    string
		content string
	}{
		{name: "events", content: `{"hooks":{` + strings.Join(events, ",") + `}}`},
		{name: "groups", content: `{"hooks":{"AnyEvent":[` + groups + `]}}`},
		{name: "handlers", content: `{"hooks":{"AnyEvent":[{"hooks":[` + handlers + `]}]}}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := hookdocument.Validate([]byte(test.content)); err != nil {
				t.Fatalf("hookdocument.Validate error = %v, want exact limit admitted", err)
			}
		})
	}
}

func TestScanImportHookStructuralBudgetStopsAtDepthLimit(t *testing.T) {
	const excessiveDepth = 500_000
	nesting := strings.Repeat(`[`, excessiveDepth) + strings.Repeat(`]`, excessiveDepth)
	var topLevelContent string
	for _, test := range []struct {
		name   string
		prefix string
		suffix string
	}{
		{name: "top-level field", prefix: `{"metadata":`, suffix: `,"hooks":{}}`},
		{name: "handler field", prefix: `{"hooks":{"AnyEvent":[{"hooks":[{"metadata":`, suffix: `}]}]}}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			content := test.prefix + nesting + test.suffix
			if test.name == "top-level field" {
				topLevelContent = content
			}
			err := hookdocument.Validate([]byte(content))
			if !errors.Is(err, jsonstrict.ErrMaximumDepthExceeded) {
				t.Fatalf("hookdocument.Validate error = %v, want maximum depth error", err)
			}
		})
	}

	contentBytes := []byte(topLevelContent)
	hooks, skipped := parseImportHooks(
		contentBytes,
		target.TargetClaudeCode,
		target.ScopeProject,
		".claude/settings.json",
	)
	if len(hooks) != 0 || len(skipped) != 1 || skipped[0].Reason != importHookSkipJSONDepth {
		t.Fatalf("parseImportHooks = (%#v, %#v), want one depth skip", hooks, skipped)
	}

	benchmark := testing.Benchmark(func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			hooks, skipped = parseImportHooks(
				contentBytes,
				target.TargetClaudeCode,
				target.ScopeProject,
				".claude/settings.json",
			)
		}
	})
	if allocated := benchmark.AllocedBytesPerOp(); allocated >= 1<<20 {
		t.Fatalf("depth rejection allocated %d bytes/op, want less than 1 MiB", allocated)
	}
}

func TestScanImportHookStructuralBudgetMatchesSharedDepthBoundary(t *testing.T) {
	for _, test := range []struct {
		name   string
		depth  int
		reason adopt.SkipReason
	}{
		{name: "exact", depth: hookdocument.MaximumDepth},
		{name: "exceeded", depth: hookdocument.MaximumDepth + 1, reason: importHookSkipJSONDepth},
	} {
		t.Run(test.name, func(t *testing.T) {
			content := `{"metadata":` +
				strings.Repeat(`[`, test.depth) +
				strings.Repeat(`]`, test.depth) +
				`,"hooks":{}}`
			scanErr := hookdocument.Validate([]byte(content))
			if test.reason == "" && scanErr != nil {
				t.Fatalf("hookdocument.Validate error = %v, want exact depth admitted", scanErr)
			}
			if test.reason != "" && !errors.Is(scanErr, jsonstrict.ErrMaximumDepthExceeded) {
				t.Fatalf("hookdocument.Validate error = %v, want maximum depth error", scanErr)
			}

			hooks, skipped := parseImportHooks(
				[]byte(content),
				target.TargetClaudeCode,
				target.ScopeProject,
				".claude/settings.json",
			)
			if test.reason == "" {
				if len(hooks) != 0 || len(skipped) != 1 || skipped[0].Reason != "hooks_empty" {
					t.Fatalf("parseImportHooks = (%#v, %#v), want exact depth admitted", hooks, skipped)
				}
				return
			}
			if len(hooks) != 0 || len(skipped) != 1 || skipped[0].Reason != test.reason {
				t.Fatalf("parseImportHooks = (%#v, %#v), want one %q skip", hooks, skipped, test.reason)
			}
		})
	}
}

func TestParseImportHooksBudgetPreemptsUnboundedMalformedTail(t *testing.T) {
	content := `{"hooks":{"AnyEvent":[{"hooks":[` +
		strings.Repeat(`{},`, hookdocument.MaximumHandlers) +
		`malformed-tail` +
		`] }]}}`
	assertHookImportBudgetFailure(t, content)
}

func TestParseImportHooksBoundsDiagnosticAmplification(t *testing.T) {
	t.Parallel()

	handlers := make([]string, hookdocument.MaximumHandlers)
	for index := range handlers {
		handlers[index] = `{"type":"unsupported"}`
	}
	handlers[0] = `{"type":"command","command":"must-not-survive"}`
	content := `{"hooks":{"AnyEvent":[{"hooks":[` + strings.Join(handlers, ",") + `]}]}}`
	hooks, skipped := parseImportHooks(
		[]byte(content),
		target.TargetClaudeCode,
		target.ScopeProject,
		".claude/settings.json",
	)
	if len(hooks) != 0 || len(skipped) != 1 || skipped[0].Reason != importHookSkipBudgetExceeded {
		t.Fatalf("parseImportHooks = (%#v, %#v), want one bounded budget skip and no partial hook", hooks, skipped)
	}
}

func TestParseImportHooksUsesDeterministicCollisionNames(t *testing.T) {
	t.Parallel()

	longField := strings.Repeat("field", 1000)
	content := `{"hooks":{"A B":[{"hooks":[{"type":"command","command":"one"}]}],"A-B":[{"hooks":[{"type":"command","command":"two"},{"type":"command","command":"three","` + longField + `":true}]}]}}`
	hooks, skipped := parseImportHooks(
		[]byte(content),
		target.TargetClaudeCode,
		target.ScopeProject,
		".claude/settings.json",
	)
	if len(hooks) != 2 || len(skipped) != 1 {
		t.Fatalf("parseImportHooks = (%#v, %#v), want two hooks and one skip", hooks, skipped)
	}
	base := importHookName(target.TargetClaudeCode, target.ScopeProject, "a_b", 0, 0)
	if hooks[0].ResourceName != base || hooks[1].ResourceName != base+"_2" {
		t.Fatalf("collision names = %q, %q, want %q, %q", hooks[0].ResourceName, hooks[1].ResourceName, base, base+"_2")
	}
	if len(skipped[0].Detail) > 256 || strings.Contains(skipped[0].Detail, longField) {
		t.Fatalf("skip detail is not bounded: length=%d detail=%q", len(skipped[0].Detail), skipped[0].Detail)
	}
}

func TestParseImportHooksPreservesSequentialSuffixAcrossNestedCollisions(t *testing.T) {
	t.Parallel()

	content := `{"hooks":{` +
		`"A B":[{"hooks":[{"type":"command","command":"one"}]}],` +
		`"A B 1":[{"hooks":[{"type":"command","command":"two"},{"type":"command","command":"three"}]}],` +
		`"A-B":[{"hooks":[{"type":"command","command":"four"}]}]}}`
	hooks, skipped := parseImportHooks(
		[]byte(content),
		target.TargetClaudeCode,
		target.ScopeProject,
		".claude/settings.json",
	)
	base := importHookName(target.TargetClaudeCode, target.ScopeProject, "A B", 0, 0)
	want := []string{
		base,
		importHookName(target.TargetClaudeCode, target.ScopeProject, "A B 1", 0, 0),
		base + "_2",
		base + "_3",
	}
	if len(hooks) != len(want) || len(skipped) != 0 {
		t.Fatalf("parseImportHooks = (%#v, %#v), want %d hooks and no skips", hooks, skipped, len(want))
	}
	for index, hook := range hooks {
		if hook.ResourceName != want[index] {
			t.Fatalf("hook %d resource name = %q, want %q", index, hook.ResourceName, want[index])
		}
	}
}

func TestParseImportHooksPreservesSinglePassEventSanitization(t *testing.T) {
	t.Parallel()

	event := "-"
	hooks, skipped := parseImportHooks(
		[]byte(`{"hooks":{"`+event+`":[{"hooks":[{"type":"command","command":"true"}]}]}}`),
		target.TargetClaudeCode,
		target.ScopeProject,
		".claude/settings.json",
	)
	want := "claude_code_project_1_1"
	if len(hooks) != 1 || len(skipped) != 0 || hooks[0].ResourceName != want {
		t.Fatalf("parseImportHooks = (%#v, %#v), want preserved resource name %q", hooks, skipped, want)
	}
}

func TestImportHookNameAllocatorMatchesSequentialContract(t *testing.T) {
	t.Parallel()

	type nameInput struct {
		event        string
		groupIndex   int
		handlerIndex int
	}
	inputs := []nameInput{
		{event: "A B"},
		{event: "A B 1"},
		{event: "A B 1", handlerIndex: 1},
		{event: "A-B"},
		{event: "A_B"},
		{event: "-"},
		{event: "A B", groupIndex: 1},
		{event: "A-B", groupIndex: 1},
	}
	collector := importHookCollector{target: target.TargetClaudeCode, scope: target.ScopeProject}
	referenceNames := make(map[string]struct{})
	for round := 0; round < 4; round++ {
		for _, input := range inputs {
			identity := newImportHookEventIdentity(input.event)
			got := collector.reserveHookName(identity, input.groupIndex, input.handlerIndex)
			want := reserveSequentialHookName(
				target.TargetClaudeCode,
				target.ScopeProject,
				input.event,
				input.groupIndex,
				input.handlerIndex,
				referenceNames,
			)
			if got != want {
				t.Fatalf("round %d input %#v name = %q, want sequential contract %q", round, input, got, want)
			}
		}
	}
}

func reserveSequentialHookName(
	targetValue target.Target,
	scope target.Scope,
	event string,
	groupIndex int,
	handlerIndex int,
	seen map[string]struct{},
) string {
	base := importHookName(targetValue, scope, event, groupIndex, handlerIndex)
	name := base
	for suffix := 2; ; suffix++ {
		if _, used := seen[name]; !used {
			seen[name] = struct{}{}
			return name
		}
		name = fmt.Sprintf("%s_%d", base, suffix)
	}
}

func TestParseImportHooksPreservesLongAdmittedEventResourceName(t *testing.T) {
	t.Parallel()

	event := strings.Repeat("e", 65)
	hooks, skipped := parseImportHooks(
		[]byte(`{"hooks":{"`+event+`":[{"hooks":[{"type":"command","command":"true"}]}]}}`),
		target.TargetClaudeCode,
		target.ScopeProject,
		".claude/settings.json",
	)
	want := importHookName(target.TargetClaudeCode, target.ScopeProject, event, 0, 0)
	if len(hooks) != 1 || len(skipped) != 0 || hooks[0].ResourceName != want {
		t.Fatalf("parseImportHooks = (%#v, %#v), want preserved resource name %q", hooks, skipped, want)
	}
}

func TestParseImportHooksRejectsCandidateOutsideDesiredHookInvariant(t *testing.T) {
	t.Parallel()

	hooks, skipped := parseImportHooks(
		[]byte("{\"hooks\":{\"Stop\\u202eHidden\":[{\"hooks\":[{\"type\":\"command\",\"command\":\"true\"}]}]}}"),
		target.TargetClaudeCode,
		target.ScopeProject,
		".claude/settings.json",
	)
	if len(hooks) != 0 || len(skipped) != 1 || skipped[0].Reason != importHookSkipInvalidCanonical {
		t.Fatalf("parseImportHooks = (%#v, %#v), want one canonical-invariant skip", hooks, skipped)
	}
}

func assertHookImportBudgetFailure(t *testing.T, content string) {
	t.Helper()
	hooks, skipped := parseImportHooks(
		[]byte(content),
		target.TargetClaudeCode,
		target.ScopeProject,
		".claude/settings.json",
	)
	if len(hooks) != 0 || len(skipped) != 1 || skipped[0].Reason != importHookSkipBudgetExceeded {
		t.Fatalf("parseImportHooks = (%d hooks, %#v), want one budget skip", len(hooks), skipped)
	}
}

func withHookWorkingDirectory(t *testing.T, directory string) {
	t.Helper()

	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(directory); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(previous); err != nil {
			t.Fatalf("restore working directory: %v", err)
		}
	})
}

func hasHookSkip(skipped []adopt.Skipped, livePath string, reason adopt.SkipReason) bool {
	for _, skip := range skipped {
		if skip.LivePath == livePath && skip.Reason == reason {
			return true
		}
	}
	return false
}
