package execute

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/isty2e/daem/internal/assurance/durable"
	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	"github.com/isty2e/daem/internal/assurance/observe"
	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/effect/journal"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/realization"
	"github.com/isty2e/daem/internal/supply/artifact/access"
	"github.com/isty2e/daem/internal/target"
	topologyprojection "github.com/isty2e/daem/internal/topology/projection"
	"github.com/isty2e/daem/test/outputtest"
)

func hasRootedPathFailureKind(err error, kind rootedpath.FailureKind) bool {
	var failure *rootedpath.Failure
	return errors.As(err, &failure) && failure.Kind() == kind
}

func TestRecoveryRejectsMissingDestinationResolverBeforeEffects(t *testing.T) {
	fixture := newProjectPathRecoveryFixture(t, projectInstructionRecoverySpec(
		"codex-project",
		target.TargetCodex,
		"AGENTS.md",
	))

	err := ExecuteRecoveryPlanWithOptions(
		context.Background(),
		fixture.plan,
		fixture.paths,
		RecoveryOptions{},
	)
	if err == nil || !strings.Contains(err.Error(), "recovery destination resolver is required") {
		t.Fatalf("ExecuteRecoveryPlanWithOptions error = %v, want missing-resolver rejection", err)
	}
	assertRecoveryTestContent(t, filepath.Join(fixture.projectRoot, "AGENTS.md"), fixture.after["AGENTS.md"])
	assertRecoveryJournalRetained(t, fixture.plan)
}

func TestRecoveryRejectsMissingStatePersistenceBeforeEffects(t *testing.T) {
	tests := []struct {
		name    string
		options func(Paths) RecoveryOptions
		want    string
	}{
		{
			name: "codec",
			options: func(paths Paths) RecoveryOptions {
				return RecoveryOptions{Resolver: destinationResolver(paths)}
			},
			want: "recovery state codec is required",
		},
		{
			name: "reader",
			options: func(paths Paths) RecoveryOptions {
				return RecoveryOptions{
					Resolver:   destinationResolver(paths),
					StateCodec: testStateCodec(),
				}
			},
			want: "recovery state reader is required",
		},
		{
			name: "filesystem",
			options: func(paths Paths) RecoveryOptions {
				return RecoveryOptions{
					Resolver:    destinationResolver(paths),
					StateCodec:  testStateCodec(),
					StateReader: testStateReader(paths.StatefilePath),
				}
			},
			want: "recovery filesystem is required",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newProjectPathRecoveryFixture(t, projectInstructionRecoverySpec(
				"codex-project",
				target.TargetCodex,
				"AGENTS.md",
			))

			err := ExecuteRecoveryPlanWithOptions(
				context.Background(),
				fixture.plan,
				fixture.paths,
				test.options(fixture.paths),
			)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("ExecuteRecoveryPlanWithOptions error = %v, want %q", err, test.want)
			}
			assertRecoveryTestContent(
				t,
				filepath.Join(fixture.projectRoot, "AGENTS.md"),
				fixture.after["AGENTS.md"],
			)
			assertRecoveryJournalRetained(t, fixture.plan)
		})
	}
}

func TestRecoveryRetainsEvidenceWhenStateReaderFailsBeforeEffects(t *testing.T) {
	fixture := newProjectPathRecoveryFixture(t, projectInstructionRecoverySpec(
		"codex-project",
		target.TargetCodex,
		"AGENTS.md",
	))
	readerErr := errors.New("injected state read failure")

	err := ExecuteRecoveryPlanWithOptions(
		context.Background(),
		fixture.plan,
		fixture.paths,
		RecoveryOptions{
			Resolver:   destinationResolver(fixture.paths),
			StateCodec: testStateCodec(),
			StateReader: func(context.Context) (durable.Snapshot, error) {
				return durable.Snapshot{}, readerErr
			},
			Filesystem: testFilesystem(),
		},
	)
	if !errors.Is(err, readerErr) {
		t.Fatalf("ExecuteRecoveryPlanWithOptions error = %v, want state read failure", err)
	}
	assertRecoveryTestContent(
		t,
		filepath.Join(fixture.projectRoot, "AGENTS.md"),
		fixture.after["AGENTS.md"],
	)
	assertRecoveryJournalRetained(t, fixture.plan)
}

func TestRecoveryRejectsDurableJournalDriftAfterFinalValidation(t *testing.T) {
	fixture := newProjectPathRecoveryFixture(t, projectInstructionRecoverySpec(
		"codex-project",
		target.TargetCodex,
		"AGENTS.md",
	))
	journalPath := filepath.Join(fixture.plan.OperationDir(), "journal.json")

	err := ExecuteRecoveryPlanWithOptions(context.Background(), fixture.plan, fixture.paths, RecoveryOptions{
		Resolver:    destinationResolver(fixture.paths),
		StateCodec:  testStateCodec(),
		StateReader: testStateReader(fixture.paths.StatefilePath),
		Filesystem:  testFilesystem(),
		reloadPlan: func(
			ctx context.Context,
			options journal.PlanLoadOptions,
		) (recovery.Plan, error) {
			content, err := os.ReadFile(journalPath)
			if err != nil {
				t.Fatalf("read recovery journal: %v", err)
			}
			var document map[string]json.RawMessage
			if err := json.Unmarshal(content, &document); err != nil {
				t.Fatalf("decode recovery journal: %v", err)
			}
			document["created_at"] = json.RawMessage(`"2099-01-02T03:04:05Z"`)
			content, err = json.MarshalIndent(document, "", "  ")
			if err != nil {
				t.Fatalf("encode recovery journal: %v", err)
			}
			if err := os.WriteFile(journalPath, content, 0o600); err != nil {
				t.Fatalf("rewrite recovery journal: %v", err)
			}
			return journal.LoadActivePlanWithOptions(ctx, fixture.paths.journalPaths(), options)
		},
	})
	if err == nil || !strings.Contains(err.Error(), "durable recovery journal changed before effects") {
		t.Fatalf("ExecuteRecoveryPlan error = %v, want durable-journal drift rejection", err)
	}
	assertRecoveryTestContent(t, filepath.Join(fixture.projectRoot, "AGENTS.md"), fixture.after["AGENTS.md"])
	assertRecoveryJournalRetained(t, fixture.plan)
}

func TestRecoveryRejectsProjectRootReplacementAfterFinalReload(t *testing.T) {
	fixture := newProjectPathRecoveryFixture(t, projectInstructionRecoverySpec(
		"codex-project",
		target.TargetCodex,
		"AGENTS.md",
	))
	movedRoot := fixture.projectRoot + "-moved"

	err := ExecuteRecoveryPlanWithOptions(context.Background(), fixture.plan, fixture.paths, RecoveryOptions{
		Resolver:    destinationResolver(fixture.paths),
		StateCodec:  testStateCodec(),
		StateReader: testStateReader(fixture.paths.StatefilePath),
		Filesystem:  testFilesystem(),
		reloadPlan: func(ctx context.Context, options journal.PlanLoadOptions) (recovery.Plan, error) {
			current, err := journal.LoadActivePlanWithOptions(ctx, fixture.paths.journalPaths(), options)
			if err != nil {
				return recovery.Plan{}, err
			}
			if err := os.Rename(fixture.projectRoot, movedRoot); err != nil {
				t.Fatalf("move selected project root: %v", err)
			}
			if err := os.Mkdir(fixture.projectRoot, 0o700); err != nil {
				t.Fatalf("create replacement project root: %v", err)
			}
			writeRecoveryTestFile(t, filepath.Join(fixture.projectRoot, "AGENTS.md"), []byte("replacement\n"))
			return current, nil
		},
	})
	if !hasRootedPathFailureKind(err, rootedpath.FailureRootReplaced) {
		t.Fatalf("ExecuteRecoveryPlan error = %v, want %s", err, rootedpath.FailureRootReplaced)
	}
	assertRecoveryTestContent(t, filepath.Join(fixture.projectRoot, "AGENTS.md"), []byte("replacement\n"))
	assertRecoveryTestContent(t, filepath.Join(movedRoot, "AGENTS.md"), fixture.after["AGENTS.md"])
	assertRecoveryJournalRetained(t, fixture.plan)
}

func TestRecoveryRejectsProjectAncestorSymlinkAfterFinalReload(t *testing.T) {
	const destination = ".agents/skills/recovery-ancestor"
	const marker = destination + "/SKILL.md"
	fixture := newProjectPathRecoveryFixture(t, projectSkillRecoverySpec("recovery-ancestor", destination))
	outside := filepath.Join(fixture.base, "outside")
	moved := filepath.Join(fixture.projectRoot, ".agents", "skills-original")
	outsideContent := []byte("outside sentinel\n")

	err := ExecuteRecoveryPlanWithOptions(context.Background(), fixture.plan, fixture.paths, RecoveryOptions{
		Resolver:    destinationResolver(fixture.paths),
		StateCodec:  testStateCodec(),
		StateReader: testStateReader(fixture.paths.StatefilePath),
		Filesystem:  testFilesystem(),
		reloadPlan: func(ctx context.Context, options journal.PlanLoadOptions) (recovery.Plan, error) {
			current, err := journal.LoadActivePlanWithOptions(ctx, fixture.paths.journalPaths(), options)
			if err != nil {
				return recovery.Plan{}, err
			}
			if err := os.Rename(filepath.Join(fixture.projectRoot, ".agents", "skills"), moved); err != nil {
				t.Fatalf("move project destination ancestor: %v", err)
			}
			if err := os.MkdirAll(filepath.Join(outside, "recovery-ancestor"), 0o700); err != nil {
				t.Fatalf("create outside directory: %v", err)
			}
			writeRecoveryTestFile(t, filepath.Join(outside, "recovery-ancestor", "SKILL.md"), outsideContent)
			if err := os.Symlink(outside, filepath.Join(fixture.projectRoot, ".agents", "skills")); err != nil {
				t.Fatalf("replace project ancestor with symlink: %v", err)
			}
			return current, nil
		},
	})
	if !hasRootedPathFailureKind(err, rootedpath.FailureAncestorSymlink) {
		t.Fatalf("ExecuteRecoveryPlan error = %v, want %s", err, rootedpath.FailureAncestorSymlink)
	}
	assertRecoveryTestContent(t, filepath.Join(outside, "recovery-ancestor", "SKILL.md"), outsideContent)
	assertRecoveryTestContent(t, filepath.Join(moved, "recovery-ancestor", "SKILL.md"), fixture.after[marker])
	assertRecoveryJournalRetained(t, fixture.plan)
}

func TestRecoveryFailureRollsBackPriorProjectWritesThroughRootAuthority(t *testing.T) {
	fixture := newProjectPathRecoveryFixture(
		t,
		projectInstructionRecoverySpec("codex-project", target.TargetCodex, "AGENTS.md"),
		projectInstructionRecoverySpec("claude-project", target.TargetClaudeCode, "CLAUDE.md"),
	)
	tamperedDestination := ""

	err := ExecuteRecoveryPlanWithOptions(context.Background(), fixture.plan, fixture.paths, RecoveryOptions{
		Resolver:    destinationResolver(fixture.paths),
		StateCodec:  testStateCodec(),
		StateReader: testStateReader(fixture.paths.StatefilePath),
		Filesystem:  testFilesystem(),
		reloadPlan: func(ctx context.Context, options journal.PlanLoadOptions) (recovery.Plan, error) {
			current, err := journal.LoadActivePlanWithOptions(ctx, fixture.paths.journalPaths(), options)
			if err != nil {
				return recovery.Plan{}, err
			}
			actions := current.Actions()
			if len(actions) != 2 {
				t.Fatalf("recovery actions = %d, want 2", len(actions))
			}
			tampered := actions[len(actions)-1]
			tamperedDestination = tampered.Destination
			if err := os.WriteFile(
				filepath.Join(current.OperationDir(), filepath.FromSlash(tampered.BackupPath)),
				[]byte("tampered backup\n"),
				0o600,
			); err != nil {
				t.Fatalf("tamper final recovery backup: %v", err)
			}
			return current, nil
		},
	})
	if err == nil || !strings.Contains(err.Error(), "does not match expected hash") {
		t.Fatalf("ExecuteRecoveryPlan error = %v, want backup hash mismatch", err)
	}
	if tamperedDestination == "" {
		t.Fatal("test did not select a backup to tamper")
	}
	for destination, content := range fixture.after {
		assertRecoveryTestContent(t, filepath.Join(fixture.projectRoot, filepath.FromSlash(destination)), content)
	}
	assertRecoveryJournalRetained(t, fixture.plan)
}

type projectPathRecoveryFixture struct {
	base        string
	projectRoot string
	paths       Paths
	plan        recovery.Plan
	after       map[string][]byte
}

type projectPathRecoverySpec struct {
	name             string
	entityKind       entity.Kind
	placementID      string
	consumerTargets  []target.Target
	contentKind      realization.PathProjectionContentKind
	permissionPolicy realization.PathPermissionPolicy
	destination      string
	marker           string
}

func projectInstructionRecoverySpec(
	name string,
	selectedTarget target.Target,
	destination string,
) projectPathRecoverySpec {
	var placementID string
	switch {
	case selectedTarget == target.TargetCodex && destination == "AGENTS.md":
		placementID = "instructions.project.agents"
	case selectedTarget == target.TargetClaudeCode && destination == "CLAUDE.md":
		placementID = "instructions.project.claude"
	default:
		panic(fmt.Sprintf(
			"unsupported project instruction recovery fixture target=%q destination=%q",
			selectedTarget,
			destination,
		))
	}
	return projectPathRecoverySpec{
		name:             name,
		entityKind:       entity.KindInstructions,
		placementID:      placementID,
		consumerTargets:  []target.Target{selectedTarget},
		contentKind:      realization.PathProjectionFile,
		permissionPolicy: realization.PathPermissionsExecutableClass,
		destination:      destination,
	}
}

func projectSkillRecoverySpec(name string, destination string) projectPathRecoverySpec {
	return projectPathRecoverySpec{
		name:             name,
		entityKind:       entity.KindSkill,
		placementID:      "skill.project.agents",
		consumerTargets:  []target.Target{target.TargetCodex},
		contentKind:      realization.PathProjectionDirectory,
		permissionPolicy: realization.PathPermissionsNone,
		destination:      destination,
		marker:           "SKILL.md",
	}
}

func newProjectPathRecoveryFixture(
	t *testing.T,
	specs ...projectPathRecoverySpec,
) projectPathRecoveryFixture {
	t.Helper()
	base := t.TempDir()
	projectRoot := filepath.Join(base, "project")
	stateDir := filepath.Join(base, "state")
	if err := os.MkdirAll(projectRoot, 0o700); err != nil {
		t.Fatalf("create project root: %v", err)
	}
	paths := Paths{
		RecoveryDir:   filepath.Join(stateDir, "recovery"),
		StateDir:      stateDir,
		StatefilePath: filepath.Join(stateDir, "state.json"),
		ManifestRoot:  projectRoot,
		DataDir:       filepath.Join(stateDir, "data"),
	}

	mutations := make([]journal.ManagedPathMutation, 0, len(specs))
	evidence := make([]observe.ManagedPathEvidence, 0, len(specs))
	currentPaths := make([]durable.ManagedPathState, 0, len(specs))
	nextPaths := make([]durable.ManagedPathState, 0, len(specs))
	afterSources := make(map[string]string, len(specs))
	afterContent := make(map[string][]byte, len(specs))
	for index, spec := range specs {
		entityID, err := entity.New(spec.entityKind, spec.name)
		if err != nil {
			t.Fatalf("construct recovery entity %q: %v", spec.name, err)
		}
		subject, err := topologyprojection.Subject(entityID, spec.placementID)
		if err != nil {
			t.Fatalf("lower recovery subject %q: %v", spec.name, err)
		}

		before := []byte("before:" + spec.destination + "\n")
		after := []byte("after:" + spec.destination + "\n")
		hostPath := filepath.Join(projectRoot, filepath.FromSlash(spec.destination))
		afterSource := filepath.Join(base, "after", fmt.Sprintf("%03d", index))
		beforeMarker := hostPath
		afterMarker := afterSource
		markerDestination := spec.destination
		liveMode := os.FileMode(0o600)
		expectedMode := os.FileMode(0o600)
		if spec.contentKind == realization.PathProjectionDirectory {
			beforeMarker = filepath.Join(hostPath, filepath.FromSlash(spec.marker))
			afterMarker = filepath.Join(afterSource, filepath.FromSlash(spec.marker))
			markerDestination = filepath.ToSlash(filepath.Join(spec.destination, spec.marker))
			liveMode = 0o700
			expectedMode = 0
		}
		writeRecoveryTestFile(t, beforeMarker, before)
		writeRecoveryTestFile(t, afterMarker, after)
		beforeHashValue, _, err := access.HashPath(context.Background(), hostPath)
		if err != nil {
			t.Fatalf("hash before recovery path %q: %v", spec.destination, err)
		}
		afterHashValue, _, err := access.HashPath(context.Background(), afterSource)
		if err != nil {
			t.Fatalf("hash after recovery path %q: %v", spec.destination, err)
		}
		beforeHash := beforeHashValue
		afterHash := afterHashValue
		previous, err := durable.NewManagedPathState(
			subject,
			spec.consumerTargets,
			target.ScopeProject,
			outputtest.Parse(t, spec.destination),
			beforeHash,
			spec.contentKind,
			spec.permissionPolicy,
			0,
		)
		if err != nil {
			t.Fatalf("construct previous recovery path %q: %v", spec.destination, err)
		}
		next, err := durable.NewManagedPathState(
			subject,
			spec.consumerTargets,
			target.ScopeProject,
			outputtest.Parse(t, spec.destination),
			afterHash,
			spec.contentKind,
			spec.permissionPolicy,
			0,
		)
		if err != nil {
			t.Fatalf("construct next recovery path %q: %v", spec.destination, err)
		}
		mutationRequest, err := journal.NewManagedPathReplaceMutation(
			subject,
			spec.consumerTargets,
			target.ScopeProject,
			outputtest.Parse(t, spec.destination),
			afterHash,
			beforeHash,
			spec.contentKind,
			expectedMode,
			previous,
		)
		if err != nil {
			t.Fatalf("construct recovery mutation %q: %v", spec.destination, err)
		}
		observation, err := observe.NewManagedPathEvidence(
			subject,
			outputtest.Parse(t, spec.destination),
			true,
			beforeHash,
			liveMode,
		)
		if err != nil {
			t.Fatalf("construct recovery evidence %q: %v", spec.destination, err)
		}
		mutations = append(mutations, mutationRequest)
		evidence = append(evidence, observation)
		currentPaths = append(currentPaths, previous)
		nextPaths = append(nextPaths, next)
		afterSources[spec.destination] = afterSource
		afterContent[markerDestination] = after
	}
	currentState, err := durable.NewSnapshot(durable.SnapshotInput{
		ManagedPaths: currentPaths,
	})
	if err != nil {
		t.Fatalf("construct current project recovery snapshot: %v", err)
	}
	nextState, err := durable.NewSnapshot(durable.SnapshotInput{
		ManagedPaths: nextPaths,
		PendingCarrierInstalls: []durablecarrier.PendingCarrierInstall{
			recoveryTestPendingCarrierInstall(
				t,
				paths.StatefilePath,
				filepath.Join(projectRoot, "daem.toml"),
			),
		},
	})
	if err != nil {
		t.Fatalf("construct next project recovery snapshot: %v", err)
	}
	createdAt := time.Date(2026, time.July, 13, 12, 0, 0, 0, time.UTC)
	if _, err := journal.CaptureJournalWithOptions(
		context.Background(),
		paths.journalPaths(),
		journal.OperationID(createdAt),
		createdAt,
		currentState,
		nextState,
		journal.CaptureOptions{
			Filesystem:           testFilesystem(),
			ManagedPathMutations: mutations,
			ManagedPathEvidence:  evidence,
			Resolver:             destinationResolver(paths),
			StateEncoder:         testStateCodec(),
		},
	); err != nil {
		t.Fatalf("CaptureJournalWithOptions returned error: %v", err)
	}
	for destination, source := range afterSources {
		hostPath := filepath.Join(projectRoot, filepath.FromSlash(destination))
		if err := os.RemoveAll(hostPath); err != nil {
			t.Fatalf("remove before recovery path %q: %v", destination, err)
		}
		if err := os.Rename(source, hostPath); err != nil {
			t.Fatalf("publish after recovery path %q: %v", destination, err)
		}
	}
	writeRecoveryTestStatefile(t, paths.StatefilePath, currentState)
	recoveryPlan, err := journal.LoadActivePlanWithOptions(
		context.Background(),
		paths.journalPaths(),
		testPlanLoadOptions(paths),
	)
	if err != nil {
		t.Fatalf("LoadActivePlan returned error: %v", err)
	}
	if recoveryPlan.Classification() != recovery.ClassificationNeedsRollback {
		t.Fatalf("recovery classification = %q, want %q", recoveryPlan.Classification(), recovery.ClassificationNeedsRollback)
	}
	return projectPathRecoveryFixture{
		base:        base,
		projectRoot: projectRoot,
		paths:       paths,
		plan:        recoveryPlan,
		after:       afterContent,
	}
}

func assertRecoveryTestContent(t *testing.T, path string, want []byte) {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read recovery test path %q: %v", path, err)
	}
	if string(content) != string(want) {
		t.Fatalf("recovery test path %q content = %q, want %q", path, content, want)
	}
}

func assertRecoveryTestMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat recovery test path %q: %v", path, err)
	}
	if got := info.Mode().Perm(); got != want.Perm() {
		t.Fatalf("recovery test path %q mode = %04o, want %04o", path, got, want.Perm())
	}
}

func assertRecoveryJournalRetained(t *testing.T, plan recovery.Plan) {
	t.Helper()
	if _, err := os.Stat(plan.OperationDir()); err != nil {
		t.Fatalf("recovery journal was not retained: %v", err)
	}
}
