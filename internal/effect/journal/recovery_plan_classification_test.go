package journal

import (
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/output/ownership"
	"github.com/isty2e/daem/internal/realization/aggregate"
	mcpcodec "github.com/isty2e/daem/internal/realization/aggregate/codec/mcp"
	topologyprojection "github.com/isty2e/daem/internal/topology/projection"
	"github.com/isty2e/daem/test/outputtest"
	mcptest "github.com/isty2e/daem/test/testkit/mcp"
)

func TestBuildRecoveryPlanClassifiesCleanStatesAsCleanupOnly(t *testing.T) {
	plan := mustBuildRecoveryPlan(t, defaultRecoveryJournal(), beforeStatefile(), []recoveryPathObservation{
		beforePathObservation(defaultRecoveryEntry()),
	}, nil)

	requireClassification(t, plan, recovery.ClassificationCleanBefore)
	requireHealthyPlan(t, plan)
	action := requireOnlyAction(t, plan, recovery.ActionKindCleanup, "clean_before")
	if action.Destination != testOperationID {
		t.Fatalf("cleanup destination = %q, want operation id %q", action.Destination, testOperationID)
	}
	guarded := plan.GuardedActions()
	if len(guarded) != 1 || guarded[0].Destination != defaultRecoveryEntry().Path {
		t.Fatalf("GuardedActions() = %#v, want the journal-described host path", guarded)
	}
	guarded[0].Destination = "changed"
	if got := plan.GuardedActions()[0].Destination; got != defaultRecoveryEntry().Path {
		t.Fatalf("GuardedActions() returned mutable plan storage: %q", got)
	}

	plan = mustBuildRecoveryPlan(t, defaultRecoveryJournal(), afterStatefile(), []recoveryPathObservation{
		afterPathObservation(defaultRecoveryEntry()),
	}, nil)

	requireClassification(t, plan, recovery.ClassificationCleanAfter)
	requireHealthyPlan(t, plan)
	action = requireOnlyAction(t, plan, recovery.ActionKindCleanup, "clean_after")
	if action.Destination != testOperationID {
		t.Fatalf("cleanup destination = %q, want operation id %q", action.Destination, testOperationID)
	}
	if guarded := plan.GuardedActions(); len(guarded) != 1 || guarded[0].Destination != defaultRecoveryEntry().Path {
		t.Fatalf("clean-after GuardedActions() = %#v, want journal-described host path", guarded)
	}
}

func TestBuildRecoveryPlanClassifiesRollbackWithRestoreActions(t *testing.T) {
	entry := defaultRecoveryEntry()
	plan := mustBuildRecoveryPlan(t, defaultRecoveryJournal(), beforeStatefile(), []recoveryPathObservation{
		afterPathObservation(entry),
	}, []recoveryBackupObservation{
		matchingBackupObservation(entry),
	})

	requireClassification(t, plan, recovery.ClassificationNeedsRollback)
	requireHealthyPlan(t, plan)
	action := requireOnlyAction(t, plan, recovery.ActionKindRestoreWrite, "restore_file")
	requireEntryFacts(t, action, entry)
	if action.BackupPath != entry.Before.BackupPath {
		t.Fatalf("BackupPath = %q, want %q", action.BackupPath, entry.Before.BackupPath)
	}
	if action.BackupHash != entry.Before.ContentHash {
		t.Fatalf("BackupHash = %q, want %q", action.BackupHash, entry.Before.ContentHash)
	}
	if action.BackupKind != entry.Before.Kind {
		t.Fatalf("BackupKind = %q, want %q", action.BackupKind, entry.Before.Kind)
	}
}

func TestBuildRecoveryPlanKeepsEntryOrderForMixedRollbackActions(t *testing.T) {
	first := defaultRecoveryEntry()
	second := recoveryEntryFor("second", "CLAUDE.md", "sha256:second-before", "sha256:second-after", "backups/CLAUDE.md")
	journal := recoveryJournalFor(first, second)

	plan := mustBuildRecoveryPlan(t, journal, journal.StatefileBefore, []recoveryPathObservation{
		beforePathObservation(first),
		afterPathObservation(second),
	}, []recoveryBackupObservation{
		matchingBackupObservation(second),
	})

	requireClassification(t, plan, recovery.ClassificationNeedsRollback)
	actions := plan.Actions()
	if len(actions) != 2 {
		t.Fatalf("actions = %#v, want noop then restore action", actions)
	}
	if actions[0].Kind != recovery.ActionKindNoOp || actions[0].Reason != "already_before" {
		t.Fatalf("first action = %#v, want already_before noop", actions[0])
	}
	if actions[1].Kind != recovery.ActionKindRestoreWrite || actions[1].Reason != "restore_file" {
		t.Fatalf("second action = %#v, want restore_file action", actions[1])
	}
	requireEntryFacts(t, actions[1], second)
}

func TestBuildRecoveryPlanRestoresAbsentBeforeStateWithoutBackup(t *testing.T) {
	entry := defaultRecoveryEntry()
	entry.Before = persistedBeforePathState(recovery.BeforePathState{Existed: false})
	journal := recoveryJournalFor(entry)
	journal.StatefileBefore = durable.EmptySnapshot()
	journal.StatefileAfter = statefileFor(resourceState(entry, testAfterHash))

	plan := mustBuildRecoveryPlan(t, journal, journal.StatefileBefore, []recoveryPathObservation{
		afterPathObservation(entry),
	}, nil)

	requireClassification(t, plan, recovery.ClassificationNeedsRollback)
	requireHealthyPlan(t, plan)
	action := requireOnlyAction(t, plan, recovery.ActionKindRestoreDelete, "restore_absent")
	requireEntryFacts(t, action, entry)
}

func TestBuildRecoveryPlanBlocksWhenPathMatchesNeitherSide(t *testing.T) {
	entry := defaultRecoveryEntry()
	plan := mustBuildRecoveryPlan(t, defaultRecoveryJournal(), beforeStatefile(), []recoveryPathObservation{
		dirtyPathObservation(entry),
	}, nil)

	requireClassification(t, plan, recovery.ClassificationBlocked)
	requireErrorAction(t, plan, "blocked", "path differs from both before and expected-after states")
}

func TestBuildRecoveryPlanTreatsPermissionDriftAsNeitherGuardedSide(t *testing.T) {
	entry := defaultRecoveryEntry()
	observation := afterPathObservation(entry)
	observation.PathMode = testRecoveryPermissionMode(0o700)

	plan := mustBuildRecoveryPlan(t, defaultRecoveryJournal(), beforeStatefile(), []recoveryPathObservation{observation}, nil)

	requireClassification(t, plan, recovery.ClassificationBlocked)
	requireErrorAction(t, plan, "blocked", "path differs from both before and expected-after states")
}

func TestBuildRecoveryPlanTreatsProjectionContainerModeDriftAsNeitherGuardedSide(t *testing.T) {
	contract := recoveryTestAggregateContract(t)
	address := contract.Address()
	document := address.Document()
	id, err := entity.New(entity.KindHook, "format")
	if err != nil {
		t.Fatalf("construct Hook entity: %v", err)
	}
	subject, err := topologyprojection.Subject(id, address.PlacementID())
	if err != nil {
		t.Fatalf("construct Hook projection subject: %v", err)
	}
	baseline := defaultRecoveryEntry()
	entry := recoveryEntry{
		Subject: persistedSubjectRef{
			Kind: string(subject.Kind()), Namespace: subject.Namespace(), Name: subject.Key(),
		},
		Target:           string(document.Target()),
		Scope:            string(document.Scope()),
		Path:             document.AggregateRoot().String(),
		ContentPath:      string(address.ContentPath()),
		Aggregate:        persistedAggregateContract(contract),
		StateIndependent: true,
		Before:           baseline.Before,
		ExpectedAfter:    baseline.ExpectedAfter,
	}
	entry.Before.PathExisted = true
	entry.ExpectedAfter.PathExisted = true
	journal := recoveryJournalFor(entry)
	observation := afterPathObservation(entry)
	observation.PathMode = testRecoveryPermissionMode(0o640)

	plan := mustBuildRecoveryPlan(t, journal, journal.StatefileBefore, []recoveryPathObservation{observation}, nil)

	requireClassification(t, plan, recovery.ClassificationBlocked)
	requireErrorAction(t, plan, "blocked", "path differs from both before and expected-after states")
}

func TestBuildRecoveryPlanReportsPathObservationErrors(t *testing.T) {
	entry := defaultRecoveryEntry()

	plan := mustBuildRecoveryPlan(t, defaultRecoveryJournal(), beforeStatefile(), nil, nil)
	requireClassification(t, plan, recovery.ClassificationBlocked)
	requireErrorAction(t, plan, "observation_error", "path observation is required")

	plan = mustBuildRecoveryPlan(t, defaultRecoveryJournal(), beforeStatefile(), []recoveryPathObservation{
		{Path: entry.Path, Error: "boom"},
	}, nil)
	requireClassification(t, plan, recovery.ClassificationBlocked)
	requireErrorAction(t, plan, "observation_error", "boom")
}

func TestBuildRecoveryPlanReportsBackupMismatches(t *testing.T) {
	entry := defaultRecoveryEntry()
	cases := []struct {
		name        string
		observation []recoveryBackupObservation
		wantDetail  string
	}{
		{
			name:       "missing observation",
			wantDetail: "backup observation is required",
		},
		{
			name: "observation error",
			observation: []recoveryBackupObservation{
				{BackupPath: entry.Before.BackupPath, Error: "backup boom"},
			},
			wantDetail: "backup boom",
		},
		{
			name: "missing file",
			observation: []recoveryBackupObservation{
				{BackupPath: entry.Before.BackupPath, Exists: false},
			},
			wantDetail: "backup file is missing",
		},
		{
			name: "kind mismatch",
			observation: []recoveryBackupObservation{
				{
					BackupPath:  entry.Before.BackupPath,
					Exists:      true,
					Kind:        recovery.PathKindDirectory,
					ContentHash: entry.Before.ContentHash,
				},
			},
			wantDetail: `backup kind "directory" does not match before kind "file"`,
		},
		{
			name: "hash mismatch",
			observation: []recoveryBackupObservation{
				{
					BackupPath:  entry.Before.BackupPath,
					Exists:      true,
					Kind:        entry.Before.Kind,
					ContentHash: testDirtyHash,
				},
			},
			wantDetail: `backup hash "` + testDirtyHash + `" does not match before hash "` + testBeforeHash + `"`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			plan := mustBuildRecoveryPlan(t, defaultRecoveryJournal(), beforeStatefile(), []recoveryPathObservation{
				afterPathObservation(entry),
			}, tc.observation)

			requireClassification(t, plan, recovery.ClassificationBlocked)
			requireErrorAction(t, plan, "backup_mismatch", tc.wantDetail)
		})
	}
}

func TestExtractRecoveryObservationProjectionRequiresMatchingContractAddress(t *testing.T) {
	canonical, err := mcpcodec.CanonicalClaudeProjectMCPServerEntry(mcpcodec.ClaudeProjectMCPServerProjection{
		ServerID:        "context7",
		Command:         "npx",
		Args:            []string{"-y", "@upstash/context7-mcp"},
		Env:             map[string]string{},
		AdapterContract: aggregate.ClaudeProjectMCPStdioAdapterV1,
	})
	if err != nil {
		t.Fatalf("CanonicalClaudeProjectMCPServerEntry returned error: %v", err)
	}
	operations, ok := mcptest.OperationsForPlacementID(aggregate.MCPPlacementClaudeProject)
	if !ok {
		t.Fatal("Claude project MCP placement operations missing")
	}
	content, err := operations.MergeCanonicalEntry(nil, "context7", canonical)
	if err != nil {
		t.Fatalf("MergeCanonicalEntry returned error: %v", err)
	}
	contract, err := operations.Placement().ProjectionContract("context7")
	if err != nil {
		t.Fatalf("ProjectionContract returned error: %v", err)
	}

	_, _, err = extractRecoveryObservationProjection(
		content,
		outputtest.Parse(t, "unknown-mcp-config.json"),
		output.ContentPath(mcpcodec.ClaudeProjectMCPContentPath("context7")),
		&contract,
		journalTestCodecs(),
	)
	if err == nil || !strings.Contains(err.Error(), "does not match observation") {
		t.Fatalf("error = %v, want mismatched contract address", err)
	}
}

func TestBuildRecoveryPlanBlocksUnsupportedBeforeState(t *testing.T) {
	entry := defaultRecoveryEntry()
	entry.Before = persistedBeforePathState(recovery.BeforePathState{
		Existed:    true,
		Kind:       recovery.PathKindSymlink,
		LinkTarget: "old-target",
	})
	entry.ExpectedAfter = persistedExpectedPathState(recovery.ExpectedPathState{
		Existed:    true,
		Kind:       recovery.PathKindSymlink,
		LinkTarget: "new-target",
	})
	journal := defaultRecoveryJournal()
	journal.Entries = []recoveryEntry{entry}

	plan := mustBuildRecoveryPlan(t, journal, journal.StatefileBefore, []recoveryPathObservation{
		{
			Path:       entry.Path,
			Exists:     true,
			Kind:       recovery.PathKindSymlink,
			LinkTarget: "new-target",
		},
	}, nil)

	requireClassification(t, plan, recovery.ClassificationBlocked)
	requireErrorAction(t, plan, "unsupported_before_state", `before path kind "symlink" is not supported`)
}

func TestBuildRecoveryPlanReportsStateMismatchesSeparatelyFromHostPaths(t *testing.T) {
	entry := defaultRecoveryEntry()
	dirtyState := statefileFor(resourceState(entry, testDirtyHash))

	plan := mustBuildRecoveryPlan(t, defaultRecoveryJournal(), dirtyState, []recoveryPathObservation{
		afterPathObservation(entry),
	}, []recoveryBackupObservation{
		matchingBackupObservation(entry),
	})
	requireClassification(t, plan, recovery.ClassificationBlocked)
	requireErrorAction(t, plan, "state_mismatch", "statefile differs from both before and expected-after states")

	plan = mustBuildRecoveryPlan(t, defaultRecoveryJournal(), afterStatefile(), []recoveryPathObservation{
		beforePathObservation(entry),
	}, nil)
	requireClassification(t, plan, recovery.ClassificationBlocked)
	requireErrorAction(t, plan, "state_mismatch", "statefile is after apply but host paths are not clean_after")
	requireAction(t, plan, recovery.ActionKindNoOp, "already_before")
}

func TestBuildRecoveryPlanRejectsDuplicateObservationIndexesBeforeClassification(t *testing.T) {
	entry := defaultRecoveryEntry()

	_, err := buildRecoveryPlan(testOperationID, testOperationDir, defaultRecoveryJournal(), beforeStatefile(), []recoveryPathObservation{
		beforePathObservation(entry),
		beforePathObservation(entry),
	}, nil, nil, ownership.EmptyRegistry(), testStateCodec())

	if err == nil || !strings.Contains(err.Error(), `duplicate path observation for "AGENTS.md"`) {
		t.Fatalf("path duplicate err = %v, want duplicate path observation diagnostic", err)
	}

	_, err = buildRecoveryPlan(testOperationID, testOperationDir, defaultRecoveryJournal(), beforeStatefile(), []recoveryPathObservation{
		afterPathObservation(entry),
	}, []recoveryBackupObservation{
		matchingBackupObservation(entry),
		matchingBackupObservation(entry),
	}, nil, ownership.EmptyRegistry(), testStateCodec())

	if err == nil || !strings.Contains(err.Error(), `duplicate backup observation for "backups/AGENTS.md"`) {
		t.Fatalf("backup duplicate err = %v, want duplicate backup observation diagnostic", err)
	}
}

func TestBuildRecoveryPlanNormalizesStatefileResourceOrder(t *testing.T) {
	first := defaultRecoveryEntry()
	second := recoveryEntryFor("second", "CLAUDE.md", "sha256:second-before", "sha256:second-after", "backups/CLAUDE.md")
	journal := recoveryJournalFor(first, second)
	current := statefileFor(resourceState(second, "sha256:second-before"), resourceState(first, testBeforeHash))

	plan := mustBuildRecoveryPlan(t, journal, current, []recoveryPathObservation{
		beforePathObservation(first),
		beforePathObservation(second),
	}, nil)

	requireClassification(t, plan, recovery.ClassificationCleanBefore)
	requireOnlyAction(t, plan, recovery.ActionKindCleanup, "clean_before")
}
