package mcp

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/isty2e/daem/internal/assurance/durable"
	durableattempt "github.com/isty2e/daem/internal/assurance/durable/attempt"
	"github.com/isty2e/daem/internal/assurance/observe"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/aggregate/codec"
	lock "github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
)

func TestClassifyLockedProjectionsRejectsDuplicateLockedSubject(t *testing.T) {
	contract := claudeMCPRecord(t)
	_, err := ClassifyLockedProjections(LockedProjectionBatchInput{
		Contracts: []lock.LockedSubjectContract{contract, contract},
	})
	if err == nil || !strings.Contains(err.Error(), "duplicate locked MCP projection subject") {
		t.Fatalf("ClassifyLockedProjections error = %v, want duplicate subject rejection", err)
	}
}

func TestClassifyLockedProjectionsRejectsForeignSubjectBeforeEvidenceUse(t *testing.T) {
	_, err := ClassifyLockedProjections(LockedProjectionBatchInput{
		Contracts: []lock.LockedSubjectContract{{}},
	})
	if err == nil || !strings.Contains(err.Error(), "is not an MCP projection") {
		t.Fatalf("ClassifyLockedProjections error = %v, want foreign subject rejection", err)
	}
}

func TestClassifyLockedProjectionsRequiresFreshAggregateEvidence(t *testing.T) {
	contract := claudeMCPRecord(t)
	_, preconditions := freshMissingProjectionEvidence(t, contract)

	_, err := ClassifyLockedProjections(LockedProjectionBatchInput{
		Contracts:     []lock.LockedSubjectContract{contract},
		Preconditions: preconditions,
	})
	if err == nil || !strings.Contains(err.Error(), "fresh aggregate observation is required") {
		t.Fatalf("ClassifyLockedProjections error = %v, want fresh evidence rejection", err)
	}
}

func TestClassifyLockedProjectionsRejectsIncompletePreconditionEvidence(t *testing.T) {
	contract := openCodeMCPRecord(t)
	evidence, preconditions := freshMissingProjectionEvidence(t, contract)
	if len(preconditions) == 0 {
		t.Fatal("OpenCode fixture unexpectedly has no aggregate preconditions")
	}

	_, err := ClassifyLockedProjections(LockedProjectionBatchInput{
		Contracts: []lock.LockedSubjectContract{contract},
		Evidence:  []observe.AggregateEvidence{evidence},
	})
	if err == nil || !strings.Contains(err.Error(), "aggregate precondition evidence is incomplete") {
		t.Fatalf("ClassifyLockedProjections error = %v, want incomplete precondition rejection", err)
	}
}

func TestClassifyLockedProjectionsUsesFreshEvidenceInsteadOfAttemptHistory(t *testing.T) {
	contract := claudeMCPRecord(t)
	evidence, preconditions := freshMissingProjectionEvidence(t, contract)
	attempt := successfulAttemptForContract(t, contract)
	currentState, err := durable.NewSnapshot(durable.SnapshotInput{
		DelegateAttempts: []durableattempt.DelegateAttempt{attempt},
	})
	if err != nil {
		t.Fatalf("durable.NewSnapshot returned error: %v", err)
	}

	observations, err := ClassifyLockedProjections(LockedProjectionBatchInput{
		Contracts:     []lock.LockedSubjectContract{contract},
		CurrentState:  currentState,
		Evidence:      []observe.AggregateEvidence{evidence},
		Preconditions: preconditions,
	})
	if err != nil {
		t.Fatalf("ClassifyLockedProjections returned error: %v", err)
	}
	if len(observations) != 1 {
		t.Fatalf("observations = %#v, want one", observations)
	}
	if got := observations[0].Current().Projection.State; got != ProjectionMissing {
		t.Fatalf("current projection = %q, want %q despite successful history", got, ProjectionMissing)
	}
	if got := observations[0].LastDelegateAttempt().State; got != DelegateAttemptSucceeded {
		t.Fatalf("last attempt = %q, want %q", got, DelegateAttemptSucceeded)
	}
}

func TestFindDelegateAttemptRecordUsesCanonicalSubjectIngress(t *testing.T) {
	subject, err := topology.NewSubjectID(
		topology.SubjectProjection,
		"claude-code.project.mcp-server",
		"context7",
	)
	if err != nil {
		t.Fatalf("NewSubjectID returned error: %v", err)
	}
	record, err := durableattempt.NewDelegateAttempt(durableattempt.DelegateAttemptInput{
		Subject:         subject,
		Target:          target.TargetClaudeCode,
		Scope:           target.ScopeProject,
		PlanIdentityKey: "plan:context7",
		ObservedAt:      time.Unix(1, 0),
		Status:          durableattempt.DelegateStatusSucceeded,
		Reason:          durableattempt.DelegateReasonNone,
	})
	if err != nil {
		t.Fatalf("NewDelegateAttempt returned error: %v", err)
	}

	got, ok := findDelegateAttemptRecord(
		[]durableattempt.DelegateAttempt{record},
		subject,
		target.TargetClaudeCode,
		target.ScopeProject,
	)
	if !ok || got.Subject() != subject {
		t.Fatalf("findDelegateAttemptRecord = (%#v, %t), want canonical match", got, ok)
	}

	if _, err := topology.NewSubjectID(
		topology.SubjectProjection,
		"claude-code.project.mcp-server",
		" context7 ",
	); err == nil {
		t.Fatal("NewSubjectID returned nil error for malformed persisted subject")
	}
}

func freshMissingProjectionEvidence(
	t *testing.T,
	contract lock.LockedSubjectContract,
) (observe.AggregateEvidence, []observe.AggregatePreconditionEvidence) {
	t.Helper()
	realization, ok := contract.Realization()
	if !ok {
		t.Fatal("MCP fixture is missing realization")
	}
	contribution, ok := realization.ManagedAggregateContribution()
	if !ok {
		t.Fatal("MCP fixture is missing aggregate contribution")
	}
	projection := contribution.Contract()
	selection, err := aggregate.NewSelection([]aggregate.ProjectionContract{projection})
	if err != nil {
		t.Fatalf("aggregate.NewSelection returned error: %v", err)
	}
	codec, ok := aggregatecodec.Catalog().Lookup(projection.CodecContractID())
	if !ok {
		t.Fatalf("codec %q is missing", projection.CodecContractID())
	}
	document := aggregate.AbsentDocument()
	snapshot, failure := codec.Read(document, selection)
	if failure != nil {
		t.Fatalf("codec.Read returned failure: %v", failure)
	}
	evidence, err := observe.NewAggregateEvidence(document, snapshot, os.FileMode(0))
	if err != nil {
		t.Fatalf("observe.NewAggregateEvidence returned error: %v", err)
	}

	expected, admitted, err := aggregate.OperationPreconditionsForCodec(projection.CodecContractID())
	if err != nil {
		t.Fatalf("OperationPreconditionsForCodec returned error: %v", err)
	}
	if !admitted {
		t.Fatalf("codec %q has no precondition profile", projection.CodecContractID())
	}
	preconditions := make([]observe.AggregatePreconditionEvidence, 0, len(expected))
	for _, precondition := range expected {
		item, err := observe.NewAggregatePreconditionEvidence(
			projection.Address().Document(),
			precondition,
			true,
		)
		if err != nil {
			t.Fatalf("NewAggregatePreconditionEvidence returned error: %v", err)
		}
		preconditions = append(preconditions, item)
	}
	return evidence, preconditions
}

func successfulAttemptForContract(
	t *testing.T,
	contract lock.LockedSubjectContract,
) durableattempt.DelegateAttempt {
	t.Helper()
	realization, ok := contract.Realization()
	if !ok {
		t.Fatal("MCP fixture is missing realization")
	}
	contribution, ok := realization.ManagedAggregateContribution()
	if !ok {
		t.Fatal("MCP fixture is missing aggregate contribution")
	}
	identity, ok := contract.DelegatePlanIdentity()
	if !ok {
		t.Fatal("MCP fixture is missing delegate plan identity")
	}
	attempt, err := durableattempt.NewDelegateAttempt(durableattempt.DelegateAttemptInput{
		Subject:         contract.SubjectID(),
		Target:          contribution.Target(),
		Scope:           contribution.Scope(),
		PlanIdentityKey: identity.IdentityKey,
		ObservedAt:      time.Unix(1, 0),
		Status:          durableattempt.DelegateStatusSucceeded,
		Reason:          durableattempt.DelegateReasonNone,
	})
	if err != nil {
		t.Fatalf("attempt.NewDelegateAttempt returned error: %v", err)
	}
	return attempt
}
