package journal

import (
	"os"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/aggregate/codec"
	hookcodec "github.com/isty2e/daem/internal/realization/aggregate/codec/hook"
	mcpcodec "github.com/isty2e/daem/internal/realization/aggregate/codec/mcp"
	"github.com/isty2e/daem/internal/realization/aggregate/hook"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
	topologymcp "github.com/isty2e/daem/internal/topology/mcp"
)

func TestManagedAggregateMutationTransitionLaw(t *testing.T) {
	for _, test := range []struct {
		name            string
		kind            AggregateMutationKind
		beforePresent   bool
		expectedPresent bool
		wantError       bool
	}{
		{name: "create", kind: AggregateMutationCreate, expectedPresent: true},
		{name: "replace", kind: AggregateMutationReplace, beforePresent: true, expectedPresent: true},
		{name: "remove", kind: AggregateMutationRemove, beforePresent: true},
		{name: "create over present projection", kind: AggregateMutationCreate, beforePresent: true, expectedPresent: true, wantError: true},
		{name: "replace missing projection", kind: AggregateMutationReplace, expectedPresent: true, wantError: true},
		{name: "remove missing projection", kind: AggregateMutationRemove, wantError: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := validateAggregateMutationTransition(test.kind, test.beforePresent, test.expectedPresent)
			if (err != nil) != test.wantError {
				t.Fatalf("validateAggregateMutationTransition(%q, %t, %t) error = %v", test.kind, test.beforePresent, test.expectedPresent, err)
			}
		})
	}
}

func TestManagedAggregateMutationSeparatesProjectionAndDocumentBeforeFacts(t *testing.T) {
	placement, ok := aggregate.HookPlacementFor(target.TargetClaudeCode, target.ScopeProject)
	if !ok {
		t.Fatal("Claude project Hook placement is missing")
	}
	canonical, err := hookcodec.CanonicalHookContribution(commandhook.ContributionInput{
		Event: "PostToolUse", Type: "command", Command: "true",
	})
	if err != nil {
		t.Fatalf("CanonicalHookContribution returned error: %v", err)
	}
	contribution, err := placement.Contribution(canonical)
	if err != nil {
		t.Fatalf("Hook contribution returned error: %v", err)
	}
	subject, err := topology.NewSubjectID(topology.SubjectProjection, string(placement.ID()), "hook:format")
	if err != nil {
		t.Fatalf("NewSubjectID returned error: %v", err)
	}
	subjectContribution, err := aggregate.NewSubjectContribution(subject, contribution)
	if err != nil {
		t.Fatalf("NewSubjectContribution returned error: %v", err)
	}
	desired, err := aggregate.NewContributionSet([]aggregate.SubjectContribution{subjectContribution})
	if err != nil {
		t.Fatalf("NewContributionSet returned error: %v", err)
	}
	selection, err := aggregate.NewSelection([]aggregate.ProjectionContract{contribution.Contract()})
	if err != nil {
		t.Fatalf("NewSelection returned error: %v", err)
	}
	codec, ok := aggregatecodec.Catalog().Lookup(placement.CodecContractID())
	if !ok {
		t.Fatal("Hook codec is missing")
	}
	beforeDocument := aggregate.ExistingDocument([]byte("{\n  \"env\": {\"TOKEN\": \"keep\"}\n}\n"))
	beforeSnapshot, failure := codec.Read(beforeDocument, selection)
	if failure != nil {
		t.Fatalf("Read returned failure: %v", failure)
	}
	intent, err := aggregate.NewProjectionIntent(beforeSnapshot.States()[0], &desired)
	if err != nil {
		t.Fatalf("NewProjectionIntent returned error: %v", err)
	}
	plan, err := aggregate.NewPlan(beforeSnapshot, []aggregate.ProjectionIntent{intent})
	if err != nil {
		t.Fatalf("NewPlan returned error: %v", err)
	}
	expected, failure := codec.Render(beforeDocument, plan)
	if failure != nil {
		t.Fatalf("Render returned failure: %v", failure)
	}
	mutation, err := NewManagedAggregateMutation(
		AggregateMutationCreate,
		subject,
		contribution.Contract(),
		beforeDocument,
		beforeSnapshot,
		expected,
		os.FileMode(0o600),
	)
	if err != nil {
		t.Fatalf("NewManagedAggregateMutation returned error: %v", err)
	}

	path := pathMutationFromAggregate(mutation)
	if path.LiveExists || path.LiveHash != "" {
		t.Fatalf("projection before facts = (%t, %q), want absent", path.LiveExists, path.LiveHash)
	}
	if !path.ExpectedExists || !path.ExpectedPathExists || path.ExpectedPathMode != aggregate.DocumentFileMode {
		t.Fatalf(
			"projection expected facts = (%t, %t, %o), want present projection in mode-%04o document",
			path.ExpectedExists,
			path.ExpectedPathExists,
			path.ExpectedPathMode,
			aggregate.DocumentFileMode,
		)
	}
	wantDocumentHash := documentHash(beforeDocument, 0o600)
	if !path.LivePathExists || path.LivePathHash != wantDocumentHash {
		t.Fatalf(
			"document before facts = (%t, %q), want existing hash %q",
			path.LivePathExists,
			path.LivePathHash,
			wantDocumentHash,
		)
	}
	zeroModeMutation, err := NewManagedAggregateMutation(
		AggregateMutationCreate,
		subject,
		contribution.Contract(),
		beforeDocument,
		beforeSnapshot,
		expected,
		0,
	)
	if err != nil {
		t.Fatalf("NewManagedAggregateMutation with mode 0000 returned error: %v", err)
	}
	zeroModePath := pathMutationFromAggregate(zeroModeMutation)
	if zeroModePath.LivePathHash != artifact.HashFileContent(beforeDocument.Content()) {
		t.Fatalf("mode 0000 path hash = %q, want non-executable document hash", zeroModePath.LivePathHash)
	}
	selections, err := EntrySelections(nil, []ManagedAggregateMutation{mutation})
	if err != nil {
		t.Fatalf("EntrySelections returned error: %v", err)
	}
	if len(selections) != 1 || !selections[0].initialized ||
		selections[0].key != entrySelectionKeyFromMutation(path) {
		t.Fatalf("aggregate selections = %#v, want exact initialized projection identity", selections)
	}
}

func TestManagedAggregateMutationCorrelatesMCPSubjectAndContractAtConstruction(t *testing.T) {
	placement, ok := aggregate.ImplementedMCPPlacement(target.TargetClaudeCode, target.ScopeProject)
	if !ok {
		t.Fatal("Claude project MCP placement is missing")
	}
	canonical, err := mcpcodec.CanonicalClaudeProjectMCPServerEntry(mcpcodec.ClaudeProjectMCPServerProjection{
		ServerID: "context7", Command: "npx", Args: []string{"-y", "@example/server"},
		AdapterContract: aggregate.ClaudeProjectMCPStdioAdapterV1,
	})
	if err != nil {
		t.Fatalf("CanonicalClaudeProjectMCPServerEntry returned error: %v", err)
	}
	contribution, err := placement.Contribution("context7", string(canonical))
	if err != nil {
		t.Fatalf("MCP contribution returned error: %v", err)
	}
	validSubject, err := topologymcp.ProjectionSubject(placement.Target(), placement.Scope(), "context7")
	if err != nil {
		t.Fatalf("ProjectionSubject returned error: %v", err)
	}
	subjectContribution, err := aggregate.NewSubjectContribution(validSubject, contribution)
	if err != nil {
		t.Fatalf("NewSubjectContribution returned error: %v", err)
	}
	desired, err := aggregate.NewContributionSet([]aggregate.SubjectContribution{subjectContribution})
	if err != nil {
		t.Fatalf("NewContributionSet returned error: %v", err)
	}
	selection, err := aggregate.NewSelection([]aggregate.ProjectionContract{contribution.Contract()})
	if err != nil {
		t.Fatalf("NewSelection returned error: %v", err)
	}
	codec, ok := aggregatecodec.Catalog().Lookup(placement.CodecContractID())
	if !ok {
		t.Fatal("Claude project MCP codec is missing")
	}
	beforeDocument := aggregate.AbsentDocument()
	beforeSnapshot, failure := codec.Read(beforeDocument, selection)
	if failure != nil {
		t.Fatalf("Read returned failure: %v", failure)
	}
	intent, err := aggregate.NewProjectionIntent(beforeSnapshot.States()[0], &desired)
	if err != nil {
		t.Fatalf("NewProjectionIntent returned error: %v", err)
	}
	plan, err := aggregate.NewPlan(beforeSnapshot, []aggregate.ProjectionIntent{intent})
	if err != nil {
		t.Fatalf("NewPlan returned error: %v", err)
	}
	expected, failure := codec.Render(beforeDocument, plan)
	if failure != nil {
		t.Fatalf("Render returned failure: %v", failure)
	}

	wrongServer, err := topologymcp.ProjectionSubject(placement.Target(), placement.Scope(), "other")
	if err != nil {
		t.Fatalf("wrong-server ProjectionSubject returned error: %v", err)
	}
	wrongFamily, err := topology.NewSubjectID(topology.SubjectProjection, string(placement.ID()), "hook:context7")
	if err != nil {
		t.Fatalf("wrong-family NewSubjectID returned error: %v", err)
	}
	for _, test := range []struct {
		name    string
		subject topology.SubjectID
	}{
		{name: "wrong server", subject: wrongServer},
		{name: "wrong family", subject: wrongFamily},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := NewManagedAggregateMutation(
				AggregateMutationCreate,
				test.subject,
				contribution.Contract(),
				beforeDocument,
				beforeSnapshot,
				expected,
				0,
			)
			if err == nil || !strings.Contains(err.Error(), "does not match placement server") {
				t.Fatalf("NewManagedAggregateMutation error = %v, want subject-contract mismatch", err)
			}
		})
	}
}

func TestRecoveryActionRestoresPersistedAggregateContract(t *testing.T) {
	contract := recoveryTestAggregateContract(t)
	placement, ok := aggregate.HookPlacementFor(target.TargetClaudeCode, target.ScopeProject)
	if !ok {
		t.Fatal("Claude project Hook placement is missing")
	}
	subject, err := topology.NewSubjectID(topology.SubjectProjection, string(placement.ID()), "hook:format")
	if err != nil {
		t.Fatalf("NewSubjectID returned error: %v", err)
	}
	entry := recoveryEntry{
		Subject: persistedSubjectRef{
			Kind: string(subject.Kind()), Namespace: subject.Namespace(), Name: subject.Key(),
		},
		Target: string(target.TargetClaudeCode), Scope: string(target.ScopeProject),
		Path: ".claude/settings.json", ContentPath: "/hooks",
		Aggregate: persistedAggregateContract(contract),
	}

	action, err := recoveryActionFromEntryForTest(entry)
	if err != nil {
		t.Fatalf("recoveryActionFromEntry returned error: %v", err)
	}
	gotSubject, hasSubject := action.SubjectID()
	if !hasSubject || gotSubject != subject {
		t.Fatalf("subject = %v/%t, want %v", gotSubject, hasSubject, subject)
	}
	if action.AggregateContract == nil || !action.AggregateContract.Equal(contract) {
		t.Fatalf("aggregate contract = %#v, want %#v", action.AggregateContract, contract)
	}
}

func TestRecoveryActionRejectsContentPathWithoutPersistedContract(t *testing.T) {
	placement, ok := aggregate.HookPlacementFor(target.TargetClaudeCode, target.ScopeProject)
	if !ok {
		t.Fatal("Claude project Hook placement is missing")
	}
	subject, err := topology.NewSubjectID(topology.SubjectProjection, string(placement.ID()), "hook:format")
	if err != nil {
		t.Fatalf("NewSubjectID returned error: %v", err)
	}
	entry := recoveryEntry{
		Subject: persistedSubjectRef{
			Kind: string(subject.Kind()), Namespace: subject.Namespace(), Name: subject.Key(),
		},
		Target: string(target.TargetClaudeCode), Scope: string(target.ScopeProject),
		Path: ".claude/settings.json", ContentPath: "/hooks",
	}

	_, err = recoveryActionFromEntryForTest(entry)
	if err == nil || !strings.Contains(err.Error(), "projection contract is required") {
		t.Fatalf("recoveryActionFromEntry error = %v, want missing persisted contract rejection", err)
	}
}

func TestRecoveryActionRejectsMalformedPersistedAggregateContract(t *testing.T) {
	persisted := persistedAggregateContract(recoveryTestAggregateContract(t))
	persisted.ContentPath = ""
	entry := defaultRecoveryEntry()
	entry.Aggregate = persisted

	_, err := recoveryActionFromEntryForTest(entry)
	if err == nil || !strings.Contains(err.Error(), "recovery aggregate contract") {
		t.Fatalf("recoveryActionFromEntry error = %v, want aggregate contract error", err)
	}
}

func recoveryTestAggregateContract(t *testing.T) aggregate.ProjectionContract {
	t.Helper()
	placement, ok := aggregate.HookPlacementFor(target.TargetClaudeCode, target.ScopeProject)
	if !ok {
		t.Fatal("Claude project Hook placement is missing")
	}
	canonical, err := hookcodec.CanonicalHookContribution(commandhook.ContributionInput{
		Event: "PostToolUse", Type: "command", Command: "true",
	})
	if err != nil {
		t.Fatalf("CanonicalHookContribution returned error: %v", err)
	}
	contribution, err := placement.Contribution(canonical)
	if err != nil {
		t.Fatalf("Hook contribution returned error: %v", err)
	}
	return contribution.Contract()
}
