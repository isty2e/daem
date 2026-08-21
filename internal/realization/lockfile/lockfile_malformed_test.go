package lockfile

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/desired/entity"
	lock "github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/realization/lock/snapshottest"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/target"
)

type malformedLockfileCase struct {
	name      string
	content   string
	wantError string
}

func TestLoadRejectsMalformedCurrentLockfiles(t *testing.T) {
	for _, test := range malformedCurrentLockfileCases(t) {
		t.Run(test.name, func(t *testing.T) {
			_, err := Load(t.Context(), writeLockfileText(t, test.content))
			if err == nil {
				t.Fatal("Load returned nil error")
			}
			if !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("error = %q, want %q\ncontent:\n%s", err.Error(), test.wantError, test.content)
			}
		})
	}
}

func malformedCurrentLockfileCases(t *testing.T) []malformedLockfileCase {
	t.Helper()
	currentEnvelope := currentLockfileVersionEnvelope()
	exact := marshalLockfileForTest(t, lockfileWithSubjects(t, directSkillSubjectContract(t, "oracle")))
	twoExact := marshalLockfileForTest(t, lockfileWithSubjects(
		t,
		directSkillSubjectContract(t, "oracle"),
		directSkillSubjectContract(t, "review"),
	))
	mcp := marshalLockfileForTest(t, lockfileWithSubjects(t, claudeProjectMCPSubjectContract(t)))
	multiPackageMCP := marshalLockfileForTest(t, lockfileWithSubjects(
		t,
		claudeProjectMCPSubjectContractForCommand(t, "context7", "npx", []string{
			"--package=server@1.2.3",
			"--package=helper@latest",
			"server",
		}),
	))
	carrier := marshalLockfileForTest(t, lockfileWithSubjects(
		t,
		lockfileClaudePluginCarrierContract(t, "marketplace", "context7@official", "context7", "context7"),
	))
	twoMCP := marshalLockfileForTest(t, lockfileWithSubjects(
		t,
		claudeProjectMCPSubjectContractNamed(t, "context7"),
		claudeProjectMCPSubjectContractNamed(t, "review"),
	))
	repaired := marshalLockfileForTest(t, lockfileWithSubjects(t, repairedSkillSubjectContract(t)))
	fileUse, err := lock.NewExactFileUse(target.ScopeProject, false)
	if err != nil {
		t.Fatal(err)
	}
	fileSubject := snapshottest.ExactSupply(t, snapshottest.ExactSupplyInput{
		Kind: entity.KindHookAsset, Name: "guard", SourceID: "local:hooks/guard.sh?mode=vendor",
		ArtifactKind: artifact.ArtifactKindFile, ContentHash: artifact.HashFileContent([]byte("guard")),
		ExactFileUse: &fileUse,
	})
	fileUseLock := marshalLockfileForTest(t, lockfileWithSubjects(t, fileSubject))

	return []malformedLockfileCase{
		{
			name:      "old v1 schema",
			content:   replaceLockfileStringOnce(t, exact, currentEnvelope, "version = 1"),
			wantError: "unsupported lockfile version 1",
		},
		{
			name:    "relockable v5 schema",
			content: replaceLockfileStringOnce(t, exact, currentEnvelope, "version = 5"),
			wantError: fmt.Sprintf(
				"unsupported lockfile version 5; run daem lock to regenerate schema version %d",
				lock.CurrentVersion,
			),
		},
		{
			name:      "unsupported version",
			content:   replaceLockfileStringOnce(t, exact, currentEnvelope, "version = 999"),
			wantError: "unsupported lockfile version 999",
		},
		{
			name:      "unknown top-level key",
			content:   replaceLockfileStringOnce(t, exact, currentEnvelope, currentEnvelope+"\nmystery = true"),
			wantError: "unknown lockfile key",
		},
		{
			name:      "removed generated-at metadata",
			content:   replaceLockfileStringOnce(t, exact, currentEnvelope, currentEnvelope+"\ngenerated_at = \"2026-06-20T00:00:00Z\""),
			wantError: "unknown lockfile key \"generated_at\"",
		},
		{
			name: "legacy family array",
			content: exact + `
[[locked.skill]]
name = "legacy"
source_id = "local:skills/legacy?mode=vendor"
content_hash = "sha256:legacy"
`,
			wantError: "unknown lockfile key",
		},
		{
			name: "inline subject array",
			content: fmt.Sprintf(`
version = %d

[locked]
subject = []
`, lock.CurrentVersion),
			wantError: "locked.subject must use [[locked.subject]] array tables",
		},
		{
			name: "observed state smuggled into subject",
			content: replaceLockfileStringOnce(
				t,
				exact,
				`subject_id = "resource/skill/oracle"`,
				"subject_id = \"resource/skill/oracle\"\napproval_state = \"approved\"",
			),
			wantError: "unknown lockfile key",
		},
		{
			name: "runtime evidence smuggled into operation",
			content: replaceLockfileStringOnce(
				t,
				exact,
				`operation = "observe"`,
				"operation = \"observe\"\nlast_exit_code = 0",
			),
			wantError: "unknown lockfile key",
		},
		{
			name: "duplicate topology subject",
			content: replaceLockfileStringOnce(t, replaceLockfileStringOnce(
				t,
				twoExact,
				`subject_id = "resource/skill/review"`,
				`subject_id = "resource/skill/oracle"`,
			), `entity_id = "skill:review"`, `entity_id = "skill:oracle"`),
			wantError: `duplicate locked subject "resource/skill/oracle"`,
		},
		{
			name:      "non-canonical subject order",
			content:   swapFirstTwoArrayTableBlocks(t, twoExact, "[[locked.subject]]"),
			wantError: "lockfile contains non-canonical values",
		},
		{
			name:      "missing exact supply content hash",
			content:   replaceFirstContentHash(t, exact, ""),
			wantError: "exact artifact content hash is required",
		},
		{
			name: "file-backed skill",
			content: replaceLockfileStringAll(
				t,
				exact,
				`kind = "directory"`,
				`kind = "file"`,
			),
			wantError: "Skill exact Supply must be a directory",
		},
		{
			name: "directory-backed instructions",
			content: replaceLockfileStringOnce(
				t,
				replaceLockfileStringOnce(t, exact, `entity_id = "skill:oracle"`, `entity_id = "instructions:oracle"`),
				`subject_id = "resource/skill/oracle"`,
				`subject_id = "resource/instructions/oracle"`,
			),
			wantError: "Instructions exact Supply must be a file",
		},
		{
			name:      "hook asset without exact file use",
			content:   removeLockfileTable(t, fileUseLock, "[locked.subject.exact_file_use]"),
			wantError: "HookAsset exact Supply requires exact file use",
		},
		{
			name: "unsupported exact supply resource family",
			content: replaceLockfileStringOnce(
				t,
				replaceLockfileStringOnce(t, exact, `entity_id = "skill:oracle"`, `entity_id = "hook:oracle"`),
				`subject_id = "resource/skill/oracle"`,
				`subject_id = "resource/hook/oracle"`,
			),
			wantError: "subject has no current exact Supply family admission",
		},
		{
			name:      "unsupported ownership basis",
			content:   replaceLockfileStringOnce(t, exact, `ownership = "manifest"`, `ownership = "ambient"`),
			wantError: `ownership basis "ambient" is unsupported`,
		},
		{
			name:      "exact supply adopted ownership drift",
			content:   replaceLockfileStringOnce(t, exact, `ownership = "manifest"`, `ownership = "adopted"`),
			wantError: "Skill exact Supply contract does not match the admitted family refinement",
		},
		{
			name:      "exact supply absence policy drift",
			content:   replaceLockfileStringOnce(t, exact, `on_absent = "apply"`, `on_absent = "block"`),
			wantError: "Skill exact Supply contract does not match the admitted family refinement",
		},
		{
			name:      "exact supply replay drift",
			content:   replaceLockfileStringOnce(t, exact, `invocation = "unavailable"`, `invocation = "partial"`),
			wantError: "Skill exact Supply contract does not match the admitted family refinement",
		},
		{
			name:      "exact supply operation drift",
			content:   replaceLockfileStringOnce(t, exact, `verification = "exact_artifact"`, `verification = "none"`),
			wantError: "Skill exact Supply contract does not match the admitted family refinement",
		},
		{
			name:      "delegate runner and package mismatch",
			content:   replaceLockfileStringOnce(t, mcp, `runner_kind = "npx"`, `runner_kind = "plain"`),
			wantError: "delegate plan packages do not match canonical command inputs",
		},
		{
			name: "delegate package order drift",
			content: swapFirstTwoAdjacentArrayTableBlocks(
				t,
				multiPackageMCP,
				"[[locked.subject.delegate_plan.package]]",
			),
			wantError: "delegate plan packages do not match canonical command inputs",
		},
		{
			name: "missing delegate package",
			content: removeNthArrayTableBlock(
				t,
				multiPackageMCP,
				"[[locked.subject.delegate_plan.package]]",
				1,
			),
			wantError: "delegate plan packages do not match canonical command inputs",
		},
		{
			name:      "forged delegate pin policy",
			content:   replaceLockfileStringOnce(t, multiPackageMCP, `pin_policy = "floating"`, `pin_policy = "pinned"`),
			wantError: "delegate plan pin policy does not match canonical package assurance",
		},
		{
			name:      "MCP entity and subject key drift",
			content:   replaceLockfileStringOnce(t, mcp, `entity_id = "mcp_server:context7"`, `entity_id = "mcp_server:renamed"`),
			wantError: "does not match subject",
		},
		{
			name:      "MCP placement id drift",
			content:   replaceLockfileStringOnce(t, mcp, `placement_id = "claude-code.project.project-config"`, `placement_id = "future.project-config"`),
			wantError: "does not match the admitted placement profile",
		},
		{
			name:      "missing MCP contribution cardinality",
			content:   removeLockfileLine(t, mcp, `contribution_cardinality = "exclusive"`),
			wantError: "managed aggregate contribution cardinality",
		},
		{
			name:      "shared MCP contribution cardinality",
			content:   replaceLockfileStringOnce(t, mcp, `contribution_cardinality = "exclusive"`, `contribution_cardinality = "shared_set"`),
			wantError: "MCP realization does not match the admitted placement profile",
		},
		{
			name:      "padded MCP placement id",
			content:   replaceLockfileStringOnce(t, mcp, `placement_id = "claude-code.project.project-config"`, `placement_id = " claude-code.project.project-config "`),
			wantError: "aggregate placement id must be a stable token",
		},
		{
			name:      "unsorted MCP compared fields",
			content:   reverseFirstStringArrayField(t, mcp, "compared_fields"),
			wantError: "lockfile contains non-canonical values",
		},
		{
			name:      "duplicate MCP compared field",
			content:   duplicateFirstStringArrayFieldValue(t, mcp, "compared_fields"),
			wantError: "lockfile contains non-canonical values",
		},
		{
			name:      "padded MCP operation precondition",
			content:   padFirstStringArrayFieldValue(t, mcp, "preconditions"),
			wantError: "lockfile contains non-canonical values",
		},
		{
			name:      "carrier wrong source kind",
			content:   replaceLockfileStringOnce(t, carrier, `source_namespace = "marketplace:context7@official"`, `source_namespace = "host-source:https://github.com/acme/context7.git"`),
			wantError: `requires source kind "marketplace"`,
		},
		{
			name:      "carrier malformed marketplace selector",
			content:   replaceLockfileStringOnce(t, carrier, `source_namespace = "marketplace:context7@official"`, `source_namespace = "marketplace:context7"`),
			wantError: "marketplace source must be PLUGIN@MARKETPLACE",
		},
		{
			name:      "carrier option-looking selector",
			content:   replaceLockfileStringOnce(t, carrier, `source_namespace = "marketplace:context7@official"`, `source_namespace = "marketplace:--help@official"`),
			wantError: "extension source must not begin with '-'",
		},
		{
			name:      "unsorted carrier verified relation fields",
			content:   reverseFirstStringArrayField(t, carrier, "verified_relation_fields"),
			wantError: "lockfile contains non-canonical values",
		},
		{
			name: "MCP projection namespace drift",
			content: replaceLockfileStringOnce(
				t,
				mcp,
				`subject_id = "projection/claude-code.project.mcp-server/context7"`,
				`subject_id = "projection/claude-code.future.mcp-server/context7"`,
			),
			wantError: "require an implemented MCP placement",
		},
		{
			name:      "MCP target profile drift",
			content:   replaceLockfileStringOnce(t, mcp, `target = "claude-code"`, `target = "codex"`),
			wantError: "does not match the admitted placement profile",
		},
		{
			name:      "MCP codec contract drift",
			content:   replaceLockfileStringOnce(t, mcp, `codec_contract = "claude-project-mcp-stdio-v1"`, `codec_contract = "claude-project-mcp-stdio-v999"`),
			wantError: "does not match managed aggregate codec contract",
		},
		{
			name:      "MCP malformed canonical contribution",
			content:   replaceLockfileStringOnce(t, mcp, `canonical_contribution = "{\n`, `canonical_contribution = "not-json\n`),
			wantError: "aggregate codec canonical_contribution_invalid",
		},
		{
			name:      "MCP replay contract drift",
			content:   replaceLockfileStringOnce(t, mcp, `invocation = "exact"`, `invocation = "unavailable"`),
			wantError: "does not match the admitted lock refinement",
		},
		{
			name: "MCP operation precondition drift",
			content: replaceLockfileStringOnce(
				t,
				mcp,
				`"managed_subtree_absent_or_managed"`,
				`"managed_subtree_absent"`,
			),
			wantError: "does not match the admitted lock refinement",
		},
		{
			name: "duplicate MCP aggregate occupancy",
			content: replaceLockfileStringOnce(
				t,
				twoMCP,
				`content_path = "/mcpServers/review"`,
				`content_path = "/mcpServers/context7"`,
			),
			wantError: "duplicate exclusive managed aggregate occupancy",
		},
		{
			name: "realization with no variant body",
			content: exact + `
[locked.subject.realization]
`,
			wantError: "exactly one realization body is required",
		},
		{
			name: "realization with multiple variant bodies",
			content: mcp + `
[locked.subject.realization.managed_path]
`,
			wantError: "exactly one realization body is required",
		},
		{
			name: "unknown realization variant",
			content: replaceLockfileStringOnce(
				t,
				mcp,
				"[locked.subject.realization.managed_aggregate]",
				"[locked.subject.realization.future_variant]",
			),
			wantError: "unknown lockfile key",
		},
		{
			name:      "duplicate operation kind",
			content:   appendFirstOperationBlock(t, exact),
			wantError: `duplicate operation contract "observe"`,
		},
		{
			name: "exact file use on directory Supply",
			content: exact + `
[locked.subject.exact_file_use]
scope = "project"
executable = true
`,
			wantError: "exact file use requires exact file Supply identity",
		},
		{
			name:      "missing exact file use executable",
			content:   replaceLockfileStringOnce(t, fileUseLock, "executable = false\n", ""),
			wantError: "exact_file_use: executable is required",
		},
		{
			name: "malformed skill set declaration correlation",
			content: exact + `
[locked.subject.skill_set_member]
declaration_identity = "skill-set-declaration:v1:sha256:not-a-digest"
`,
			wantError: "invalid skill set declaration identity",
		},
		{
			name:      "repair on non-skill entity",
			content:   repairAsHook(t, repaired),
			wantError: "repair recipe requires Skill entity",
		},
		{
			name:      "width-congruent future repair recipe version",
			content:   replaceRecipeVersion(t, repaired, 1<<32+1),
			wantError: "repair recipe version 4294967297 is unsupported",
		},
		{
			name:      "maximum repair recipe wire version",
			content:   replaceRecipeVersion(t, repaired, 9223372036854775807),
			wantError: "repair recipe version 9223372036854775807 is unsupported",
		},
		{
			name:      "repair missing recipe hash",
			content:   replaceRecipeHash(t, repaired, ""),
			wantError: "does not match canonical hash",
		},
		{
			name:      "repair unknown operation kind",
			content:   replaceLockfileStringOnce(t, repaired, `kind = "rename"`, `kind = "rewrite_description"`),
			wantError: `unknown repair operation kind "rewrite_description"`,
		},
		{
			name:      "repair missing old value presence",
			content:   removeFirstRepairOldValuePresence(t, repaired),
			wantError: "old_value_present is required",
		},
		{
			name:      "repair path traversal",
			content:   replaceLockfileStringOnce(t, repaired, `from = "skill.md"`, `from = "../skill.md"`),
			wantError: `repair path "../skill.md" is not a canonical relative file path`,
		},
	}
}

func TestLoadRejectsTruncatedV2Lockfile(t *testing.T) {
	content := marshalLockfileForTest(t, lockfileWithSubjects(t, directSkillSubjectContract(t, "oracle")))
	_, err := Load(t.Context(), writeLockfileText(t, content+"\n[locked.subject"))
	if err == nil {
		t.Fatal("Load returned nil error for truncated lockfile")
	}
}

func TestLoadRejectsInvalidTextEncoding(t *testing.T) {
	content := marshalLockfileForTest(t, lockfileWithSubjects(t, directSkillSubjectContract(t, "oracle")))
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{name: "invalid UTF-8", content: string([]byte{0xff, 0xfe}) + content, want: "lockfile is not valid UTF-8"},
		{name: "embedded NUL", content: strings.Replace(content, currentLockfileVersionEnvelope(), currentLockfileVersionEnvelope()+"\x00", 1)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := Load(t.Context(), writeLockfileText(t, test.content))
			if err == nil {
				t.Fatal("Load accepted invalid lockfile text")
			}
			if test.want != "" && !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Load error = %q, want %q", err, test.want)
			}
		})
	}
}

func marshalLockfileForTest(t *testing.T, file lock.File) string {
	t.Helper()
	content, err := Marshal(file)
	if err != nil {
		t.Fatalf("Marshal returned error: %v", err)
	}
	return string(content)
}

func replaceLockfileStringOnce(t *testing.T, content string, old string, replacement string) string {
	t.Helper()
	if !strings.Contains(content, old) {
		t.Fatalf("lockfile content missing %q:\n%s", old, content)
	}
	return strings.Replace(content, old, replacement, 1)
}

func removeLockfileLine(t *testing.T, content string, value string) string {
	t.Helper()
	start := strings.Index(content, value)
	if start < 0 {
		t.Fatalf("lockfile content is missing %q:\n%s", value, content)
	}
	lineStart := strings.LastIndex(content[:start], "\n") + 1
	lineEndOffset := strings.IndexByte(content[start:], '\n')
	if lineEndOffset < 0 {
		return content[:lineStart]
	}
	return content[:lineStart] + content[start+lineEndOffset+1:]
}

func replaceLockfileStringAll(t *testing.T, content string, old string, replacement string) string {
	t.Helper()
	if !strings.Contains(content, old) {
		t.Fatalf("lockfile content missing %q:\n%s", old, content)
	}
	return strings.ReplaceAll(content, old, replacement)
}

func removeLockfileTable(t *testing.T, content string, header string) string {
	t.Helper()
	start := strings.Index(content, header)
	if start < 0 {
		t.Fatalf("lockfile content is missing table %q:\n%s", header, content)
	}
	tail := content[start+len(header):]
	before, _, ok := strings.Cut(tail, "[locked.subject.")
	if !ok {
		return strings.TrimRight(content[:start], "\n") + "\n"
	}
	lineStart := strings.LastIndex(before, "\n") + 1
	return content[:start] + tail[lineStart:]
}

func reverseFirstStringArrayField(t *testing.T, content string, field string) string {
	t.Helper()
	return mutateFirstStringArrayField(t, content, field, func(values []string) []string {
		for left, right := 0, len(values)-1; left < right; left, right = left+1, right-1 {
			values[left], values[right] = values[right], values[left]
		}
		return values
	})
}

func padFirstStringArrayFieldValue(t *testing.T, content string, field string) string {
	t.Helper()
	return mutateFirstStringArrayField(t, content, field, func(values []string) []string {
		if len(values) == 0 {
			t.Fatalf("lockfile field %q has no values:\n%s", field, content)
		}
		values[0] = " " + values[0] + " "
		return values
	})
}

func duplicateFirstStringArrayFieldValue(t *testing.T, content string, field string) string {
	t.Helper()
	return mutateFirstStringArrayField(t, content, field, func(values []string) []string {
		if len(values) == 0 {
			t.Fatalf("lockfile field %q has no values:\n%s", field, content)
		}
		return append(values, values[0])
	})
}

func mutateFirstStringArrayField(
	t *testing.T,
	content string,
	field string,
	mutate func([]string) []string,
) string {
	t.Helper()
	marker := field + " = ["
	start := strings.Index(content, marker)
	if start < 0 {
		t.Fatalf("lockfile content is missing %q:\n%s", field, content)
	}
	lineEnd := strings.IndexByte(content[start:], '\n')
	if lineEnd < 0 {
		lineEnd = len(content) - start
	}
	lineEnd += start
	arrayStart := start + len(field) + len(" = ")
	var values []string
	if err := json.Unmarshal([]byte(content[arrayStart:lineEnd]), &values); err != nil {
		t.Fatalf("decode lockfile field %q: %v", field, err)
	}
	encoded, err := json.Marshal(mutate(values))
	if err != nil {
		t.Fatalf("encode lockfile field %q: %v", field, err)
	}
	return content[:arrayStart] + string(encoded) + content[lineEnd:]
}

func swapFirstTwoArrayTableBlocks(t *testing.T, content string, marker string) string {
	t.Helper()
	first := strings.Index(content, marker)
	if first < 0 {
		t.Fatalf("lockfile content is missing first %q:\n%s", marker, content)
	}
	secondOffset := strings.Index(content[first+len(marker):], marker)
	if secondOffset < 0 {
		t.Fatalf("lockfile content is missing second %q:\n%s", marker, content)
	}
	second := first + len(marker) + secondOffset
	return content[:first] + content[second:] + content[first:second]
}

func swapFirstTwoAdjacentArrayTableBlocks(t *testing.T, content string, marker string) string {
	t.Helper()
	firstStart, firstEnd := nthArrayTableBlockBounds(t, content, marker, 0)
	secondStart, secondEnd := nthArrayTableBlockBounds(t, content, marker, 1)
	if firstEnd != secondStart {
		t.Fatalf("lockfile %q tables are not adjacent:\n%s", marker, content)
	}
	return content[:firstStart] + content[secondStart:secondEnd] + content[firstStart:firstEnd] + content[secondEnd:]
}

func removeNthArrayTableBlock(t *testing.T, content string, marker string, occurrence int) string {
	t.Helper()
	start, end := nthArrayTableBlockBounds(t, content, marker, occurrence)
	return content[:start] + content[end:]
}

func nthArrayTableBlockBounds(t *testing.T, content string, marker string, occurrence int) (int, int) {
	t.Helper()
	searchStart := 0
	markerStart := -1
	for index := 0; index <= occurrence; index++ {
		offset := strings.Index(content[searchStart:], marker)
		if offset < 0 {
			t.Fatalf("lockfile content is missing %q occurrence %d:\n%s", marker, occurrence, content)
		}
		markerStart = searchStart + offset
		searchStart = markerStart + len(marker)
	}
	start := strings.LastIndex(content[:markerStart], "\n") + 1
	end := nextTOMLTableLineStart(content, searchStart)
	if end < 0 {
		end = len(content)
	}
	return start, end
}

func nextTOMLTableLineStart(content string, searchStart int) int {
	for cursor := searchStart; cursor < len(content); {
		offset := strings.IndexByte(content[cursor:], '\n')
		if offset < 0 {
			return -1
		}
		lineStart := cursor + offset + 1
		lineEnd := len(content)
		if lineOffset := strings.IndexByte(content[lineStart:], '\n'); lineOffset >= 0 {
			lineEnd = lineStart + lineOffset
		}
		if strings.HasPrefix(strings.TrimSpace(content[lineStart:lineEnd]), "[") {
			return lineStart
		}
		cursor = lineStart
	}
	return -1
}

func replaceFirstContentHash(t *testing.T, content string, replacement string) string {
	t.Helper()
	start := strings.Index(content, `content_hash = "`)
	if start < 0 {
		t.Fatalf("lockfile content is missing content_hash:\n%s", content)
	}
	valueStart := start + len(`content_hash = "`)
	valueEnd := strings.Index(content[valueStart:], `"`)
	if valueEnd < 0 {
		t.Fatalf("lockfile content has unterminated content_hash:\n%s", content)
	}
	valueEnd += valueStart
	return content[:valueStart] + replacement + content[valueEnd:]
}

func replaceRecipeHash(t *testing.T, content string, replacement string) string {
	t.Helper()
	section := strings.Index(content, "[locked.subject.repair_recipe]")
	if section < 0 {
		t.Fatalf("lockfile content is missing repair recipe:\n%s", content)
	}
	prefix := content[:section]
	tail := content[section:]
	return prefix + replaceFirstNamedStringValue(t, tail, "recipe_hash", replacement)
}

func replaceRecipeVersion(t *testing.T, content string, replacement int64) string {
	t.Helper()
	section := strings.Index(content, "[locked.subject.repair_recipe]")
	if section < 0 {
		t.Fatalf("lockfile content is missing repair recipe:\n%s", content)
	}
	prefix := content[:section]
	tail := content[section:]
	return prefix + replaceLockfileStringOnce(t, tail, "version = 1", fmt.Sprintf("version = %d", replacement))
}

func removeFirstRepairOldValuePresence(t *testing.T, content string) string {
	t.Helper()
	const marker = "old_value_present = "
	start := strings.Index(content, marker)
	if start < 0 {
		t.Fatalf("lockfile content is missing old_value_present:\n%s", content)
	}
	end := strings.IndexByte(content[start:], '\n')
	if end < 0 {
		return content[:start]
	}
	return content[:start] + content[start+end+1:]
}

func replaceFirstNamedStringValue(t *testing.T, content string, name string, replacement string) string {
	t.Helper()
	marker := name + ` = "`
	start := strings.Index(content, marker)
	if start < 0 {
		t.Fatalf("lockfile content is missing %s:\n%s", name, content)
	}
	valueStart := start + len(marker)
	valueEnd := strings.Index(content[valueStart:], `"`)
	if valueEnd < 0 {
		t.Fatalf("lockfile content has unterminated %s:\n%s", name, content)
	}
	valueEnd += valueStart
	return content[:valueStart] + replacement + content[valueEnd:]
}

func repairAsHook(t *testing.T, content string) string {
	t.Helper()
	content = replaceLockfileStringOnce(t, content, `entity_id = "skill:oracle"`, `entity_id = "hook:oracle"`)
	return replaceLockfileStringOnce(t, content, `subject_id = "resource/skill/oracle"`, `subject_id = "resource/hook/oracle"`)
}

func appendFirstOperationBlock(t *testing.T, content string) string {
	t.Helper()
	const marker = "[[locked.subject.operation]]"
	start := strings.Index(content, marker)
	if start < 0 {
		t.Fatalf("lockfile content is missing an operation block:\n%s", content)
	}
	end := strings.Index(content[start+len(marker):], marker)
	if end < 0 {
		end = len(content)
	} else {
		end += start + len(marker)
	}
	return content + "\n" + content[start:end]
}
