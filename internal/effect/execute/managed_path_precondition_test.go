package execute

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/realization"
	"github.com/isty2e/daem/internal/supply/artifact"
)

func TestManagedPathFileModeExpectationDistinguishesAbsentFromExplicitZero(t *testing.T) {
	actual := os.FileMode(0o600)
	explicitZero := os.FileMode(0)
	if !managedPathFileModeMatches(actual, nil) {
		t.Fatal("unmodeled permission unexpectedly rejected an observed mode")
	}
	if managedPathFileModeMatches(actual, &explicitZero) {
		t.Fatal("explicit mode zero collapsed into an unmodeled permission")
	}
	if !managedPathFileModeMatches(0, &explicitZero) {
		t.Fatal("explicit mode zero did not match itself")
	}
}

func TestManagedPathPreconditionRejectsInvalidHashBeforeAuthorityUse(t *testing.T) {
	destination := mutationDestination{logical: "AGENTS.md"}
	if _, err := captureRootedManagedPathPrecondition(
		context.Background(),
		nil,
		destination,
		true,
		artifact.ContentHash("sha256:short"),
		realization.PathProjectionFile,
		nil,
	); err == nil || !strings.Contains(err.Error(), "lowercase SHA-256 digest") {
		t.Fatalf("malformed present hash error = %v", err)
	}
	if _, err := captureRootedManagedPathPrecondition(
		context.Background(),
		nil,
		destination,
		false,
		testArtifactHash("unexpected"),
		realization.PathProjectionFile,
		nil,
	); err == nil || !strings.Contains(err.Error(), "must not carry an expected hash") {
		t.Fatalf("absent hash error = %v", err)
	}
}
