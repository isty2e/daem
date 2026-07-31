package cli_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/observe"
	"github.com/isty2e/daem/internal/assurance/statefile"
	"github.com/isty2e/daem/internal/effect/journal"
	"github.com/isty2e/daem/internal/output/hostpath"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/realization"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
	"github.com/isty2e/daem/test/outputtest"
	"github.com/isty2e/daem/test/testkit"
)

func captureCLIRecoveryUpdateJournal(t *testing.T, manifestPath string) (daempaths.Paths, durable.Snapshot, durable.Snapshot, string, string) {
	t.Helper()

	root := filepath.Dir(manifestPath)
	testkit.WriteFile(t, root, "AGENTS.md", "old instructions\n")
	oldHash := testkit.HashPath(t, filepath.Join(root, "AGENTS.md"))
	testkit.WriteFile(t, root, "AGENTS.md", "old instructions\n")
	testkit.WriteFile(t, root, "desired.md", "new instructions\n")
	newHash := testkit.HashPath(t, filepath.Join(root, "desired.md"))

	paths, err := daempaths.Resolve(manifestPath)
	if err != nil {
		t.Fatalf("daempaths.Resolve returned error: %v", err)
	}
	currentState := testkit.Snapshot(
		t,
		testkit.InstructionPathState(t, "project", []string{"codex"}, "project", "AGENTS.md", oldHash),
	)
	nextState := testkit.Snapshot(
		t,
		testkit.InstructionPathState(t, "project", []string{"codex"}, "project", "AGENTS.md", newHash),
	)
	previous := singleCLIManagedPath(t, currentState)
	mutation, err := journal.NewManagedPathReplaceMutation(
		previous.Subject(),
		[]target.Target{target.TargetCodex},
		target.ScopeProject,
		outputtest.Parse(t, "AGENTS.md"),
		artifact.ContentHash(newHash),
		artifact.ContentHash(oldHash),
		realization.PathProjectionFile,
		0o600,
		previous,
	)
	if err != nil {
		t.Fatalf("NewManagedPathReplaceMutation returned error: %v", err)
	}
	evidence := managedInstructionEvidence(t, previous.Subject(), true, oldHash, 0o600)
	captureCLIManagedPathJournal(t, paths, mutation, evidence, currentState, nextState)

	return paths, currentState, nextState, oldHash, newHash
}

func captureCLIRecoveryCreateJournal(t *testing.T, manifestPath string) (daempaths.Paths, durable.Snapshot, durable.Snapshot, string) {
	t.Helper()

	root := filepath.Dir(manifestPath)
	testkit.WriteFile(t, root, "desired.md", "new instructions\n")
	newHash := testkit.HashPath(t, filepath.Join(root, "desired.md"))
	paths, err := daempaths.Resolve(manifestPath)
	if err != nil {
		t.Fatalf("daempaths.Resolve returned error: %v", err)
	}
	currentState := durable.EmptySnapshot()
	nextState := testkit.Snapshot(
		t,
		testkit.InstructionPathState(t, "project", []string{"codex"}, "project", "AGENTS.md", newHash),
	)
	desired := singleCLIManagedPath(t, nextState)
	mutation, err := journal.NewManagedPathCreateMutation(
		desired.Subject(),
		[]target.Target{target.TargetCodex},
		target.ScopeProject,
		outputtest.Parse(t, "AGENTS.md"),
		artifact.ContentHash(newHash),
		realization.PathProjectionFile,
		0o600,
		nil,
	)
	if err != nil {
		t.Fatalf("NewManagedPathCreateMutation returned error: %v", err)
	}
	evidence := managedInstructionEvidence(t, desired.Subject(), false, "", 0)
	captureCLIManagedPathJournal(t, paths, mutation, evidence, currentState, nextState)

	return paths, currentState, nextState, newHash
}

func captureCLIRecoveryDeleteJournal(t *testing.T, manifestPath string) (daempaths.Paths, durable.Snapshot, durable.Snapshot, string) {
	t.Helper()

	root := filepath.Dir(manifestPath)
	testkit.WriteFile(t, root, "AGENTS.md", "old instructions\n")
	oldHash := testkit.HashPath(t, filepath.Join(root, "AGENTS.md"))
	paths, err := daempaths.Resolve(manifestPath)
	if err != nil {
		t.Fatalf("daempaths.Resolve returned error: %v", err)
	}
	currentState := testkit.Snapshot(
		t,
		testkit.InstructionPathState(t, "project", []string{"codex"}, "project", "AGENTS.md", oldHash),
	)
	nextState := durable.EmptySnapshot()
	previous := singleCLIManagedPath(t, currentState)
	mutation, err := journal.NewManagedPathRemoveMutation(previous, artifact.ContentHash(oldHash))
	if err != nil {
		t.Fatalf("NewManagedPathRemoveMutation returned error: %v", err)
	}
	evidence := managedInstructionEvidence(t, previous.Subject(), true, oldHash, 0o600)
	captureCLIManagedPathJournal(t, paths, mutation, evidence, currentState, nextState)

	return paths, currentState, nextState, oldHash
}

func singleCLIManagedPath(t *testing.T, snapshot durable.Snapshot) durable.ManagedPathState {
	t.Helper()
	states := snapshot.ManagedPaths()
	if len(states) != 1 {
		t.Fatalf("managed paths = %#v, want exactly one", states)
	}
	return states[0]
}

func managedInstructionEvidence(
	t *testing.T,
	subject topology.SubjectID,
	exists bool,
	contentHash string,
	mode os.FileMode,
) observe.ManagedPathEvidence {
	t.Helper()
	evidence, err := observe.NewManagedPathEvidence(
		subject,
		outputtest.Parse(t, "AGENTS.md"),
		exists,
		artifact.ContentHash(contentHash),
		mode,
	)
	if err != nil {
		t.Fatalf("NewManagedPathEvidence returned error: %v", err)
	}
	return evidence
}

func captureCLIManagedPathJournal(
	t *testing.T,
	paths daempaths.Paths,
	mutation journal.ManagedPathMutation,
	evidence observe.ManagedPathEvidence,
	currentState durable.Snapshot,
	nextState durable.Snapshot,
) {
	t.Helper()
	_, err := journal.CaptureJournalWithOptions(
		context.Background(),
		recoveryJournalPaths(paths),
		"20260621T120000.000000000Z-apply",
		time.Date(2026, 6, 21, 12, 0, 0, 0, time.UTC),
		currentState,
		nextState,
		journal.CaptureOptions{
			Filesystem:           testFilesystem(),
			ManagedPathMutations: []journal.ManagedPathMutation{mutation},
			ManagedPathEvidence:  []observe.ManagedPathEvidence{evidence},
			Resolver:             hostpath.NewResolver(paths.ManifestRoot).Resolve,
			StateCodec:           statefile.Codec{},
		},
	)
	if err != nil {
		t.Fatalf("journal.CaptureJournalWithOptions returned error: %v", err)
	}
}

func captureCLIRecoverySkillUpdateJournal(t *testing.T, manifestPath string) (daempaths.Paths, durable.Snapshot, durable.Snapshot, string, string) {
	t.Helper()

	root := filepath.Dir(manifestPath)
	testkit.WriteFile(t, root, ".agents/skills/oracle/SKILL.md", "---\nname: oracle\nversion: old\n---\n")
	testkit.WriteFile(t, root, "desired/oracle/SKILL.md", "---\nname: oracle\nversion: new\n---\n")
	oldHash := testkit.HashDirectory(t, filepath.Join(root, ".agents/skills/oracle"))
	newHash := testkit.HashDirectory(t, filepath.Join(root, "desired/oracle"))
	paths, err := daempaths.Resolve(manifestPath)
	if err != nil {
		t.Fatalf("daempaths.Resolve returned error: %v", err)
	}
	currentState := testkit.Snapshot(
		t,
		testkit.SkillPathState(t, "oracle", []string{"codex"}, "project", ".agents/skills/oracle", oldHash),
	)
	nextState := testkit.Snapshot(
		t,
		testkit.SkillPathState(t, "oracle", []string{"codex"}, "project", ".agents/skills/oracle", newHash),
	)
	subject := singleCLIManagedPath(t, currentState).Subject()
	previous, err := durable.NewManagedPathState(
		subject,
		[]target.Target{target.TargetCodex},
		target.ScopeProject,
		outputtest.Parse(t, ".agents/skills/oracle"),
		artifact.ContentHash(oldHash),
		realization.PathProjectionDirectory,
		realization.PathPermissionsNone,
		0,
	)
	if err != nil {
		t.Fatalf("NewManagedPathState returned error: %v", err)
	}
	mutation, err := journal.NewManagedPathReplaceMutation(
		subject,
		[]target.Target{target.TargetCodex},
		target.ScopeProject,
		outputtest.Parse(t, ".agents/skills/oracle"),
		artifact.ContentHash(newHash),
		artifact.ContentHash(oldHash),
		realization.PathProjectionDirectory,
		0,
		previous,
	)
	if err != nil {
		t.Fatalf("NewManagedPathReplaceMutation returned error: %v", err)
	}
	evidence, err := observe.NewManagedPathEvidence(
		subject,
		outputtest.Parse(t, ".agents/skills/oracle"),
		true,
		artifact.ContentHash(oldHash),
		0,
	)
	if err != nil {
		t.Fatalf("NewManagedPathEvidence returned error: %v", err)
	}

	_, err = journal.CaptureJournalWithOptions(
		context.Background(),
		recoveryJournalPaths(paths),
		"20260621T120000.000000000Z-apply",
		time.Date(2026, 6, 21, 12, 0, 0, 0, time.UTC),
		currentState,
		nextState,
		journal.CaptureOptions{
			Filesystem:           testFilesystem(),
			ManagedPathMutations: []journal.ManagedPathMutation{mutation},
			ManagedPathEvidence:  []observe.ManagedPathEvidence{evidence},
			Resolver:             hostpath.NewResolver(paths.ManifestRoot).Resolve,
			StateCodec:           statefile.Codec{},
		},
	)
	if err != nil {
		t.Fatalf("journal.CaptureJournalWithOptions returned error: %v", err)
	}

	return paths, currentState, nextState, oldHash, newHash
}
