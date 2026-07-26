package journal

import (
	"slices"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/aggregate"
	mcpcodec "github.com/isty2e/daem/internal/realization/aggregate/codec/mcp"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
)

func TestEntrySelectionsPreserveHostMutationOrderAndIdentity(t *testing.T) {
	first := testManagedPathCreateMutation(t, "oracle", ".agents/skills/oracle")
	second := testManagedPathCreateMutation(t, "review", ".agents/skills/review")

	selections, err := EntrySelections([]ManagedPathMutation{first, second}, nil)
	if err != nil {
		t.Fatalf("EntrySelections returned error: %v", err)
	}
	if len(selections) != 2 {
		t.Fatalf("selections = %d, want 2", len(selections))
	}
	if selections[0].key.destination != ".agents/skills/oracle" ||
		selections[1].key.destination != ".agents/skills/review" {
		t.Fatalf("selection order = %#v, want host mutation order", selections)
	}
	if !selections[0].initialized || selections[0].key.subject != first.facts().subject {
		t.Fatalf("first selection = %#v, want initialized exact identity", selections[0])
	}
}

func TestEntrySelectionsRejectInvalidAndDuplicateMutations(t *testing.T) {
	valid := testManagedPathCreateMutation(t, "oracle", ".agents/skills/oracle")
	if _, err := EntrySelections([]ManagedPathMutation{{}}, nil); err == nil || !strings.Contains(err.Error(), "managed path journal mutation[0]") {
		t.Fatalf("invalid mutation error = %v, want indexed validation failure", err)
	}
	if _, err := EntrySelections([]ManagedPathMutation{valid, valid}, nil); err == nil || !strings.Contains(err.Error(), "duplicate recovery entry selection") {
		t.Fatalf("duplicate mutation error = %v, want duplicate selection failure", err)
	}
	if _, err := EntrySelections(nil, []ManagedAggregateMutation{{}}); err == nil || !strings.Contains(err.Error(), "managed aggregate journal mutation[0]") {
		t.Fatalf("invalid aggregate mutation error = %v, want indexed validation failure", err)
	}
}

func TestEntrySelectionPreservesCanonicalSubjectIdentity(t *testing.T) {
	subject, err := topology.NewSubjectID(topology.SubjectProjection, "claude-code.project.mcp-server", "context7")
	if err != nil {
		t.Fatalf("NewSubjectID returned error: %v", err)
	}
	mutation := pathMutation{
		Subject:     subject,
		Target:      target.TargetClaudeCode,
		Scope:       target.ScopeProject,
		Destination: aggregate.ClaudeProjectMCPConfigPath,
		ContentPath: output.ContentPath(mcpcodec.ClaudeProjectMCPContentPath("context7")),
	}

	selection := EntrySelection{key: entrySelectionKeyFromMutation(mutation), initialized: true}
	if selection.key.subject != subject {
		t.Fatalf("selection = %#v, want exact subject identity", selection)
	}
}

func TestRecoveryIngressPreservesCanonicalSubjectID(t *testing.T) {
	entry := subjectOwnedRecoveryEntry("context/가")
	entry.Path = "AGENTS.md"
	entry.ContentPath = ""
	entry.Before.PathExisted = false
	entry.Before.ParentExisted = false
	entry.ExpectedAfter.PathExisted = false
	entryKey, err := recoveryStateKeyForEntry(entry)
	if err != nil {
		t.Fatalf("recoveryStateKeyForEntry returned error: %v", err)
	}
	want, err := topology.NewSubjectID(
		topology.SubjectProjection,
		"claude-code.project.mcp-server",
		"context/가",
	)
	if err != nil {
		t.Fatalf("NewSubjectID returned error: %v", err)
	}
	action, err := recoveryActionFromEntryForTest(entry)
	if err != nil {
		t.Fatalf("recoveryActionFromEntry returned error: %v", err)
	}
	gotSubject, hasSubject := action.SubjectID()
	if !hasSubject || gotSubject != want {
		t.Fatalf("recovery action subject = %q/%t, want %q", gotSubject, hasSubject, want)
	}
	if entryKey.subject != want {
		t.Fatalf("recovery key subject = %q, want %q", entryKey.subject, want)
	}
}

func subjectOwnedRecoveryEntry(name string) recoveryEntry {
	entry := defaultRecoveryEntry()
	entry.Subject = persistedSubjectRef{
		Kind:      string(topology.SubjectProjection),
		Namespace: "claude-code.project.mcp-server",
		Name:      name,
	}
	entry.Target = string(target.TargetClaudeCode)
	entry.Targets = nil
	entry.Path = string(aggregate.ClaudeProjectMCPConfigPath)
	entry.ContentPath = string(mcpcodec.ClaudeProjectMCPContentPath(name))
	entry.ContentKind = ""
	entry.Before.PathExisted = true
	entry.Before.ParentExisted = true
	entry.ExpectedAfter.PathExisted = true
	return entry
}

func TestSelectedRecoveryEntriesPreserveJournalOrderAndExactAuthority(t *testing.T) {
	first := defaultRecoveryEntry()
	second := recoveryEntryFor("second", "CLAUDE.md", "sha256:second-before", "sha256:second-after", "backups/CLAUDE.md")
	third := recoveryEntryFor("third", "GEMINI.md", "sha256:third-before", "sha256:third-after", "backups/GEMINI.md")
	selected := []EntrySelection{
		entrySelectionForRecoveryEntry(t, third),
		entrySelectionForRecoveryEntry(t, first),
	}

	indexes, err := selectedRecoveryEntryIndexes([]recoveryEntry{first, second, third}, selected)
	if err != nil {
		t.Fatalf("selectedRecoveryEntryIndexes returned error: %v", err)
	}
	if !slices.Equal(indexes, []int{0, 2}) {
		t.Fatalf("selected indexes = %#v, want journal order [0 2]", indexes)
	}
}

func TestSelectedRecoveryEntriesRejectInvalidAuthority(t *testing.T) {
	entry := defaultRecoveryEntry()
	selection := entrySelectionForRecoveryEntry(t, entry)
	if _, err := selectedRecoveryEntryIndexes([]recoveryEntry{entry}, []EntrySelection{{}}); err == nil || !strings.Contains(err.Error(), "uninitialized") {
		t.Fatalf("zero selection error = %v, want uninitialized failure", err)
	}
	if _, err := selectedRecoveryEntryIndexes([]recoveryEntry{entry}, []EntrySelection{selection, selection}); err == nil || !strings.Contains(err.Error(), "duplicate selected recovery entry") {
		t.Fatalf("duplicate selection error = %v", err)
	}

	unknown := selection
	unknown.key.destination = "UNKNOWN.md"
	if _, err := selectedRecoveryEntryIndexes([]recoveryEntry{entry}, []EntrySelection{unknown}); err == nil || !strings.Contains(err.Error(), "do not match the active journal") {
		t.Fatalf("unknown selection error = %v", err)
	}
}

func entrySelectionForRecoveryEntry(t *testing.T, entry recoveryEntry) EntrySelection {
	t.Helper()
	key, err := entrySelectionKeyFromRecoveryEntry(entry)
	if err != nil {
		t.Fatalf("entrySelectionKeyFromRecoveryEntry returned error: %v", err)
	}
	return EntrySelection{key: key, initialized: true}
}

func testManagedPathCreateMutation(t *testing.T, name string, destination output.Destination) ManagedPathMutation {
	t.Helper()
	mutation, err := NewManagedPathCreateMutation(
		testManagedPathSubject(t, name),
		[]target.Target{target.TargetCodex},
		target.ScopeProject,
		destination,
		testContentHash("desired-"+name),
		"directory",
		0,
		nil,
	)
	if err != nil {
		t.Fatalf("NewManagedPathCreateMutation returned error: %v", err)
	}
	return mutation
}
