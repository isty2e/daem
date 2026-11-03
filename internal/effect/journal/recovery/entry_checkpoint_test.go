package recovery

import (
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/target"
)

func TestEntryAssessmentRequiresQualifiedReplacementVacancy(t *testing.T) {
	intent := testRemovalIntent(t)
	before := BeforePathState{Existed: true, Kind: PathKindFile, ContentHash: "sha256:" + strings.Repeat("3", 64), BackupPath: "files/000001", PathMode: testPermissionMode(0o600)}
	entry, err := NewEntry(testSubject(t), target.TargetCodex, nil, target.ScopeProject, intent.Destination().String(), "", "", before, ExpectedPathState{Existed: true, Kind: PathKindFile, ContentHash: "sha256:" + strings.Repeat("4", 64), PathMode: before.PathMode}, nil)
	if err != nil {
		t.Fatal(err)
	}
	absent, err := NewRemovalResidueEntryObservation(RemovalResidueEntryAbsent, "", "", nil, "", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"qualified absence", "missing checkpoint", "namespace changed", "other destination", "present third value", "corrupt backup"} {
		t.Run(mode, func(t *testing.T) {
			status := RemovalNamespaceMatched
			if mode == "namespace changed" {
				status = RemovalNamespaceChanged
			}
			detail := ""
			if status != RemovalNamespaceMatched {
				detail = "parent changed"
			}
			namespace, err := NewRemovalNamespaceObservation(status, detail)
			if err != nil {
				t.Fatal(err)
			}
			checkpoint, err := intent.AssessCleanup(namespace, NewRemovalResidueObservation(absent, absent))
			if err != nil {
				t.Fatal(err)
			}
			checkpoints := []RemovalCleanupObligation{checkpoint}
			if mode == "missing checkpoint" {
				checkpoints = nil
			}
			path := PathEvidence{Path: entry.destination.String()}
			if mode == "other destination" {
				path.Path = "other.md"
			}
			if mode == "present third value" {
				path.Exists, path.PathExisted, path.Kind, path.ContentHash = true, true, PathKindFile, "sha256:third"
				path.PathMode = before.PathMode
			}
			backup := BackupEvidence{BackupPath: before.BackupPath, Exists: true, Kind: PathKindFile, ContentHash: before.ContentHash}
			if mode == "corrupt backup" {
				backup.ContentHash = "sha256:corrupt"
			}
			assessed, err := AssessEntriesWithRemovalCleanup([]Entry{entry}, []PathEvidence{path}, []BackupEvidence{backup}, checkpoints)
			if err != nil {
				t.Fatal(err)
			}
			if assessed.Recoverable() != (mode == "qualified absence") || assessed.MatchesBefore() || assessed.MatchesExpected() {
				t.Fatalf("invalid checkpoint facts: recoverable=%t Before=%t expected=%t", assessed.Recoverable(), assessed.MatchesBefore(), assessed.MatchesExpected())
			}
			if mode == "qualified absence" && (assessed.Actions()[0].Kind != ActionKindRestoreWrite || !assessed.Actions()[0].ExpectedRestorationInput().Equal(ExpectedPathState{}) || !assessed.Actions()[0].ExpectedAfter.Existed) {
				t.Fatal("qualified vacancy lost its restoration input or durable expected image")
			}
			legacy, err := AssessEntries([]Entry{entry}, []PathEvidence{path}, []BackupEvidence{backup})
			if err != nil || legacy.Recoverable() {
				t.Fatal("ordinary assessment gained implicit vacancy authority", err)
			}
		})
	}
}
