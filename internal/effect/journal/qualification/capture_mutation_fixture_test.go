package qualification_test

import (
	"os"
	"testing"

	journalrecord "github.com/isty2e/daem/internal/effect/journal/record"

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

func normalizedPathCapture(t *testing.T, kind journalrecord.CaptureKind, scope target.Scope, destination output.Destination, liveHash artifact.ContentHash, contentKind realization.PathProjectionContentKind) journalrecord.CaptureMutation {
	t.Helper()
	subject := testManagedPathSubject(t, "capture")
	consumers := []target.Target{target.TargetCodex}
	exists := kind != journalrecord.CaptureCreate
	mode, permissions := os.FileMode(0), realization.PathPermissionsNone
	if contentKind == realization.PathProjectionFile {
		mode, permissions = 0o600, realization.PathPermissionsExact
	}
	var mutation journalrecord.ManagedPathMutation
	var err error
	if !exists {
		mutation, err = journalrecord.NewManagedPathCreateMutation(subject, consumers, scope, destination, testContentHash("capture expected"), contentKind, mode, nil)
	} else {
		previous, previousErr := durable.NewManagedPathState(subject, consumers, scope, destination, liveHash, contentKind, permissions, mode)
		if previousErr != nil {
			t.Fatal(previousErr)
		}
		switch kind {
		case journalrecord.CaptureReplace:
			mutation, err = journalrecord.NewManagedPathReplaceMutation(subject, consumers, scope, destination, liveHash, liveHash, contentKind, mode, previous)
		case journalrecord.CaptureRemove:
			mutation, err = journalrecord.NewManagedPathRemoveMutation(previous, liveHash)
		case journalrecord.CaptureRecord:
			mutation, err = journalrecord.NewManagedPathRecordMutation(subject, consumers, scope, destination, liveHash, liveHash, contentKind, mode, &previous)
		default:
			t.Fatalf("unsupported capture fixture kind %q", kind)
		}
	}
	if err != nil {
		t.Fatal(err)
	}
	evidence := testManagedPathEvidence(t, subject, destination, exists, liveHash)
	values, err := journalrecord.NormalizeCaptureMutations([]journalrecord.ManagedPathMutation{mutation}, nil, []observe.ManagedPathEvidence{evidence})
	if err != nil || len(values) != 1 {
		t.Fatal("normalize managed capture fixture", err)
	}
	return values[0]
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
