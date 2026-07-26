package recovery

import (
	"strings"
	"testing"
	"time"

	"github.com/isty2e/daem/internal/assurance/durable"
	durableattempt "github.com/isty2e/daem/internal/assurance/durable/attempt"
	ownershipmutation "github.com/isty2e/daem/internal/effect/mutation/ownership"
	"github.com/isty2e/daem/internal/output/ownership"
	"github.com/isty2e/daem/internal/realization"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
)

func TestClassifyRecoveryPrecedence(t *testing.T) {
	authority := testAuthority(t, "fingerprint:one")
	selection, err := NewSelection(authority, nil)
	if err != nil {
		t.Fatal(err)
	}
	beforeEvidence := []PathEvidence{{Path: "AGENTS.md"}}
	afterEvidence := []PathEvidence{{
		Path: "AGENTS.md", Exists: true, Kind: PathKindFile,
		ContentHash: "sha256:after", PathMode: testPermissionMode(0o600),
	}}
	dirtyEvidence := []PathEvidence{{
		Path: "AGENTS.md", Exists: true, Kind: PathKindFile,
		ContentHash: "sha256:dirty", PathMode: testPermissionMode(0o600),
	}}

	tests := []struct {
		name           string
		state          durable.Snapshot
		evidence       []PathEvidence
		classification Classification
		actionKind     ActionKind
	}{
		{name: "clean before", state: authority.statefileBefore, evidence: beforeEvidence, classification: ClassificationCleanBefore, actionKind: ActionKindCleanup},
		{name: "clean after", state: authority.statefileAfter, evidence: afterEvidence, classification: ClassificationCleanAfter, actionKind: ActionKindCleanup},
		{name: "rollback", state: authority.statefileBefore, evidence: afterEvidence, classification: ClassificationNeedsRollback, actionKind: ActionKindRestoreDelete},
		{name: "blocked", state: authority.statefileBefore, evidence: dirtyEvidence, classification: ClassificationBlocked, actionKind: ActionKindError},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			plan, err := Classify(
				authority,
				selection,
				test.state,
				test.evidence,
				nil,
				ownership.EmptyRegistry(),
			)
			if err != nil {
				t.Fatal(err)
			}
			if plan.Classification() != test.classification {
				t.Fatalf("classification = %q, want %q", plan.Classification(), test.classification)
			}
			actions := plan.Actions()
			if len(actions) != 1 || actions[0].Kind != test.actionKind {
				t.Fatalf("actions = %#v, want one %q", actions, test.actionKind)
			}
		})
	}
}

func TestClassifyRejectsMalformedEvidenceBeforeDecision(t *testing.T) {
	authority := testAuthority(t, "fingerprint:one")
	selection, err := NewSelection(authority, nil)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name     string
		paths    []PathEvidence
		backups  []BackupEvidence
		contains string
	}{
		{name: "empty path", paths: []PathEvidence{{}}, contains: "path observation path is required"},
		{name: "duplicate path", paths: []PathEvidence{{Path: "AGENTS.md"}, {Path: "AGENTS.md"}}, contains: "duplicate path observation"},
		{name: "empty backup", paths: []PathEvidence{{Path: "AGENTS.md"}}, backups: []BackupEvidence{{}}, contains: "backup observation path is required"},
		{name: "duplicate backup", paths: []PathEvidence{{Path: "AGENTS.md"}}, backups: []BackupEvidence{{BackupPath: "backups/a"}, {BackupPath: "backups/a"}}, contains: "duplicate backup observation"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := Classify(
				authority,
				selection,
				authority.statefileBefore,
				test.paths,
				test.backups,
				ownership.EmptyRegistry(),
			)
			if err == nil || !strings.Contains(err.Error(), test.contains) {
				t.Fatalf("error = %v, want %q", err, test.contains)
			}
		})
	}
}

func TestSelectionRejectsZeroAndCrossAuthorityUse(t *testing.T) {
	first := testAuthority(t, "fingerprint:one")
	second := testAuthority(t, "fingerprint:two")
	selection, err := NewSelection(first, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name      string
		authority Authority
		selection Selection
		contains  string
	}{
		{name: "zero", authority: first, selection: Selection{}, contains: "uninitialized"},
		{name: "cross authority", authority: second, selection: selection, contains: "different authority"},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := Classify(
				test.authority,
				test.selection,
				test.authority.statefileBefore,
				nil,
				nil,
				ownership.EmptyRegistry(),
			)
			if err == nil || !strings.Contains(err.Error(), test.contains) {
				t.Fatalf("error = %v, want %q", err, test.contains)
			}
		})
	}
}

func TestNewEntryRejectsMalformedCanonicalFacts(t *testing.T) {
	subject := testSubject(t)
	before := BeforePathState{Existed: false}
	expected := ExpectedPathState{
		Existed: true, Kind: PathKindFile, ContentHash: "sha256:after",
		PathMode: testPermissionMode(0o600),
	}
	aggregateSubject, aggregateContract := testAggregateEntryContract(t)

	tests := []struct {
		name string
		run  func() error
	}{
		{name: "unknown target", run: func() error {
			_, err := NewEntry(subject, target.Target("unknown"), nil, target.ScopeProject, "AGENTS.md", "", "", before, expected, nil)
			return err
		}},
		{name: "unknown scope", run: func() error {
			_, err := NewEntry(subject, target.TargetCodex, nil, target.Scope("unknown"), "AGENTS.md", "", "", before, expected, nil)
			return err
		}},
		{name: "noncanonical destination", run: func() error {
			_, err := NewEntry(subject, target.TargetCodex, nil, target.ScopeProject, "../AGENTS.md", "", "", before, expected, nil)
			return err
		}},
		{name: "unsupported content kind", run: func() error {
			_, err := NewEntry(subject, "", []target.Target{target.TargetCodex}, target.ScopeProject, "AGENTS.md", "", realization.PathProjectionContentKind("symlink"), before, expected, nil)
			return err
		}},
		{name: "duplicate path consumers", run: func() error {
			_, err := NewEntry(subject, "", []target.Target{target.TargetCodex, target.TargetCodex}, target.ScopeProject, "AGENTS.md", "", realization.PathProjectionFile, before, expected, nil)
			return err
		}},
		{name: "path entry carries aggregate facts", run: func() error {
			_, err := NewEntry(subject, "", []target.Target{target.TargetCodex}, target.ScopeProject, "AGENTS.md", "/hooks", realization.PathProjectionFile, before, expected, &aggregateContract)
			return err
		}},
		{name: "content path lacks contract", run: func() error {
			_, err := NewEntry(subject, target.TargetCodex, nil, target.ScopeProject, "AGENTS.md", "/hooks", "", before, expected, nil)
			return err
		}},
		{name: "contract lacks content path", run: func() error {
			_, err := NewEntry(aggregateSubject, target.TargetCodex, nil, target.ScopeProject, ".codex/hooks.json", "", "", before, expected, &aggregateContract)
			return err
		}},
		{name: "aggregate address mismatch", run: func() error {
			_, err := NewEntry(aggregateSubject, target.TargetCodex, nil, target.ScopeProject, "AGENTS.md", "/hooks", "", before, expected, &aggregateContract)
			return err
		}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := test.run(); err == nil {
				t.Fatal("NewEntry accepted malformed canonical facts")
			}
		})
	}
}

func TestNewAuthorityRejectsMalformedOwnedValues(t *testing.T) {
	entry, err := NewEntry(
		testSubject(t),
		target.TargetCodex,
		nil,
		target.ScopeProject,
		"AGENTS.md",
		"",
		"",
		BeforePathState{Existed: false},
		ExpectedPathState{Existed: false},
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name        string
		entries     []Entry
		transitions []ownershipmutation.ClaimTransition
		provenance  *ProjectRootProvenance
	}{
		{name: "zero entry", entries: []Entry{{}}},
		{name: "zero claim transition", entries: []Entry{entry}, transitions: []ownershipmutation.ClaimTransition{{}}},
		{name: "zero project provenance", entries: []Entry{entry}, provenance: &ProjectRootProvenance{}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := NewAuthority(
				"operation",
				"recovery/operation",
				test.entries,
				durable.EmptySnapshot(),
				durable.EmptySnapshot(),
				test.transitions,
				test.provenance,
				"fingerprint",
			); err == nil {
				t.Fatal("NewAuthority accepted malformed owned value")
			}
		})
	}
}

func TestPlanDisclosureDoesNotAliasNestedActionState(t *testing.T) {
	beforeMode := PermissionMode(0o600)
	expectedMode := PermissionMode(0o640)
	contract := aggregate.ProjectionContract{}
	action := Action{
		ConsumerTargets:   []target.Target{target.TargetCodex},
		BeforePathMode:    &beforeMode,
		ExpectedAfter:     ExpectedPathState{PathMode: &expectedMode},
		AggregateContract: &contract,
	}
	plan := Plan{actions: []Action{action}, guardedActions: []Action{action}}

	assertIndependent := func(disclosed Action, canonical Action) {
		t.Helper()
		if disclosed.AggregateContract == canonical.AggregateContract {
			t.Fatal("aggregate contract pointer aliases canonical action")
		}
		disclosed.ConsumerTargets[0] = target.TargetClaudeCode
		*disclosed.BeforePathMode = PermissionMode(0o700)
		*disclosed.ExpectedAfter.PathMode = PermissionMode(0o777)
		if canonical.ConsumerTargets[0] != target.TargetCodex ||
			*canonical.BeforePathMode != PermissionMode(0o600) ||
			*canonical.ExpectedAfter.PathMode != PermissionMode(0o640) {
			t.Fatalf("disclosure mutation changed canonical action: %#v", canonical)
		}
	}
	assertIndependent(plan.Actions()[0], plan.actions[0])
	assertIndependent(plan.GuardedActions()[0], plan.guardedActions[0])
	clone := plan.Clone()
	assertIndependent(clone.actions[0], plan.actions[0])
	assertIndependent(clone.guardedActions[0], plan.guardedActions[0])
}

func TestActionExecutionAuthorityComparesAllCanonicalIdentity(t *testing.T) {
	subject := testSubject(t)
	base := Action{
		Kind:          ActionKindRestoreWrite,
		subject:       subject,
		Destination:   "AGENTS.md",
		ExpectedAfter: ExpectedPathState{Kind: PathKindFile, ContentHash: "sha256:after"},
	}
	equivalent := base
	equivalent.ConsumerTargets = []target.Target{}
	if !base.sameExecutionAuthority(equivalent) {
		t.Fatal("nil and empty consumer representations changed authority")
	}

	differentConsumer := base
	differentConsumer.ConsumerTargets = []target.Target{target.TargetCodex}
	if base.sameExecutionAuthority(differentConsumer) {
		t.Fatal("different consumers retained authority")
	}
	differentSubject := base
	differentSubject.subject, _ = topology.NewSubjectID(topology.SubjectProjection, "test.project", "other")
	if base.sameExecutionAuthority(differentSubject) {
		t.Fatal("different subject retained authority")
	}
	differentExpected := base
	differentExpected.ExpectedAfter.Kind = PathKindDirectory
	if base.sameExecutionAuthority(differentExpected) {
		t.Fatal("different expected state retained authority")
	}
}

func testAuthority(t *testing.T, fingerprint string) Authority {
	t.Helper()
	entry, err := NewEntry(
		testSubject(t),
		target.TargetCodex,
		nil,
		target.ScopeProject,
		"AGENTS.md",
		"",
		"",
		BeforePathState{Existed: false},
		ExpectedPathState{
			Existed: true, Kind: PathKindFile, ContentHash: "sha256:after",
			PathMode: testPermissionMode(0o600),
		},
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	after, err := testAfterSnapshot(t)
	if err != nil {
		t.Fatal(err)
	}
	authority, err := NewAuthority(
		"operation",
		"recovery/operation",
		[]Entry{entry},
		durable.EmptySnapshot(),
		after,
		nil,
		nil,
		fingerprint,
	)
	if err != nil {
		t.Fatal(err)
	}
	return authority
}

func testAfterSnapshot(t *testing.T) (durable.Snapshot, error) {
	t.Helper()
	attempt, err := durableattempt.NewDelegateAttempt(durableattempt.DelegateAttemptInput{
		Subject:         testSubject(t),
		Target:          target.TargetCodex,
		Scope:           target.ScopeProject,
		PlanIdentityKey: "delegate:test",
		ObservedAt:      time.Date(2026, time.July, 21, 0, 0, 0, 0, time.UTC),
		Status:          durableattempt.DelegateStatusSucceeded,
		Reason:          durableattempt.DelegateReasonNone,
	})
	if err != nil {
		return durable.Snapshot{}, err
	}
	return durable.NewSnapshot(durable.SnapshotInput{DelegateAttempts: []durableattempt.DelegateAttempt{attempt}})
}

func testSubject(t *testing.T) topology.SubjectID {
	t.Helper()
	subject, err := topology.NewSubjectID(topology.SubjectProjection, "test.project", "agents")
	if err != nil {
		t.Fatal(err)
	}
	return subject
}

func testAggregateEntryContract(t *testing.T) (topology.SubjectID, aggregate.ProjectionContract) {
	t.Helper()
	placement, present := aggregate.HookPlacementFor(target.TargetCodex, target.ScopeProject)
	if !present {
		t.Fatal("Codex project Hook placement is missing")
	}
	contribution, err := placement.Contribution("canonical")
	if err != nil {
		t.Fatal(err)
	}
	subject, err := topology.NewSubjectID(topology.SubjectProjection, string(placement.ID()), "hook:format")
	if err != nil {
		t.Fatal(err)
	}
	return subject, contribution.Contract()
}

func testPermissionMode(mode uint32) *PermissionMode {
	value := PermissionMode(mode)
	return &value
}
