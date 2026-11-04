package record

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/output/ownership"
	"github.com/isty2e/daem/internal/realization"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
	topologyprojection "github.com/isty2e/daem/internal/topology/projection"
)

const (
	testOperationID  = "20260625T000000.000000000Z-apply"
	testOperationDir = "recovery/20260625T000000.000000000Z-apply"
	testBackupPath   = "backups/AGENTS.md"
)

var (
	testBeforeHash = testContentHashString("before")
	testAfterHash  = testContentHashString("after")
	testDirtyHash  = testContentHashString("dirty")
)

type requiredRemovalStates struct {
	before   bool
	expected bool
}

func recoveryActionFromEntryForTest(entry recoveryEntry) (recovery.Action, error) {
	journal := recoveryJournal{
		Version: OrdinaryVersion, OperationID: testOperationID, Operation: OperationApply, CreatedAt: "2026-06-25T00:00:00Z",
		ManifestRootProvenance: testRecoveryManifestRootProvenance(), Entries: []recoveryEntry{entry},
		StatefileBefore: durable.EmptySnapshot(), StatefileAfter: durable.EmptySnapshot(),
	}
	fingerprint, err := recoveryJournalAuthorityFingerprint(journal, testStateCodec())
	if err != nil {
		return recovery.Action{}, err
	}
	authority, err := canonicalRecoveryAuthority(journal, testOperationDir, nil, nil, fingerprint)
	if err != nil {
		return recovery.Action{}, err
	}
	selection, err := recovery.NewSelection(authority, []int{0})
	if err != nil {
		return recovery.Action{}, err
	}
	before := entry.Before.canonical()
	observation := recovery.PathEvidence{Path: entry.Path, ContentPath: entry.ContentPath, Exists: before.Existed, PathExisted: before.PathExisted, PathMode: before.PathMode}
	if before.Existed {
		observation.Kind, observation.ContentHash, observation.LinkTarget = before.Kind, before.ContentHash, before.LinkTarget
	}
	plan, err := recovery.Classify(authority, selection, durable.EmptySnapshot(), []recovery.PathEvidence{observation}, nil, ownership.EmptyRegistry())
	if err != nil {
		return recovery.Action{}, err
	}
	actions := plan.GuardedActions()
	if len(actions) != 1 {
		return recovery.Action{}, fmt.Errorf("guarded recovery actions = %d, want one", len(actions))
	}
	return actions[0], nil
}

func defaultRecoveryJournal() recoveryJournal {
	return recoveryJournalFor(defaultRecoveryEntry())
}

func recoveryJournalFor(entries ...recoveryEntry) recoveryJournal {
	entries = append([]recoveryEntry(nil), entries...)
	for index := range entries {
		if entries[index].RemovalTransition == nil {
			entries[index].RemovalTransition = testRemovalTransition(entries[index])
		}
	}
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

	removalIntents := testRemovalIntents(entries)
	return recoveryJournal{
		Version:                OrdinaryVersion,
		OperationID:            testOperationID,
		Operation:              OperationApply,
		CreatedAt:              "2026-06-25T00:00:00Z",
		ManifestRootProvenance: testRecoveryManifestRootProvenance(),
		Entries:                append([]recoveryEntry(nil), entries...),
		RemovalIntents:         removalIntents,
		StatefileBefore:        statefileFor(beforeResources...),
		StatefileAfter:         statefileFor(afterResources...),
	}
}

func testRemovalTransition(entry recoveryEntry) *recoveryRemovalTransition {
	before := entry.Before
	before.BackupPath = ""
	expected := entry.ExpectedAfter
	requirements, required := testRemovalStateRequirements(entry)
	if !required {
		return nil
	}
	transition := &recoveryRemovalTransition{}
	if requirements.before {
		transition.Before = &before
	}
	if requirements.expected {
		transition.ExpectedAfter = &expected
	}
	return transition
}

func testRemovalIntents(entries []recoveryEntry) []recoveryRemovalIntent {
	intents := make([]recovery.RemovalIntent, 0, len(entries))
	for index, entry := range entries {
		requirements, required := testRemovalStateRequirements(entry)
		if !required {
			continue
		}
		scope, err := target.ParseScope(entry.Scope)
		if err != nil {
			panic(err)
		}
		destination, err := output.Parse(entry.Path)
		if err != nil {
			panic(err)
		}
		states := make([]recovery.RemovalState, 0, 2)
		if requirements.before {
			state, err := recovery.NewBeforeRemovalState(entry.Before.canonical())
			if err != nil {
				panic(err)
			}
			states = append(states, state)
		}
		if requirements.expected {
			state, err := recovery.NewExpectedRemovalState(entry.ExpectedAfter.canonical())
			if err != nil {
				panic(err)
			}
			states = append(states, state)
		}
		names, err := mutationfs.NewLogicalRemovalNames(
			fmt.Sprintf(".daem-tombstone-%032x", index),
			fmt.Sprintf(".daem-cleanup-%032x", index),
		)
		if err != nil {
			panic(err)
		}
		manifestRoot, err := canonicalRecoveryRootProvenance(testRecoveryManifestRootProvenance())
		if err != nil {
			panic(err)
		}
		namespace, err := recovery.NewExistingParentAuthority(manifestRoot, names)
		if err != nil {
			panic(err)
		}
		demand, err := recovery.NewRemovalDemand(scope, destination, states)
		if err != nil {
			panic(err)
		}
		intent, err := recovery.NewRemovalIntent(demand, namespace)
		if err != nil {
			panic(err)
		}
		intents = append(intents, intent)
	}
	persisted, err := persistedRecoveryRemovalIntents(intents)
	if err != nil {
		panic(err)
	}
	return persisted
}

func testRemovalStateRequirements(entry recoveryEntry) (requiredRemovalStates, bool) {
	if entry.ContentPath != "" {
		return testRemovalStateTransition(entry.Before.Existed, entry.ExpectedAfter.Existed)
	}
	if entry.ContentKind == string(realization.PathProjectionDirectory) && entry.Before.Existed && entry.ExpectedAfter.Existed {
		return requiredRemovalStates{before: true, expected: true}, true
	}
	return testRemovalStateTransition(entry.Before.Existed, entry.ExpectedAfter.Existed)
}

func testRemovalStateTransition(before bool, expected bool) (requiredRemovalStates, bool) {
	switch {
	case !before && expected:
		return requiredRemovalStates{expected: true}, true
	case before && !expected:
		return requiredRemovalStates{before: true}, true
	default:
		return requiredRemovalStates{}, false
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

func mustPersistedAggregateContract(t testing.TB, contract aggregate.ProjectionContract) *recoveryAggregateContract {
	t.Helper()
	persisted, err := persistedAggregateContract(contract)
	if err != nil {
		t.Fatal(err)
	}
	return persisted
}

func managedPathRecoveryEntry() recoveryEntry {
	return recoveryEntry{
		Subject: persistedSubjectRef{
			Kind:      "projection",
			Namespace: "skill.project.agents",
			Name:      "skill:oracle",
		},
		Targets:     []string{"codex"},
		Scope:       "project",
		Path:        ".agents/skills/oracle",
		ContentKind: "directory",
		Before: persistedBeforePathState(recovery.BeforePathState{
			Existed:     true,
			Kind:        recovery.PathKindDirectory,
			ContentHash: testBeforeHash,
			BackupPath:  "backup-0000",
		}),
		ExpectedAfter: persistedExpectedPathState(recovery.ExpectedPathState{
			Existed:     true,
			Kind:        recovery.PathKindDirectory,
			ContentHash: testAfterHash,
		}),
		StateBefore: recoveryManagedMembership{
			Managed:     true,
			ContentHash: testBeforeHash,
		},
		StateExpectedAfter: recoveryManagedMembership{
			Managed:     true,
			ContentHash: testAfterHash,
		},
	}
}

func globalAcquireRecoveryEntry(t *testing.T) recoveryEntry {
	t.Helper()
	entry := recoveryEntryFor("global", "~/.codex/AGENTS.md", "", testAfterHash, "")
	entry.GlobalPathBinding = testRecoveryGlobalPathBinding("/tmp/daem-global-config.json")
	entry.Before = persistedBeforePathState(recovery.BeforePathState{Existed: false})
	entry.ExpectedAfter.PathExisted = false
	entry.StateBefore = recoveryManagedMembership{}
	return entry
}

func journalWithVersion(t *testing.T, content []byte, version int) []byte {
	t.Helper()
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(content, &envelope); err != nil {
		t.Fatal(err)
	}
	envelope["version"] = json.RawMessage(fmt.Sprint(version))
	encoded, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}

func setNestedStateField(t testing.TB, content json.RawMessage, name string, value json.RawMessage) json.RawMessage {
	t.Helper()
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(content, &fields); err != nil {
		t.Fatal(err)
	}
	fields[name] = value
	encoded, err := json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}
