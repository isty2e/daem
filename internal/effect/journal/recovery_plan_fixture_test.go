package journal

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/output/ownership"
	"github.com/isty2e/daem/internal/realization"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
	topologyprojection "github.com/isty2e/daem/internal/topology/projection"
)

const (
	testOperationID  = "20260625T000000.000000000Z-apply"
	testOperationDir = "recovery/20260625T000000.000000000Z-apply"

	testBackupPath = "backups/AGENTS.md"
)

var (
	testBeforeHash = testContentHashString("before")
	testAfterHash  = testContentHashString("after")
	testDirtyHash  = testContentHashString("dirty")
)

func mustBuildRecoveryPlan(
	t *testing.T,
	journal recoveryJournal,
	currentState durable.Snapshot,
	observations []recoveryPathObservation,
	backupObservations []recoveryBackupObservation,
) recovery.Plan {
	t.Helper()

	plan, err := buildRecoveryPlan(testOperationID, testOperationDir, journal, currentState, observations, backupObservations, nil, ownership.EmptyRegistry(), testStateCodec())
	if err != nil {
		t.Fatalf("buildRecoveryPlan returned error: %v", err)
	}

	return plan
}

func defaultRecoveryJournal() recoveryJournal {
	return recoveryJournalFor(defaultRecoveryEntry())
}

func recoveryJournalFor(entries ...recoveryEntry) recoveryJournal {
	beforeResources := make([]durable.ManagedPathState, 0, len(entries))
	afterResources := make([]durable.ManagedPathState, 0, len(entries))
	for _, entry := range entries {
		if !entry.StateIndependent && entry.Before.Existed {
			beforeResources = append(beforeResources, resourceState(entry, entry.Before.ContentHash))
		}
		if !entry.StateIndependent && entry.ExpectedAfter.Existed {
			afterResources = append(afterResources, resourceState(entry, entry.ExpectedAfter.ContentHash))
		}
	}

	return recoveryJournal{
		Version:                recoveryJournalVersion,
		OperationID:            testOperationID,
		Operation:              recoveryOperationApply,
		CreatedAt:              "2026-06-25T00:00:00Z",
		ManifestRootProvenance: testRecoveryManifestRootProvenance(),
		Entries:                append([]recoveryEntry(nil), entries...),
		StatefileBefore:        statefileFor(beforeResources...),
		StatefileAfter:         statefileFor(afterResources...),
	}
}

func testRecoveryManifestRootProvenance() recoveryRootProvenance {
	return recoveryRootProvenance{
		PhysicalRoot:      "/test/project",
		ObjectFingerprint: "sha256:" + strings.Repeat("1", 64),
		MountFingerprint:  "sha256:" + strings.Repeat("2", 64),
	}
}

func testRecoveryGlobalPathBinding(resolvedPath string) *recoveryGlobalPathBinding {
	return &recoveryGlobalPathBinding{
		ResolvedPath: resolvedPath,
		RootProvenance: recoveryRootProvenance{
			PhysicalRoot:      filepath.Dir(resolvedPath),
			ObjectFingerprint: "sha256:" + strings.Repeat("3", 64),
			MountFingerprint:  "sha256:" + strings.Repeat("4", 64),
		},
	}
}

func defaultRecoveryEntry() recoveryEntry {
	return recoveryEntryFor("project", "AGENTS.md", testBeforeHash, testAfterHash, testBackupPath)
}

func recoveryEntryFor(name string, destination string, beforeHash string, afterHash string, backupPath string) recoveryEntry {
	beforeHash = testContentHashString(beforeHash)
	afterHash = testContentHashString(afterHash)
	selectedTarget, scope, placementID := testManagedPathContract(destination)
	subject := mustTestManagedPathSubject(name, placementID)
	return recoveryEntry{
		Subject: persistedSubjectRef{
			Kind:      string(subject.Kind()),
			Namespace: subject.Namespace(),
			Name:      subject.Key(),
		},
		Targets:     []string{string(selectedTarget)},
		Scope:       string(scope),
		Path:        destination,
		ContentKind: string(realization.PathProjectionFile),
		Before: persistedBeforePathState(recovery.BeforePathState{
			Existed:     true,
			PathMode:    testRecoveryPermissionMode(0o600),
			Kind:        recovery.PathKindFile,
			ContentHash: beforeHash,
			BackupPath:  backupPath,
		}),
		ExpectedAfter: persistedExpectedPathState(recovery.ExpectedPathState{
			Existed:     true,
			PathMode:    testRecoveryPermissionMode(0o600),
			Kind:        recovery.PathKindFile,
			ContentHash: afterHash,
		}),
		StateBefore: recoveryManagedMembership{
			Managed:     true,
			ContentHash: beforeHash,
		},
		StateExpectedAfter: recoveryManagedMembership{
			Managed:     true,
			ContentHash: afterHash,
		},
	}
}

func testRecoveryPermissionMode(mode uint32) *recovery.PermissionMode {
	value := recovery.PermissionMode(mode)
	return &value
}

func beforeStatefile() durable.Snapshot {
	return defaultRecoveryJournal().StatefileBefore
}

func afterStatefile() durable.Snapshot {
	return defaultRecoveryJournal().StatefileAfter
}

func statefileFor(resources ...durable.ManagedPathState) durable.Snapshot {
	snapshot, err := durable.NewSnapshot(durable.SnapshotInput{
		ManagedPaths: append([]durable.ManagedPathState(nil), resources...),
	})
	if err != nil {
		panic(err)
	}
	return snapshot
}

func resourceState(entry recoveryEntry, contentHash string) durable.ManagedPathState {
	policy := realization.PathPermissionsExecutableClass
	if entry.ContentKind == string(realization.PathProjectionDirectory) {
		policy = realization.PathPermissionsNone
	}
	return resourceStateWithPermissions(entry, contentHash, policy, 0)
}

func resourceStateWithPermissions(
	entry recoveryEntry,
	contentHash string,
	policy realization.PathPermissionPolicy,
	fileMode os.FileMode,
) durable.ManagedPathState {
	subject, err := entry.Subject.canonical()
	if err != nil {
		panic("test recovery entry requires one canonical subject")
	}
	consumers := make([]target.Target, 0, len(entry.Targets))
	for _, value := range entry.Targets {
		consumers = append(consumers, target.Target(value))
	}
	destination, err := output.Parse(entry.Path)
	if err != nil {
		panic("test recovery entry requires one canonical destination")
	}
	state, err := durable.NewManagedPathState(
		subject,
		consumers,
		target.Scope(entry.Scope),
		destination,
		testContentHash(contentHash),
		realization.PathProjectionContentKind(entry.ContentKind),
		policy,
		fileMode,
	)
	if err != nil {
		panic(err)
	}
	return state
}

func mustTestManagedPathSubject(name string, placementID string) topology.SubjectID {
	id, err := entity.New(entity.KindInstructions, name)
	if err != nil {
		panic(err)
	}
	subject, err := topologyprojection.Subject(id, placementID)
	if err != nil {
		panic(err)
	}
	return subject
}

func testManagedPathContract(destination string) (target.Target, target.Scope, string) {
	switch destination {
	case "AGENTS.md":
		return target.TargetCodex, target.ScopeProject, "instructions.project.agents"
	case "CLAUDE.md":
		return target.TargetClaudeCode, target.ScopeProject, "instructions.project.claude"
	case "GEMINI.md":
		return target.TargetAntigravityCLI, target.ScopeProject, "instructions.project.gemini"
	case "~/.codex/AGENTS.md":
		return target.TargetCodex, target.ScopeGlobal, "instructions.global.codex"
	default:
		panic("test destination has no admitted Instructions placement: " + destination)
	}
}

func beforePathObservation(entry recoveryEntry) recoveryPathObservation {
	if !entry.Before.Existed {
		return recoveryPathObservation{
			Path:        entry.Path,
			ContentPath: entry.ContentPath,
			Exists:      false,
			PathExisted: entry.Before.PathExisted,
			PathMode:    entry.Before.PathMode,
		}
	}

	return recoveryPathObservation{
		Path:        entry.Path,
		ContentPath: entry.ContentPath,
		Exists:      true,
		PathExisted: entry.Before.PathExisted,
		PathMode:    entry.Before.PathMode,
		Kind:        entry.Before.Kind,
		ContentHash: entry.Before.ContentHash,
		LinkTarget:  entry.Before.LinkTarget,
	}
}

func afterPathObservation(entry recoveryEntry) recoveryPathObservation {
	if !entry.ExpectedAfter.Existed {
		return recoveryPathObservation{
			Path:        entry.Path,
			ContentPath: entry.ContentPath,
			Exists:      false,
			PathExisted: entry.ExpectedAfter.PathExisted,
			PathMode:    entry.ExpectedAfter.PathMode,
		}
	}

	return recoveryPathObservation{
		Path:        entry.Path,
		ContentPath: entry.ContentPath,
		Exists:      true,
		PathExisted: entry.ExpectedAfter.PathExisted,
		PathMode:    entry.ExpectedAfter.PathMode,
		Kind:        entry.ExpectedAfter.Kind,
		ContentHash: entry.ExpectedAfter.ContentHash,
		LinkTarget:  entry.ExpectedAfter.LinkTarget,
	}
}

func dirtyPathObservation(entry recoveryEntry) recoveryPathObservation {
	return recoveryPathObservation{
		Path:        entry.Path,
		Exists:      true,
		PathMode:    testRecoveryPermissionMode(0o600),
		Kind:        recovery.PathKindFile,
		ContentHash: testDirtyHash,
	}
}

func matchingBackupObservation(entry recoveryEntry) recoveryBackupObservation {
	return recoveryBackupObservation{
		BackupPath:  entry.Before.BackupPath,
		Exists:      true,
		Kind:        entry.Before.Kind,
		ContentHash: entry.Before.ContentHash,
	}
}

func recoveryActionFromEntryForTest(entry recoveryEntry) (recovery.Action, error) {
	journal := recoveryJournal{
		Version:                recoveryJournalVersion,
		OperationID:            testOperationID,
		Operation:              recoveryOperationApply,
		CreatedAt:              "2026-06-25T00:00:00Z",
		ManifestRootProvenance: testRecoveryManifestRootProvenance(),
		Entries:                []recoveryEntry{entry},
		StatefileBefore:        durable.EmptySnapshot(),
		StatefileAfter:         durable.EmptySnapshot(),
	}
	plan, err := buildRecoveryPlan(
		testOperationID,
		testOperationDir,
		journal,
		durable.EmptySnapshot(),
		[]recoveryPathObservation{beforePathObservation(entry)},
		nil,
		nil,
		ownership.EmptyRegistry(),
		testStateCodec(),
	)
	if err != nil {
		return recovery.Action{}, err
	}
	actions := plan.GuardedActions()
	if len(actions) != 1 {
		return recovery.Action{}, fmt.Errorf("guarded recovery actions = %d, want one", len(actions))
	}
	return actions[0], nil
}

func requireClassification(t *testing.T, plan recovery.Plan, want recovery.Classification) {
	t.Helper()

	if plan.Classification() != want {
		t.Fatalf("recovery.Classification = %q, want %q", plan.Classification(), want)
	}
}

func requireHealthyPlan(t *testing.T, plan recovery.Plan) {
	t.Helper()

	if plan.Blocked() {
		t.Fatalf("Blocked() = true, want false")
	}
	if plan.HasErrors() {
		t.Fatalf("HasErrors() = true, want false")
	}
}

func requireOnlyAction(t *testing.T, plan recovery.Plan, kind recovery.ActionKind, reason string) recovery.Action {
	t.Helper()

	actions := plan.Actions()
	if len(actions) != 1 {
		t.Fatalf("actions = %#v, want exactly one action", actions)
	}
	if actions[0].Kind != kind || actions[0].Reason != reason {
		t.Fatalf("action = %#v, want kind %q reason %q", actions[0], kind, reason)
	}

	return actions[0]
}

func requireAction(t *testing.T, plan recovery.Plan, kind recovery.ActionKind, reason string) recovery.Action {
	t.Helper()

	for _, action := range plan.Actions() {
		if action.Kind == kind && action.Reason == reason {
			return action
		}
	}
	t.Fatalf("actions = %#v, want kind %q reason %q", plan.Actions(), kind, reason)
	return recovery.Action{}
}

func requireErrorAction(t *testing.T, plan recovery.Plan, reason string, detail string) recovery.Action {
	t.Helper()

	action := requireAction(t, plan, recovery.ActionKindError, reason)
	if action.Detail != detail {
		t.Fatalf("error detail = %q, want %q", action.Detail, detail)
	}
	if !plan.Blocked() {
		t.Fatalf("Blocked() = false, want true")
	}
	if !plan.HasErrors() {
		t.Fatalf("HasErrors() = false, want true")
	}

	return action
}

func requireEntryFacts(t *testing.T, action recovery.Action, entry recoveryEntry) {
	t.Helper()

	wantSubject, err := entry.Subject.canonical()
	if err != nil {
		t.Fatalf("entry subject = %#v, error = %v, want canonical subject", entry.Subject, err)
	}
	gotSubject, hasSubject := action.SubjectID()
	if !hasSubject || gotSubject != wantSubject {
		t.Fatalf("action identity = subject %#v/%t, want subject %q", gotSubject, hasSubject, wantSubject)
	}
	if action.Target != target.Target(entry.Target) {
		t.Fatalf("Target = %q, want %q", action.Target, entry.Target)
	}
	wantConsumers := make([]target.Target, 0, len(entry.Targets))
	for _, value := range entry.Targets {
		wantConsumers = append(wantConsumers, target.Target(value))
	}
	if !slices.Equal(action.ConsumerTargets, wantConsumers) {
		t.Fatalf("ConsumerTargets = %v, want %v", action.ConsumerTargets, wantConsumers)
	}
	if action.Scope != target.Scope(entry.Scope) {
		t.Fatalf("Scope = %q, want %q", action.Scope, entry.Scope)
	}
	if action.Destination != entry.Path {
		t.Fatalf("Destination = %q, want %q", action.Destination, entry.Path)
	}
}
