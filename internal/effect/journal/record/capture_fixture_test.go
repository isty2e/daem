package record

import (
	"os"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/observe"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization"
	"github.com/isty2e/daem/internal/realization/aggregate"
	mcpcodec "github.com/isty2e/daem/internal/realization/aggregate/codec/mcp"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/target"
	topologymcp "github.com/isty2e/daem/internal/topology/mcp"
)

func ordinaryEvidenceFixture(t *testing.T, createdAt string) (Native, OrdinaryEvidence) {
	t.Helper()
	fixture := nativeJournalFixture(t)
	input, before := ordinaryRecordDraftInput(t)
	journal := Native{record: fixture.record, owner: fixture.claims.owner, before: input.before, after: input.after}
	journal.record.OperationID, journal.record.CreatedAt, journal.record.ManifestRootProvenance = input.operationID, createdAt, persistedNativeRoot(input.manifest)
	basis, err := journal.OrdinaryCaptureBasis()
	if err != nil {
		t.Fatal(err)
	}
	draft, err := NewOrdinaryDraft(basis.OperationID(), basis.CreatedAt(), basis.Before(), basis.After(), input.manifest, input.mutations, input.removalDemands)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := draft.BeginEntry(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := draft.SupplyBefore(before); err != nil {
		t.Fatal(err)
	}
	if err := draft.SupplyGlobalBinding(nil); err != nil {
		t.Fatal(err)
	}
	return journal, closeOrdinaryRecordDraft(t, draft)
}

func normalizedPathCapture(t *testing.T, kind CaptureKind, scope target.Scope, destination output.Destination, liveHash artifact.ContentHash, contentKind realization.PathProjectionContentKind) CaptureMutation {
	t.Helper()
	subject := testManagedPathSubject(t, "capture")
	consumers := []target.Target{target.TargetCodex}
	exists := kind != CaptureCreate
	mode, permissions := os.FileMode(0), realization.PathPermissionsNone
	if contentKind == realization.PathProjectionFile {
		mode, permissions = 0o600, realization.PathPermissionsExact
	}
	var mutation ManagedPathMutation
	var err error
	if !exists {
		mutation, err = NewManagedPathCreateMutation(subject, consumers, scope, destination, testContentHash("capture expected"), contentKind, mode, nil)
	} else {
		previous, previousErr := durable.NewManagedPathState(subject, consumers, scope, destination, liveHash, contentKind, permissions, mode)
		if previousErr != nil {
			t.Fatal(previousErr)
		}
		switch kind {
		case CaptureReplace:
			mutation, err = NewManagedPathReplaceMutation(subject, consumers, scope, destination, liveHash, liveHash, contentKind, mode, previous)
		case CaptureRemove:
			mutation, err = NewManagedPathRemoveMutation(previous, liveHash)
		case CaptureRecord:
			mutation, err = NewManagedPathRecordMutation(subject, consumers, scope, destination, liveHash, liveHash, contentKind, mode, &previous)
		default:
			t.Fatalf("unsupported capture fixture kind %q", kind)
		}
	}
	if err != nil {
		t.Fatal(err)
	}
	evidence := testManagedPathEvidence(t, subject, destination, exists, liveHash)
	values, err := NormalizeCaptureMutations([]ManagedPathMutation{mutation}, nil, []observe.ManagedPathEvidence{evidence})
	if err != nil || len(values) != 1 {
		t.Fatal("normalize managed capture fixture", err)
	}
	return values[0]
}

func claudeRecoveryBaselineMutation(t *testing.T, scope target.Scope, serverID string) CaptureMutation {
	t.Helper()
	placement, ok := aggregate.ImplementedMCPPlacement(target.TargetClaudeCode, scope)
	if !ok {
		t.Fatalf("Claude MCP placement for %s is missing", scope)
	}
	var canonical []byte
	var err error
	if scope == target.ScopeProject {
		canonical, err = mcpcodec.CanonicalClaudeProjectMCPServerEntry(mcpcodec.ClaudeProjectMCPServerProjection{ServerID: serverID, Command: "npx", AdapterContract: aggregate.ClaudeProjectMCPStdioAdapterV1})
	} else {
		canonical, err = mcpcodec.CanonicalClaudeGlobalMCPServerEntry(mcpcodec.ClaudeGlobalMCPServerProjection{ServerID: serverID, Command: "npx", AdapterContract: aggregate.ClaudeGlobalMCPStdioEnvAdapterV1})
	}
	if err != nil {
		t.Fatal(err)
	}
	contribution, err := placement.Contribution(serverID, string(canonical))
	if err != nil {
		t.Fatal(err)
	}
	subject, err := topologymcp.ProjectionSubject(target.TargetClaudeCode, scope, serverID)
	if err != nil {
		t.Fatal(err)
	}
	selected, err := aggregate.NewSubjectContribution(subject, contribution)
	if err != nil {
		t.Fatal(err)
	}
	desired, err := aggregate.NewContributionSet([]aggregate.SubjectContribution{selected})
	if err != nil {
		t.Fatal(err)
	}
	selection, err := aggregate.NewSelection([]aggregate.ProjectionContract{contribution.Contract()})
	if err != nil {
		t.Fatal(err)
	}
	codec, ok := journalTestCodecs().Lookup(placement.CodecContractID())
	if !ok {
		t.Fatal("MCP fixture codec is missing")
	}
	before := aggregate.AbsentDocument()
	snapshot, failure := codec.Read(before, selection)
	if failure != nil {
		t.Fatal(failure)
	}
	intent, err := aggregate.NewProjectionIntent(snapshot.States()[0], &desired)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := aggregate.NewPlan(snapshot, []aggregate.ProjectionIntent{intent})
	if err != nil {
		t.Fatal(err)
	}
	expected, failure := codec.Render(before, plan)
	if failure != nil {
		t.Fatal(failure)
	}
	mutation, err := NewManagedAggregateMutation(AggregateMutationCreate, subject, contribution.Contract(), before, snapshot, expected, 0)
	if err != nil {
		t.Fatal(err)
	}
	values, err := NormalizeCaptureMutations(nil, []ManagedAggregateMutation{mutation}, nil)
	if err != nil || len(values) != 1 {
		t.Fatal("normalize aggregate capture fixture", err)
	}
	return values[0]
}
