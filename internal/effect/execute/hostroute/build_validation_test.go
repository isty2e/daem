package hostroute

import (
	"slices"
	"strings"
	"testing"

	observeclaudeplugin "github.com/isty2e/daem/internal/assurance/observe/claudeplugin"
	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	lock "github.com/isty2e/daem/internal/realization/lock"
	reconciliation "github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/target"
)

func TestBuildCommandBuildsUnsupportedObserverAttempt(t *testing.T) {
	fixture := newCodexHostRouteFixture(t, codexHostRouteFixture{
		sourceRef:  "documents@openai-primary-runtime",
		subjectKey: "documents@openai-primary-runtime",
		scope:      target.ScopeGlobal,
		workDir:    t.TempDir(),
	})
	fixture.action = genericRelationActionFor(
		t,
		fixture.record,
		fixture.subject,
		observerelation.InventorySpec{
			Availability: observerelation.InventoryUnsupported,
			Freshness:    observerelation.EvidenceFresh,
		},
		attemptWhenUnsupportedAdmission(t),
	)
	if fixture.action.Kind() != reconciliation.ActionAttempt {
		t.Fatalf("action kind = %q, want attempt", fixture.action.Kind())
	}

	command, err := BuildCommand(BuildInput{
		Action:   fixture.action,
		Lockfile: fixture.lockfile,
		WorkDir:  fixture.workDir,
	})
	if err != nil {
		t.Fatalf("BuildCommand returned error: %v", err)
	}
	if command.AttemptRequest().Command != "codex" ||
		!slices.Equal(command.AttemptRequest().Args, []string{"plugin", "add", "documents@openai-primary-runtime", "--json"}) {
		t.Fatalf("attempt request = %#v", command.AttemptRequest())
	}
}

func TestBuildCommandRejectsBlockedSameNameConflictBeforeCommandConstruction(t *testing.T) {
	fixture := newHostRouteFixture(t, hostRouteFixture{
		sourceKind: desiredextension.SourceKindMarketplace,
		sourceRef:  "context7@market",
		subjectKey: "context7@market",
		scope:      target.ScopeProject,
		workDir:    t.TempDir(),
		inventorySpec: observeclaudeplugin.InventorySpec{
			Availability: observerelation.InventorySupported,
			Freshness:    observerelation.EvidenceFresh,
			Rows: []observeclaudeplugin.Row{
				mustClaudeRow(t, "context7@market", "", false),
			},
		},
	})

	_, err := BuildCommand(BuildInput{
		Action:   fixture.action,
		Lockfile: fixture.lockfile,
		WorkDir:  fixture.workDir,
	})
	validation := assertValidationCode(t, err, ReasonUnsupportedAction)
	if !strings.Contains(validation.Error(), string(reconciliation.ReasonUnkeyedSameSubject)) {
		t.Fatalf("error = %q, want planner reason %q", validation.Error(), reconciliation.ReasonUnkeyedSameSubject)
	}
}

func TestBuildCommandRejectsNoOpBeforeCommandConstruction(t *testing.T) {
	fixture := newHostRouteFixture(t, hostRouteFixture{
		sourceKind: desiredextension.SourceKindMarketplace,
		sourceRef:  "context7@market",
		subjectKey: "context7@market",
		scope:      target.ScopeProject,
		workDir:    t.TempDir(),
	})
	fixture.action = relationActionFor(t, fixture.record, fixture.subject, observeclaudeplugin.InventorySpec{
		Availability: observerelation.InventorySupported,
		Freshness:    observerelation.EvidenceFresh,
		Rows: []observeclaudeplugin.Row{
			mustClaudeRow(t, "context7@market", string(fixture.subject.ExpectedRelation().ManagedInstanceKey()), true),
		},
	}, hostDelegatedAdmission(t))

	_, err := BuildCommand(BuildInput{
		Action:   fixture.action,
		Lockfile: fixture.lockfile,
		WorkDir:  fixture.workDir,
	})
	assertValidationCode(t, err, ReasonUnsupportedAction)
}

func TestBuildCommandRejectsMissingWorkDirAndLockedRecord(t *testing.T) {
	fixture := newHostRouteFixture(t, hostRouteFixture{
		sourceKind: desiredextension.SourceKindMarketplace,
		sourceRef:  "context7@market",
		subjectKey: "context7@market",
		scope:      target.ScopeProject,
		workDir:    t.TempDir(),
	})

	_, err := BuildCommand(BuildInput{
		Action:   fixture.action,
		Lockfile: fixture.lockfile,
	})
	assertValidationCode(t, err, ReasonMissingWorkDir)

	_, err = BuildCommand(BuildInput{
		Action:   fixture.action,
		Lockfile: lock.File{Version: lock.CurrentVersion},
		WorkDir:  fixture.workDir,
	})
	assertValidationCode(t, err, ReasonLockedSubjectMissing)
}

func TestBuildCommandRejectsWhitespaceOnlyWorkDir(t *testing.T) {
	fixture := newHostRouteFixture(t, hostRouteFixture{
		sourceKind: desiredextension.SourceKindMarketplace,
		sourceRef:  "context7@market",
		subjectKey: "context7@market",
		scope:      target.ScopeProject,
		workDir:    t.TempDir(),
	})

	_, err := BuildCommand(BuildInput{
		Action:   fixture.action,
		Lockfile: fixture.lockfile,
		WorkDir:  " \t\n",
	})
	assertValidationCode(t, err, ReasonMissingWorkDir)
}

func TestBuildCommandRejectsMismatchedLockedRouteRequest(t *testing.T) {
	actionFixture := newHostRouteFixture(t, hostRouteFixture{
		sourceKind: desiredextension.SourceKindMarketplace,
		sourceRef:  "context7@market",
		subjectKey: "context7@market",
		scope:      target.ScopeProject,
		workDir:    t.TempDir(),
	})
	otherRecord, _ := mustClaudePluginFixture(t, subjectSpec{
		sourceKind: desiredextension.SourceKindMarketplace,
		sourceRef:  "context7@other-market",
		subjectKey: "context7@market",
		scope:      target.ScopeProject,
	})

	_, err := BuildCommand(BuildInput{
		Action:   actionFixture.action,
		Lockfile: testLockedFile(t, otherRecord),
		WorkDir:  actionFixture.workDir,
	})
	assertValidationCode(t, err, ReasonRouteRequestMismatch)
}

func TestBuildCommandFindsSelectedRecordAfterUnrelatedRecord(t *testing.T) {
	selected := newHostRouteFixture(t, hostRouteFixture{
		sourceKind: desiredextension.SourceKindMarketplace,
		sourceRef:  "context7@market",
		subjectKey: "context7@market",
		scope:      target.ScopeProject,
		workDir:    t.TempDir(),
	})
	unrelated, _ := mustClaudePluginFixture(t, subjectSpec{
		sourceKind:    desiredextension.SourceKindMarketplace,
		sourceRef:     "other@market",
		subjectKey:    "other@market",
		scope:         target.ScopeProject,
		declarationID: "other",
	})

	command, err := BuildCommand(BuildInput{
		Action:   selected.action,
		Lockfile: testLockedFile(t, unrelated, selected.record),
		WorkDir:  selected.workDir,
	})
	if err != nil {
		t.Fatalf("BuildCommand returned error: %v", err)
	}
	if command.Subject() != selected.record.SubjectID() {
		t.Fatalf("subject = %#v, want %#v", command.Subject(), selected.record.SubjectID())
	}
}
