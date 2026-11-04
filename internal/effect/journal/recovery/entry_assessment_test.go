package recovery

import (
	"testing"

	"github.com/isty2e/daem/internal/target"
)

func TestEntryAssessmentPreservesBothMatchesAndIndependentBackupValidation(t *testing.T) {
	before := BeforePathState{
		Existed: true, Kind: PathKindFile,
		ContentHash: "sha256:same", BackupPath: "files/000001", PathMode: testPermissionMode(0o640),
	}
	entry, err := NewEntry(testSubject(t), target.TargetCodex, nil, target.ScopeProject, "AGENTS.md", "", "", before,
		ExpectedPathState{Existed: true, Kind: PathKindFile, ContentHash: before.ContentHash, PathMode: before.PathMode}, nil)
	if err != nil {
		t.Fatal(err)
	}
	evidence := []PathEvidence{{Path: "AGENTS.md", Exists: true, PathExisted: true, Kind: PathKindFile, ContentHash: before.ContentHash, PathMode: before.PathMode}}
	backup := []BackupEvidence{{BackupPath: before.BackupPath, Exists: true, Kind: PathKindFile, ContentHash: before.ContentHash}}
	assessed, err := AssessEntries([]Entry{entry}, evidence, backup)
	if err != nil || !assessed.MatchesBefore() || !assessed.MatchesExpected() || !assessed.Recoverable() {
		t.Fatalf("equal host states lost a phase match: %+v %v", assessed, err)
	}
	actions := assessed.Actions()
	if len(actions) != 1 || actions[0].Kind != ActionKindNoOp {
		t.Fatalf("Before action precedence changed: %+v", actions)
	}
	actions[0].Destination = "foreign"
	if assessed.Actions()[0].Destination != "AGENTS.md" {
		t.Fatal("entry assessment exposed mutable action state")
	}
	if err := ValidateBackups([]Entry{entry}, backup); err != nil {
		t.Fatal(err)
	}
	backup[0].ContentHash = "sha256:corrupt"
	assessed, err = AssessEntries([]Entry{entry}, evidence, backup)
	if err != nil || !assessed.MatchesBefore() || !assessed.Recoverable() {
		t.Fatal("factoring changed the ordinary already-Before rule", err)
	}
	if err := ValidateBackups([]Entry{entry}, backup); err == nil {
		t.Fatal("complete artifact validation skipped an already-Before entry")
	}
}

func TestEntryAssessmentEmptyIsObservedRatherThanZeroValue(t *testing.T) {
	assessed, err := AssessEntries(nil, nil, nil)
	if err != nil || !assessed.MatchesBefore() || !assessed.MatchesExpected() || !assessed.Recoverable() || len(assessed.Actions()) != 0 {
		t.Fatal("an actual empty entry census lost its identity", err)
	}
	var unknown EntryAssessment
	if unknown.MatchesBefore() || unknown.MatchesExpected() || unknown.Recoverable() {
		t.Fatal("an unknown census was treated as observed empty evidence")
	}
}
