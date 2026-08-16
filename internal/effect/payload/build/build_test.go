package build

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/desired"
	hookassetresource "github.com/isty2e/daem/internal/desired/hookasset"
	instructionsresource "github.com/isty2e/daem/internal/desired/instructions"
	skillresource "github.com/isty2e/daem/internal/desired/skill"
	payloadmodel "github.com/isty2e/daem/internal/effect/payload"
	lock "github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/realization/lock/snapshottest"
	"github.com/isty2e/daem/internal/supply/artifact"
	skillrepair "github.com/isty2e/daem/internal/supply/compat/skill/repair"
	"github.com/isty2e/daem/internal/supply/source"
	"github.com/isty2e/daem/internal/supply/source/sourcetest"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
	topologyhook "github.com/isty2e/daem/internal/topology/hook"
	"github.com/isty2e/daem/test/outputtest"
)

func TestBuildPayloadSetMaterializesLockedInstruction(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	writeHostOutputTestFile(t, root, "instructions/AGENTS.md", "agent rules\n")
	sourceSpec := sourcetest.Local(t, "instructions/AGENTS.md", source.LocalSourceModeVendor)
	locked := lockedInstructionsForPath(t, ctx, root, "project", sourceSpec, "instructions/AGENTS.md", target.TargetCodex)
	environment := hostOutputEnvironment(t, desired.Spec{
		Instructions: []instructionsresource.Instructions{
			instructionResource(t, "project", sourceSpec, target.TargetCodex),
		},
	})
	payloads, err := PayloadSet(ctx, Input{
		Paths:                      hostOutputTestPaths(t, root),
		Environment:                environment,
		Lockfile:                   snapshottest.File(t, locked...),
		Selection:                  mustHostOutputSelection(t, "codex"),
		ManagedPathPayloadSubjects: []topology.SubjectID{locked[1].SubjectID()},
	})
	if err != nil {
		t.Fatalf("PayloadSet returned error: %v", err)
	}

	payload, ok := payloads.LookupSubject(locked[1].SubjectID())
	if !ok {
		t.Fatal("instruction payload missing")
	}
	file, ok := payload.File()
	if !ok {
		t.Fatal("File returned no file variant")
	}
	if string(file.Bytes()) != "agent rules\n" {
		t.Fatalf("Content = %q, want source bytes", file.Bytes())
	}
	materializedIdentity, _ := locked[0].MaterializedFileIdentity()
	if payload.Hash() != materializedIdentity.ContentHash() {
		t.Fatalf("Hash = %q, want %q", payload.Hash(), materializedIdentity.ContentHash())
	}
	if file.Mode() != 0o600 {
		t.Fatalf("FileMode = %04o, want 0600", file.Mode())
	}
}

func TestBuildPayloadSetDoesNotInitializeSupplyWithoutRequiredSubjects(t *testing.T) {
	if _, err := PayloadSet(t.Context(), Input{}); err != nil {
		t.Fatalf("PayloadSet with no required subjects returned error: %v", err)
	}

	_, err := PayloadSet(t.Context(), Input{
		ManagedPathPayloadSubjects: []topology.SubjectID{{}},
	})
	if err == nil || !strings.Contains(err.Error(), "not an entity-backed Skill projection") {
		t.Fatalf("PayloadSet malformed subject error = %v, want classification before resolver initialization", err)
	}
}

func TestMaterializeLockedFileRejectsDirectorySourceBeforeReadingContent(t *testing.T) {
	root := t.TempDir()
	writeHostOutputTestFile(t, root, "instructions/AGENTS.md", "agent rules\n")
	fileSource := sourcetest.Local(t, "instructions/AGENTS.md", source.LocalSourceModeVendor)
	locked := lockedInstructionsForPath(
		t,
		t.Context(),
		root,
		"project",
		fileSource,
		"instructions/AGENTS.md",
		target.TargetCodex,
	)
	if err := os.MkdirAll(filepath.Join(root, "skills", "directory"), 0o700); err != nil {
		t.Fatalf("create directory source: %v", err)
	}
	directorySource := sourcetest.Local(t, "skills/directory", source.LocalSourceModeVendor)
	resolvers := sourceResolverOnce{paths: hostOutputTestPaths(t, root)}
	resolver, err := resolvers.get()
	if err != nil {
		t.Fatalf("initialize source resolver: %v", err)
	}

	_, err = materializeLockedFile(t.Context(), resolver, directorySource, locked[0], false)
	if err == nil || !strings.Contains(err.Error(), "expected file artifact") {
		t.Fatalf("materializeLockedFile directory error = %v, want file-kind rejection", err)
	}
}

func TestBuildPayloadSetRejectsInstructionLockProblems(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	writeHostOutputTestFile(t, root, "instructions/AGENTS.md", "agent rules\n")
	sourceSpec := sourcetest.Local(t, "instructions/AGENTS.md", source.LocalSourceModeVendor)
	validLocked := lockedInstructionsForPath(t, ctx, root, "project", sourceSpec, "instructions/AGENTS.md", target.TargetCodex)
	environment := hostOutputEnvironment(t, desired.Spec{
		Instructions: []instructionsresource.Instructions{
			instructionResource(t, "project", sourceSpec, target.TargetCodex),
		},
	})
	input := Input{
		Paths:                      hostOutputTestPaths(t, root),
		Environment:                environment,
		Selection:                  mustHostOutputSelection(t, "codex"),
		ManagedPathPayloadSubjects: []topology.SubjectID{validLocked[1].SubjectID()},
	}
	validIdentity, ok := validLocked[0].ExactSupply()
	if !ok {
		t.Fatal("valid lock fixture is missing exact Supply")
	}
	staleIdentity, err := artifact.NewExactIdentity(
		validIdentity.SourceID(),
		validIdentity.ResolvedRef(),
		validIdentity.Kind(),
		artifact.HashFileContent([]byte("stale instructions")),
	)
	if err != nil {
		t.Fatalf("NewExactIdentity returned error: %v", err)
	}
	staleLocked := append([]lock.LockedSubjectContract{
		lockedInstructionExactSupply(t, "project", staleIdentity),
	}, validLocked[1:]...)

	for _, test := range []struct {
		name     string
		subjects []lock.LockedSubjectContract
		wantErr  string
	}{
		{
			name:    "missing lock entry",
			wantErr: `instructions "project": missing lockfile entry`,
		},
		{
			name:     "stale exact Supply",
			subjects: staleLocked,
			wantErr:  `source identity does not match lockfile entry`,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			input.Lockfile = snapshottest.File(t, test.subjects...)
			_, err := PayloadSet(ctx, input)
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("PayloadSet error = %v, want containing %q", err, test.wantErr)
			}
		})
	}
}

func TestPayloadVerifyHashForRejectsMismatch(t *testing.T) {
	subject, err := topology.NewSubjectID(topology.SubjectProjection, "instructions", "project")
	if err != nil {
		t.Fatalf("NewSubjectID returned error: %v", err)
	}
	value, err := payloadmodel.NewFilePayload(subject, []byte("payload"), 0o600)
	if err != nil {
		t.Fatalf("NewFilePayload returned error: %v", err)
	}
	plannedHash := artifact.HashFileContent([]byte("planned"))
	err = value.VerifyHash(plannedHash, outputtest.Parse(t, "AGENTS.md"))
	want := `host payload hash "` + string(value.Hash()) + `" does not match planned hash "` + string(plannedHash) + `" for "AGENTS.md"`
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("VerifyHash error = %v, want hash mismatch", err)
	}
}

func TestBuildPayloadSetSkipsSkillsWithoutRequiredProjectionEffects(t *testing.T) {
	root := t.TempDir()
	missingSource := sourcetest.Local(t, "skills/unavailable", source.LocalSourceModeVendor)
	environment := hostOutputEnvironment(t, desired.Spec{
		Skills: []skillresource.Skill{
			skillResource(t, "unavailable", missingSource, target.TargetCodex),
		},
	})

	payloads, err := PayloadSet(context.Background(), Input{
		Paths:       hostOutputTestPaths(t, root),
		Environment: environment,
		Lockfile:    snapshottest.File(t),
		Selection:   mustHostOutputSelection(t, "codex"),
	})
	if err != nil {
		t.Fatalf("PayloadSet materialized a Skill with no required effect: %v", err)
	}
	if _, ok := payloads.LookupSubject(skillProjectionSubject(t, "unavailable", target.TargetCodex)); ok {
		t.Fatal("PayloadSet emitted an unrequested Skill payload")
	}
}

func TestBuildPayloadSetCleansUpRepairedSkillPayload(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	root := t.TempDir()
	writeHostOutputTestFile(t, root, "skills/review/skill.md", " ---   \ndescription: Demo skill\n---\nBody\n")
	sourceSpec := sourcetest.Local(t, "skills/review", source.LocalSourceModeVendor)
	identity, view := resolvedHostOutputArtifact(t, ctx, root, sourceSpec, "skills/review")
	result, err := skillrepair.Repair(ctx, identity, view, "review", []target.Target{target.TargetCodex})
	if err != nil {
		t.Fatalf("Repair returned error: %v", err)
	}
	locked := lockedSkillResourceFromRecipe(t, "review", result)
	if err := result.Release(); err != nil {
		t.Fatalf("release lock fixture repair: %v", err)
	}
	environment := hostOutputEnvironment(t, desired.Spec{
		Skills: []skillresource.Skill{
			skillResource(t, "review", sourceSpec, target.TargetCodex),
		},
	})
	payloads, err := PayloadSet(ctx, Input{
		Paths:                      hostOutputTestPaths(t, root),
		Environment:                environment,
		Lockfile:                   snapshottest.File(t, locked...),
		Selection:                  mustHostOutputSelection(t, "codex"),
		ManagedPathPayloadSubjects: []topology.SubjectID{locked[1].SubjectID()},
	})
	if err != nil {
		t.Fatalf("PayloadSet returned error: %v", err)
	}

	payload, ok := payloads.LookupSubject(locked[1].SubjectID())
	if !ok {
		t.Fatal("skill payload missing")
	}
	directory, ok := payload.Directory()
	if !ok {
		t.Fatal("Directory returned no directory variant")
	}
	if err := directory.View().Verify(ctx, directory.Identity()); err != nil {
		t.Fatalf("repaired payload is unavailable before cleanup: %v", err)
	}
	cancel()
	if err := payloads.Cleanup(); err != nil {
		t.Fatalf("Cleanup after context cancellation returned error: %v", err)
	}
	if err := directory.View().Verify(context.Background(), directory.Identity()); err == nil {
		t.Fatal("repaired payload remained available after cleanup")
	}
}

func TestBuildPayloadSetCleansRepairedSkillWhenLaterPayloadValidationFails(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	temporaryRoot := t.TempDir()
	t.Setenv("TMPDIR", temporaryRoot)
	writeHostOutputTestFile(t, root, "skills/review/skill.md", " ---   \ndescription: Demo skill\n---\nBody\n")
	sourceSpec := sourcetest.Local(t, "skills/review", source.LocalSourceModeVendor)
	identity, view := resolvedHostOutputArtifact(t, ctx, root, sourceSpec, "skills/review")
	result, err := skillrepair.Repair(ctx, identity, view, "review", []target.Target{target.TargetCodex})
	if err != nil {
		t.Fatalf("Repair returned error: %v", err)
	}
	locked := lockedSkillResourceFromRecipe(t, "review", result)
	if err := result.Release(); err != nil {
		t.Fatalf("release lock fixture repair: %v", err)
	}
	environment := hostOutputEnvironment(t, desired.Spec{
		Skills: []skillresource.Skill{
			skillResource(t, "review", sourceSpec, target.TargetCodex),
		},
	})
	asset := hookAssetResource(t, "cleanup-trigger", sourcetest.Local(t, "hooks/cleanup-trigger.sh", source.LocalSourceModeVendor))
	assetSubject, subjectErr := topologyhook.AssetSubjectID(asset.ID(), asset.Scope())
	if subjectErr != nil {
		t.Fatalf("build HookAsset subject: %v", subjectErr)
	}
	environment = hostOutputEnvironment(t, desired.Spec{
		Skills:     environment.Skills(),
		HookAssets: []hookassetresource.HookAsset{asset},
	})

	_, err = PayloadSet(ctx, Input{
		Paths:       hostOutputTestPaths(t, root),
		Environment: environment,
		Lockfile:    snapshottest.File(t, locked...),
		Selection:   mustHostOutputSelection(t, "codex"),
		ManagedPathPayloadSubjects: []topology.SubjectID{
			locked[1].SubjectID(),
			assetSubject,
		},
	})
	if err == nil || !strings.Contains(err.Error(), `missing locked path projection`) {
		t.Fatalf("PayloadSet error = %v, want later HookAsset validation failure", err)
	}
	entries, readErr := os.ReadDir(temporaryRoot)
	if readErr != nil {
		t.Fatalf("read temporary root: %v", readErr)
	}
	if len(entries) != 0 {
		t.Fatalf("temporary entries after failed PayloadSet = %v, want cleanup", entries)
	}
}

func TestBuildPayloadSetCleansRepairedSkillWhenLaterSkillLockIsMissing(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	temporaryRoot := t.TempDir()
	t.Setenv("TMPDIR", temporaryRoot)
	writeHostOutputTestFile(t, root, "skills/review/skill.md", " ---   \ndescription: Demo skill\n---\nBody\n")
	reviewSource := sourcetest.Local(t, "skills/review", source.LocalSourceModeVendor)
	missingSource := sourcetest.Local(t, "skills/missing", source.LocalSourceModeVendor)
	identity, view := resolvedHostOutputArtifact(t, ctx, root, reviewSource, "skills/review")
	repairResult, err := skillrepair.Repair(ctx, identity, view, "review", []target.Target{target.TargetCodex})
	if err != nil {
		t.Fatalf("Repair returned error: %v", err)
	}
	locked := lockedSkillResourceFromRecipe(t, "review", repairResult)
	if err := repairResult.Release(); err != nil {
		t.Fatalf("release lock fixture repair: %v", err)
	}
	environment := hostOutputEnvironment(t, desired.Spec{
		Skills: []skillresource.Skill{
			skillResource(t, "review", reviewSource, target.TargetCodex),
			skillResource(t, "missing", missingSource, target.TargetCodex),
		},
	})

	_, err = PayloadSet(ctx, Input{
		Paths:       hostOutputTestPaths(t, root),
		Environment: environment,
		Lockfile:    snapshottest.File(t, locked...),
		Selection:   mustHostOutputSelection(t, "codex"),
		ManagedPathPayloadSubjects: []topology.SubjectID{
			locked[1].SubjectID(),
			skillProjectionSubject(t, "missing", target.TargetCodex),
		},
	})
	if err == nil || !strings.Contains(err.Error(), `skill "missing": missing lockfile entry`) {
		t.Fatalf("PayloadSet error = %v, want missing later skill lock", err)
	}
	entries, readErr := os.ReadDir(temporaryRoot)
	if readErr != nil {
		t.Fatalf("read temporary root: %v", readErr)
	}
	if len(entries) != 0 {
		t.Fatalf("temporary entries after failed later skill = %v, want cleanup", entries)
	}
}

func TestRunCleanupsJoinsFailuresInReverseOrder(t *testing.T) {
	firstFailure := errors.New("first cleanup")
	secondFailure := errors.New("second cleanup")
	var order []string
	err := runCleanups([]func() error{
		func() error {
			order = append(order, "first")
			return firstFailure
		},
		nil,
		func() error {
			order = append(order, "second")
			return secondFailure
		},
	})
	if !errors.Is(err, firstFailure) || !errors.Is(err, secondFailure) {
		t.Fatalf("runCleanups error = %v, want both failures", err)
	}
	if len(order) != 2 || order[0] != "second" || order[1] != "first" {
		t.Fatalf("cleanup order = %v, want [second first]", order)
	}
}

func TestBuildPayloadSetSkipsUnselectedInstructionTargets(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	writeHostOutputTestFile(t, root, "instructions/codex.md", "codex rules\n")
	writeHostOutputTestFile(t, root, "instructions/claude.md", "claude rules\n")
	codexSource := sourcetest.Local(t, "instructions/codex.md", source.LocalSourceModeVendor)
	claudeSource := sourcetest.Local(t, "instructions/claude.md", source.LocalSourceModeVendor)
	codexLocked := lockedInstructionsForPath(t, ctx, root, "codex", codexSource, "instructions/codex.md", target.TargetCodex)
	claudeLocked := lockedInstructionsForPath(t, ctx, root, "claude", claudeSource, "instructions/claude.md", target.TargetClaudeCode)
	locked := append(codexLocked, claudeLocked...)
	environment := hostOutputEnvironment(t, desired.Spec{
		Instructions: []instructionsresource.Instructions{
			instructionResource(t, "codex", codexSource, target.TargetCodex),
			instructionResource(t, "claude", claudeSource, target.TargetClaudeCode),
		},
	})

	payloads, err := PayloadSet(ctx, Input{
		Paths:                      hostOutputTestPaths(t, root),
		Environment:                environment,
		Lockfile:                   snapshottest.File(t, locked...),
		Selection:                  mustHostOutputSelection(t, "codex"),
		ManagedPathPayloadSubjects: []topology.SubjectID{locked[1].SubjectID()},
	})
	if err != nil {
		t.Fatalf("PayloadSet returned error: %v", err)
	}
	if _, ok := payloads.LookupSubject(locked[1].SubjectID()); !ok {
		t.Fatal("selected codex instruction payload was not materialized")
	}
	if _, ok := payloads.LookupSubject(claudeLocked[1].SubjectID()); ok {
		t.Fatal("unselected claude instruction payload was materialized")
	}
}
