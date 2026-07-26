package journal

import (
	"testing"

	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/realization"
	"github.com/isty2e/daem/internal/target"
)

func TestExpectedPathStateFromMutationPreservesExplicitModeZero(t *testing.T) {
	action := pathMutation{
		Subject:         testManagedPathSubject(t, "mode-zero"),
		ConsumerTargets: []target.Target{target.TargetCodex},
		Destination:     "AGENTS.md",
		DesiredHash:     testContentHash("after"),
		ExpectedExists:  true,
		ContentKind:     realization.PathProjectionFile,
	}

	expected, err := expectedPathStateFromMutation(action)
	if err != nil {
		t.Fatalf("expectedPathStateFromMutation returned error: %v", err)
	}
	if !expected.Existed || expected.Kind != recovery.PathKindFile || expected.PathMode == nil || expected.PathMode.FileMode() != 0 {
		t.Fatalf("expected state = %#v, want existing file with explicit mode 0000", expected)
	}
}

func TestExpectedPathStateFromMutationKeepsProjectionAndDocumentExistenceIndependent(t *testing.T) {
	action := pathMutation{
		Subject:            testManagedPathSubject(t, "removed-projection"),
		Target:             target.TargetClaudeCode,
		Destination:        ".claude.json",
		ContentPath:        "/mcpServers/removed",
		ExpectedExists:     false,
		ExpectedPathExists: true,
		ExpectedPathMode:   0o600,
		StateIndependent:   true,
	}

	expected, err := expectedPathStateFromMutation(action)
	if err != nil {
		t.Fatalf("expectedPathStateFromMutation returned error: %v", err)
	}
	if expected.Existed || !expected.PathExisted || expected.PathMode == nil || expected.PathMode.FileMode() != 0o600 {
		t.Fatalf("expected state = %#v, want absent projection in existing mode-0600 document", expected)
	}
}

func TestCaptureRecoveryExpectedAfterManagedRemovalIsAbsentAndUnmanaged(t *testing.T) {
	action := pathMutation{
		Kind:               pathMutationRemove,
		Subject:            testManagedPathSubject(t, "removed"),
		ConsumerTargets:    []target.Target{target.TargetCodex},
		Destination:        ".agents/skills/removed",
		DesiredHash:        testContentHash("before"),
		ExpectedExists:     false,
		ExpectedPathExists: false,
		ContentKind:        realization.PathProjectionDirectory,
	}

	expected, membership, err := captureRecoveryExpectedAfter(action)
	if err != nil {
		t.Fatalf("captureRecoveryExpectedAfter returned error: %v", err)
	}
	if expected.Existed || expected.PathMode != nil || membership.Managed || membership.ContentHash != "" {
		t.Fatalf("expected/membership = %#v/%#v, want exact absent unmanaged state", expected, membership)
	}
}
