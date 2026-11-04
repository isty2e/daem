package journal

import (
	"os"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/observe"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/target"
)

func normalizedPathCapture(t *testing.T, kind pathMutationKind, scope target.Scope, destination output.Destination, liveHash artifact.ContentHash, contentKind realization.PathProjectionContentKind) captureMutation {
	t.Helper()
	subject := testManagedPathSubject(t, "capture")
	consumers := []target.Target{target.TargetCodex}
	exists := kind != pathMutationCreate
	mode, permissions := os.FileMode(0), realization.PathPermissionsNone
	if contentKind == realization.PathProjectionFile {
		mode, permissions = 0o600, realization.PathPermissionsExact
	}
	var mutation ManagedPathMutation
	var err error
	if !exists {
		mutation, err = NewManagedPathCreateMutation(subject, consumers, scope, destination, testContentHash("capture expected"), contentKind, mode, nil)
	} else {
		previous, previousErr := durable.NewManagedPathState(subject, consumers, scope, destination, liveHash, contentKind, permissions, mode)
		if previousErr != nil {
			t.Fatal(previousErr)
		}
		switch kind {
		case pathMutationReplace:
			mutation, err = NewManagedPathReplaceMutation(subject, consumers, scope, destination, liveHash, liveHash, contentKind, mode, previous)
		case pathMutationRemove:
			mutation, err = NewManagedPathRemoveMutation(previous, liveHash)
		case pathMutationRecord:
			mutation, err = NewManagedPathRecordMutation(subject, consumers, scope, destination, liveHash, liveHash, contentKind, mode, &previous)
		default:
			t.Fatalf("unsupported capture fixture kind %q", kind)
		}
	}
	if err != nil {
		t.Fatal(err)
	}
	evidence := testManagedPathEvidence(t, subject, destination, exists, liveHash)
	values, err := pathMutations([]ManagedPathMutation{mutation}, nil, []observe.ManagedPathEvidence{evidence})
	if err != nil || len(values) != 1 {
		t.Fatal("normalize managed capture fixture", err)
	}
	return values[0]
}
