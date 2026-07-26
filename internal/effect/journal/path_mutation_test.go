package journal

import (
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/observe"
	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
	topologyprojection "github.com/isty2e/daem/internal/topology/projection"
)

func TestManagedPathEvidenceCoverageRequiresExactCurrentFact(t *testing.T) {
	subject := testManagedPathSubject(t, "oracle")
	destination := output.Destination(".agents/skills/oracle")
	mutation, err := NewManagedPathCreateMutation(
		subject,
		[]target.Target{target.TargetCodex},
		target.ScopeProject,
		destination,
		testContentHash("desired"),
		realization.PathProjectionDirectory,
		0,
		nil,
	)
	if err != nil {
		t.Fatalf("NewManagedPathCreateMutation returned error: %v", err)
	}
	absent := testManagedPathEvidence(t, subject, destination, false, "")
	if _, err := managedPathEvidenceByKey([]ManagedPathMutation{mutation}, []observe.ManagedPathEvidence{absent}); err != nil {
		t.Fatalf("valid absent evidence rejected: %v", err)
	}
	paths, err := pathMutations(
		[]ManagedPathMutation{mutation},
		nil,
		[]observe.ManagedPathEvidence{absent},
	)
	if err != nil {
		t.Fatalf("pathMutations returned error: %v", err)
	}
	if len(paths) != 1 || paths[0].LiveExists || paths[0].LivePathExists || paths[0].LiveHash != "" || paths[0].LivePathHash != "" {
		t.Fatalf("create before facts = %#v, want exact absent evidence", paths)
	}

	existing := testManagedPathEvidence(t, subject, destination, true, testContentHash("foreign"))
	if _, err := managedPathEvidenceByKey([]ManagedPathMutation{mutation}, []observe.ManagedPathEvidence{existing}); err == nil || !strings.Contains(err.Error(), "expected absent") {
		t.Fatalf("existing create evidence error = %v, want absence rejection", err)
	}
}

func TestManagedPathEvidenceCoverageRejectsWrongSubjectStaleHashAndDuplicates(t *testing.T) {
	subject := testManagedPathSubject(t, "oracle")
	other := testManagedPathSubject(t, "review")
	destination := output.Destination(".agents/skills/oracle")
	previous, err := durable.NewManagedPathState(
		subject,
		[]target.Target{target.TargetCodex},
		target.ScopeProject,
		destination,
		testContentHash("old"),
		realization.PathProjectionDirectory,
		realization.PathPermissionsNone,
		0,
	)
	if err != nil {
		t.Fatalf("NewManagedPathState returned error: %v", err)
	}
	mutation, err := NewManagedPathRecordMutation(
		subject,
		[]target.Target{target.TargetCodex},
		target.ScopeProject,
		destination,
		testContentHash("old"),
		testContentHash("old"),
		realization.PathProjectionDirectory,
		0,
		&previous,
	)
	if err != nil {
		t.Fatalf("NewManagedPathRecordMutation returned error: %v", err)
	}

	wrongSubject := testManagedPathEvidence(t, other, destination, true, testContentHash("old"))
	if _, err := managedPathEvidenceByKey([]ManagedPathMutation{mutation}, []observe.ManagedPathEvidence{wrongSubject}); err == nil || !strings.Contains(err.Error(), "exact subject/address") {
		t.Fatalf("wrong-subject error = %v, want exact correlation rejection", err)
	}
	stale := testManagedPathEvidence(t, subject, destination, true, testContentHash("stale"))
	if _, err := managedPathEvidenceByKey([]ManagedPathMutation{mutation}, []observe.ManagedPathEvidence{stale}); err == nil || !strings.Contains(err.Error(), "does not match live hash") {
		t.Fatalf("stale-hash error = %v, want live hash rejection", err)
	}
	exact := testManagedPathEvidence(t, subject, destination, true, testContentHash("old"))
	if _, err := managedPathEvidenceByKey([]ManagedPathMutation{mutation}, []observe.ManagedPathEvidence{exact, exact}); err == nil || !strings.Contains(err.Error(), "duplicates") {
		t.Fatalf("duplicate evidence error = %v, want duplicate rejection", err)
	}
	paths, err := pathMutations(
		[]ManagedPathMutation{mutation},
		nil,
		[]observe.ManagedPathEvidence{exact},
	)
	if err != nil {
		t.Fatalf("pathMutations returned error: %v", err)
	}
	if len(paths) != 1 || !paths[0].LiveExists || !paths[0].LivePathExists ||
		paths[0].LiveHash != testContentHash("old") || paths[0].LivePathHash != testContentHash("old") {
		t.Fatalf("record before facts = %#v, want exact current evidence", paths)
	}
}

func TestManagedPathMutationCopiesPreviousState(t *testing.T) {
	subject := testManagedPathSubject(t, "oracle")
	destination := output.Destination(".agents/skills/oracle")
	previous, err := durable.NewManagedPathState(
		subject,
		[]target.Target{target.TargetCodex},
		target.ScopeProject,
		destination,
		testContentHash("old"),
		realization.PathProjectionDirectory,
		realization.PathPermissionsNone,
		0,
	)
	if err != nil {
		t.Fatal(err)
	}
	mutation, err := NewManagedPathRecordMutation(
		subject,
		[]target.Target{target.TargetCodex},
		target.ScopeProject,
		destination,
		testContentHash("old"),
		testContentHash("old"),
		realization.PathProjectionDirectory,
		0,
		&previous,
	)
	if err != nil {
		t.Fatal(err)
	}
	previous = durable.ManagedPathState{}

	stored := mutation.facts().previous
	if stored == nil || stored.Subject() != subject || stored.Destination() != destination {
		t.Fatalf("stored previous state = %#v, want constructor-time value", stored)
	}
}

func testManagedPathSubject(t *testing.T, name string) topology.SubjectID {
	t.Helper()
	id, err := entity.New(entity.KindSkill, name)
	if err != nil {
		t.Fatalf("entity.New returned error: %v", err)
	}
	subject, err := topologyprojection.Subject(id, "skill.project.agents")
	if err != nil {
		t.Fatalf("projection.Subject returned error: %v", err)
	}
	return subject
}

func testManagedPathEvidence(
	t *testing.T,
	subject topology.SubjectID,
	destination output.Destination,
	exists bool,
	contentHash artifact.ContentHash,
) observe.ManagedPathEvidence {
	t.Helper()
	evidence, err := observe.NewManagedPathEvidence(subject, destination, exists, contentHash, 0)
	if err != nil {
		t.Fatalf("NewManagedPathEvidence returned error: %v", err)
	}
	return evidence
}
