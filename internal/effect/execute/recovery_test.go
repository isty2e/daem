package execute

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/isty2e/daem/internal/assurance/durable"
	durableattempt "github.com/isty2e/daem/internal/assurance/durable/attempt"
	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	"github.com/isty2e/daem/internal/assurance/observe"
	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	"github.com/isty2e/daem/internal/assurance/statefile"
	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/effect/journal"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization"
	realizationdelegate "github.com/isty2e/daem/internal/realization/delegate"
	lock "github.com/isty2e/daem/internal/realization/lock"
	hostrelation "github.com/isty2e/daem/internal/realization/relation"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
	topologyprojection "github.com/isty2e/daem/internal/topology/projection"
	"github.com/isty2e/daem/test/outputtest"
)

func TestCaptureLoadAndExecuteRollback(t *testing.T) {
	root := t.TempDir()
	paths := Paths{
		RecoveryDir:   filepath.Join(root, ".daem", "recovery"),
		StateDir:      filepath.Join(root, ".daem"),
		StatefilePath: filepath.Join(root, ".daem", "state.json"),
		ManifestRoot:  root,
	}
	journalPaths := paths.journalPaths()

	oldContent := []byte("old instructions\n")
	newContent := []byte("new instructions\n")
	oldHash := string(artifact.HashFileContentWithExecutable(oldContent, true))
	newHash := string(artifact.HashFileContentWithExecutable(newContent, true))
	writeRecoveryTestFile(t, filepath.Join(root, "AGENTS.md"), oldContent)
	if err := os.Chmod(filepath.Join(root, "AGENTS.md"), 0o700); err != nil {
		t.Fatalf("chmod old instructions: %v", err)
	}

	currentPath := recoveryInstructionPathState(t, "guard", artifact.ContentHash(oldHash))
	nextPath := recoveryInstructionPathState(t, "guard", artifact.ContentHash(newHash))
	pendingCarrier := recoveryTestPendingCarrierInstall(
		t,
		paths.StatefilePath,
		filepath.Join(root, "daem.toml"),
	)
	delegateAttempt := recoveryTestDelegateAttempt(currentPath.Subject())
	hostRouteAttempt := recoveryTestHostRouteAttempt()
	currentState, err := durable.NewSnapshot(durable.SnapshotInput{
		ManagedPaths:           []durable.ManagedPathState{currentPath},
		PendingCarrierInstalls: []durablecarrier.PendingCarrierInstall{pendingCarrier},
		DelegateAttempts:       []durableattempt.DelegateAttempt{delegateAttempt},
		HostRouteAttempts:      []durableattempt.HostRouteAttempt{hostRouteAttempt},
	})
	if err != nil {
		t.Fatal(err)
	}
	nextState, err := durable.NewSnapshot(durable.SnapshotInput{
		ManagedPaths:           []durable.ManagedPathState{nextPath},
		PendingCarrierInstalls: []durablecarrier.PendingCarrierInstall{pendingCarrier},
		DelegateAttempts:       []durableattempt.DelegateAttempt{delegateAttempt},
		HostRouteAttempts:      []durableattempt.HostRouteAttempt{hostRouteAttempt},
	})
	if err != nil {
		t.Fatal(err)
	}
	mutationRequest, err := journal.NewManagedPathReplaceMutation(
		currentPath.Subject(),
		currentPath.ConsumerTargets(),
		currentPath.Scope(),
		currentPath.Destination(),
		nextPath.ContentHash(),
		currentPath.ContentHash(),
		realization.PathProjectionFile,
		0o700,
		currentPath,
	)
	if err != nil {
		t.Fatal(err)
	}
	evidence, err := observe.NewManagedPathEvidence(
		currentPath.Subject(),
		currentPath.Destination(),
		true,
		currentPath.ContentHash(),
		0o700,
	)
	if err != nil {
		t.Fatal(err)
	}

	resolver := func(destination output.Destination) (string, error) {
		return filepath.Join(root, filepath.FromSlash(destination.RelativePath())), nil
	}
	result, err := journal.CaptureJournalWithOptions(
		context.Background(),
		journalPaths,
		journal.OperationID(time.Date(2026, 6, 21, 12, 0, 0, 0, time.UTC)),
		time.Date(2026, 6, 21, 12, 0, 0, 0, time.UTC),
		currentState,
		nextState,
		journal.CaptureOptions{
			Filesystem:           testFilesystem(),
			ManagedPathMutations: []journal.ManagedPathMutation{mutationRequest},
			ManagedPathEvidence:  []observe.ManagedPathEvidence{evidence},
			Resolver:             resolver,
			StateCodec:           testStateCodec(),
		},
	)
	if err != nil {
		t.Fatalf("journal.CaptureJournal returned error: %v", err)
	}
	if result.Directory == "" || result.JournalPath == "" {
		t.Fatalf("journal.CaptureJournal result = %#v, want directory and journal path", result)
	}

	writeRecoveryTestFile(t, filepath.Join(root, "AGENTS.md"), newContent)
	if err := os.Chmod(filepath.Join(root, "AGENTS.md"), 0o700); err != nil {
		t.Fatalf("chmod applied hook asset: %v", err)
	}
	writeRecoveryTestStatefile(t, paths.StatefilePath, currentState)
	plan, err := journal.LoadActivePlanWithOptions(
		context.Background(),
		journalPaths,
		testPlanLoadOptions(paths),
	)
	if err != nil {
		t.Fatalf("journal.LoadActivePlan returned error: %v", err)
	}
	if plan.Classification() != recovery.ClassificationNeedsRollback {
		t.Fatalf("Classification = %q, want %q", plan.Classification(), recovery.ClassificationNeedsRollback)
	}
	canceledContext, cancel := context.WithCancel(context.Background())
	cancel()
	if err := executeRecoveryPlanWithOptionsForTest(canceledContext, plan, paths, RecoveryOptions{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled ExecuteRecoveryPlan error = %v", err)
	}
	if _, err := os.Stat(result.Directory); err != nil {
		t.Fatalf("canceled recovery removed journal evidence: %v", err)
	}
	validationErr := errors.New("recovery authority changed")
	validationCalls := 0
	if err := executeRecoveryPlanWithOptionsForTest(context.Background(), plan, paths, RecoveryOptions{
		Resolver:    destinationResolver(paths),
		StateCodec:  testStateCodec(),
		StateReader: testStateReader(paths.StatefilePath),
		Filesystem:  testFilesystem(),
		ValidateBeforeEffects: func(context.Context, mutation.PhysicalAuthoritySet) error {
			validationCalls++
			return validationErr
		},
	}); !errors.Is(err, validationErr) {
		t.Fatalf("validated ExecuteRecoveryPlan error = %v, want validation error", err)
	}
	if validationCalls != 1 {
		t.Fatalf("validation calls = %d, want 1", validationCalls)
	}
	contentAfterValidation, err := os.ReadFile(filepath.Join(root, "AGENTS.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(contentAfterValidation) != string(newContent) {
		t.Fatalf("validation failure changed host content to %q", contentAfterValidation)
	}
	if _, err := os.Stat(result.Directory); err != nil {
		t.Fatalf("validation failure removed journal evidence: %v", err)
	}
	externalContent := []byte("external instructions\n")
	if err := executeRecoveryPlanWithOptionsForTest(context.Background(), plan, paths, RecoveryOptions{
		Resolver:    destinationResolver(paths),
		StateCodec:  testStateCodec(),
		StateReader: testStateReader(paths.StatefilePath),
		Filesystem:  testFilesystem(),
		ValidateBeforeEffects: func(context.Context, mutation.PhysicalAuthoritySet) error {
			writeRecoveryTestFile(t, filepath.Join(root, "AGENTS.md"), externalContent)
			if err := os.Chmod(filepath.Join(root, "AGENTS.md"), 0o700); err != nil {
				t.Fatalf("chmod external hook asset: %v", err)
			}
			return nil
		},
	}); err == nil || !strings.Contains(err.Error(), "blocked by current evidence") {
		t.Fatalf("recovery after validation-time drift error = %v, want blocked", err)
	}
	contentAfterDrift, err := os.ReadFile(filepath.Join(root, "AGENTS.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(contentAfterDrift) != string(externalContent) {
		t.Fatalf("validation-time external edit = %q, want preserved %q", contentAfterDrift, externalContent)
	}
	if _, err := os.Stat(result.Directory); err != nil {
		t.Fatalf("validation-time drift removed journal evidence: %v", err)
	}
	if err := executeRecoveryPlanWithOptionsForTest(context.Background(), plan, paths, RecoveryOptions{
		Resolver:    destinationResolver(paths),
		StateCodec:  testStateCodec(),
		StateReader: testStateReader(paths.StatefilePath),
		Filesystem:  testFilesystem(),
		ValidateBeforeEffects: func(context.Context, mutation.PhysicalAuthoritySet) error {
			writeRecoveryTestFile(t, filepath.Join(root, "AGENTS.md"), oldContent)
			if err := os.Chmod(filepath.Join(root, "AGENTS.md"), 0o700); err != nil {
				t.Fatalf("chmod already-before hook asset: %v", err)
			}
			return nil
		},
	}); err == nil || !strings.Contains(err.Error(), "execution authority changed") {
		t.Fatalf("recovery after classification change error = %v, want re-plan refusal", err)
	}
	contentAfterClassificationChange, err := os.ReadFile(filepath.Join(root, "AGENTS.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(contentAfterClassificationChange) != string(oldContent) {
		t.Fatalf("already-before content = %q, want preserved %q", contentAfterClassificationChange, oldContent)
	}
	if _, err := os.Stat(result.Directory); err != nil {
		t.Fatalf("classification change removed journal evidence: %v", err)
	}

	writeRecoveryTestFile(t, filepath.Join(root, "AGENTS.md"), newContent)
	if err := os.Chmod(filepath.Join(root, "AGENTS.md"), 0o700); err != nil {
		t.Fatalf("restore expected-after hook asset: %v", err)
	}

	if err := executeRecoveryPlanWithOptionsForTest(
		context.Background(),
		plan,
		paths,
		testRecoveryOptions(paths),
	); err != nil {
		t.Fatalf("ExecuteRecoveryPlan returned error: %v", err)
	}
	content, err := os.ReadFile(filepath.Join(root, "AGENTS.md"))
	if err != nil {
		t.Fatalf("ReadFile restored destination returned error: %v", err)
	}
	if string(content) != string(oldContent) {
		t.Fatalf("restored content = %q, want %q", content, oldContent)
	}
	restoredInfo, err := os.Stat(filepath.Join(root, "AGENTS.md"))
	if err != nil {
		t.Fatalf("stat restored instructions: %v", err)
	}
	if restoredInfo.Mode().Perm() != 0o700 {
		t.Fatalf("restored mode = %04o, want 0700", restoredInfo.Mode().Perm())
	}
	state, err := statefile.Load(t.Context(), paths.StatefilePath)
	if err != nil {
		t.Fatalf("statefile.Load returned error: %v", err)
	}
	if len(state.ManagedPaths()) != 1 || string(state.ManagedPaths()[0].ContentHash()) != oldHash {
		t.Fatalf("statefile = %#v, want before state hash %q", state, oldHash)
	}
	if !state.Equal(currentState) {
		t.Fatalf("recovered statefile lost durable dimensions: %#v", state)
	}
	if _, err := os.Stat(result.Directory); !os.IsNotExist(err) {
		t.Fatalf("recovery directory stat err = %v, want removed", err)
	}
}

func recoveryTestRelationSubject() topology.SubjectID {
	subject, err := topology.NewSubjectID(
		topology.SubjectHostRelation,
		"claude-code.plugin-carrier",
		"context7-managed",
	)
	if err != nil {
		panic(err)
	}
	return subject
}

func recoveryInstructionPathState(
	t *testing.T,
	name string,
	contentHash artifact.ContentHash,
) durable.ManagedPathState {
	t.Helper()
	id, err := entity.New(entity.KindInstructions, name)
	if err != nil {
		t.Fatal(err)
	}
	subject, err := topologyprojection.Subject(id, "instructions.project.agents")
	if err != nil {
		t.Fatal(err)
	}
	state, err := durable.NewManagedPathState(
		subject,
		[]target.Target{target.TargetCodex},
		target.ScopeProject,
		outputtest.Parse(t, "AGENTS.md"),
		contentHash,
		realization.PathProjectionFile,
		realization.PathPermissionsExecutableClass,
		0,
	)
	if err != nil {
		t.Fatal(err)
	}
	return state
}

func recoveryTestPendingCarrierInstall(
	t *testing.T,
	statefilePath string,
	manifestPath string,
) durablecarrier.PendingCarrierInstall {
	t.Helper()
	subjectKey, err := hostrelation.NewSubjectKey("context7@market")
	if err != nil {
		t.Fatal(err)
	}
	identity, _ := testManagedCarrierIdentityForScope(
		t,
		recoveryTestRelationSubject(),
		subjectKey,
		target.ScopeGlobal,
	)
	request, err := realizationdelegate.NewRequest(
		"claude-code.plugin-carrier.install",
		"claude-plugin-carrier-v1",
		"sha256:1111111111111111111111111111111111111111111111111111111111111111",
	)
	if err != nil {
		t.Fatal(err)
	}
	statefileKey, err := mutation.CanonicalDirectoryEntryKey(statefilePath)
	if err != nil {
		t.Fatal(err)
	}
	owner, err := stateauthority.New(mustObservedPathAuthority(t, statefileKey), manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	pending, err := durablecarrier.NewPendingCarrierInstall(owner, identity, request)
	if err != nil {
		t.Fatal(err)
	}
	return pending
}

func recoveryTestDelegateAttempt(subject topology.SubjectID) durableattempt.DelegateAttempt {
	attempt, err := durableattempt.NewDelegateAttempt(durableattempt.DelegateAttemptInput{
		Subject:         subject,
		Target:          target.TargetCodex,
		Scope:           target.ScopeProject,
		PlanIdentityKey: "sha256:delegate",
		ObservedAt:      time.Date(2026, time.June, 21, 12, 0, 0, 0, time.UTC),
		Status:          durableattempt.DelegateStatusSucceeded,
		Reason:          durableattempt.DelegateReasonNone,
	})
	if err != nil {
		panic(err)
	}
	return attempt
}

func recoveryTestHostRouteAttempt() durableattempt.HostRouteAttempt {
	attempt, err := durableattempt.NewHostRouteAttempt(durableattempt.HostRouteAttemptInput{
		Subject:          recoveryTestRelationSubject(),
		Target:           target.TargetClaudeCode,
		Scope:            target.ScopeGlobal,
		Operation:        lock.OperationInstall,
		RouteID:          "claude-code.plugin-carrier.install",
		RouteRequestHash: "sha256:1111111111111111111111111111111111111111111111111111111111111111",
		ObservedAt:       time.Date(2026, time.June, 21, 12, 0, 0, 0, time.UTC),
		ResultClass:      durableattempt.HostRouteResultAttemptedUnverified,
		Reason:           durableattempt.HostRouteReasonObservationUnavailable,
		AttemptObserved:  true,
		Observation:      observerelation.ObservationNotObserved,
		Postcondition:    observerelation.PostconditionUnknown,
	})
	if err != nil {
		panic(err)
	}
	return attempt
}

func TestRecoveryHostActionsRequireAggregateContractForContentPath(t *testing.T) {
	err := executeRecoveryHostActions(
		context.Background(),
		t.TempDir(),
		nil,
		[]recoveryHostAction{{
			Kind:        recovery.ActionKindRestoreDelete,
			ContentPath: "/mcpServers/context7",
		}},
		[]hostRollbackEntry{{}},
		nil,
		testAggregateCodecs(),
		visibilityEffectGate{},
	)
	if err == nil || !strings.Contains(err.Error(), "has no aggregate contract") {
		t.Fatalf("error = %v, want missing aggregate contract rejection", err)
	}
}

func TestRecoveryHostActionsRejectMismatchedAggregateContract(t *testing.T) {
	locked := managedMCPContract(t, "context7", "context7-command")
	contribution, present, err := locked.ManagedAggregateContribution()
	if err != nil || !present {
		t.Fatalf("ManagedAggregateContribution = %#v, %t, %v", contribution, present, err)
	}
	contract := contribution.Contribution().Contract()
	valid := recoveryHostAction{
		Kind:              recovery.ActionKindRestoreDelete,
		Scope:             target.ScopeProject,
		Destination:       contract.Address().Document().AggregateRoot().String(),
		ContentPath:       string(contract.Address().ContentPath()),
		AggregateContract: &contract,
	}
	for _, test := range []struct {
		name string
		edit func(*recoveryHostAction)
		want string
	}{
		{
			name: "scope",
			edit: func(action *recoveryHostAction) {
				action.Scope = target.ScopeGlobal
			},
			want: "scope",
		},
		{
			name: "destination",
			edit: func(action *recoveryHostAction) {
				action.Destination = "other.json"
			},
			want: "aggregate address",
		},
		{
			name: "content path",
			edit: func(action *recoveryHostAction) {
				action.ContentPath = "/mcpServers/other"
			},
			want: "aggregate address",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			action := valid
			test.edit(&action)
			err := executeRecoveryHostActions(
				context.Background(),
				t.TempDir(),
				nil,
				[]recoveryHostAction{action},
				[]hostRollbackEntry{{}},
				nil,
				testAggregateCodecs(),
				visibilityEffectGate{},
			)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("executeRecoveryHostActions error = %v, want containing %q", err, test.want)
			}
		})
	}
}

func TestRecoveryRollbackFailureDistinguishesCleanupResidue(t *testing.T) {
	primary := errors.New("recovery host effect failed")
	cleanupErr := errors.New("private rollback residue remains")

	err := recoveryRollbackFailure(primary, nil, cleanupErr)
	if !errors.Is(err, primary) || !errors.Is(err, cleanupErr) {
		t.Fatalf("recoveryRollbackFailure error = %v, want both causes", err)
	}
	if strings.Contains(err.Error(), "rollback failed") {
		t.Fatalf("cleanup-only error = %v, must not report host-state rollback failure", err)
	}
	if !strings.Contains(err.Error(), "cleanup recovery rollback stage") {
		t.Fatalf("cleanup-only error = %v, want private cleanup classification", err)
	}

	rollbackErr := errors.New("restore before-image failed")
	err = recoveryRollbackFailure(primary, rollbackErr, nil)
	if !errors.Is(err, primary) || !strings.Contains(err.Error(), "rollback failed") {
		t.Fatalf("rollback error = %v, want rollback failure classification", err)
	}
}

func writeRecoveryTestFile(t *testing.T, path string, content []byte) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
}

func writeRecoveryTestStatefile(t *testing.T, path string, snapshot durable.Snapshot) {
	t.Helper()

	content, err := statefile.Marshal(snapshot)
	if err != nil {
		t.Fatalf("statefile.Marshal returned error: %v", err)
	}
	writeRecoveryTestFile(t, path, content)
}
