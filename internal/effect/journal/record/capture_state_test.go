package record

import (
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/test/outputtest"
)

func TestCaptureRecoveryStateBeforeUsesPreviousStateIdentity(t *testing.T) {
	sharedHash := testContentHashString("shared")
	previousState := resourceState(defaultRecoveryEntry(), sharedHash)
	action := pathMutationFromManagedState(t, previousState)
	action.Subject = mustTestManagedPathSubject("relocated", "instructions.project.claude")
	action.ConsumerTargets = []target.Target{target.TargetClaudeCode}
	action.Destination = outputtest.Parse(t, "CLAUDE.md")
	state := recoveryManagedStateMap(previousState)

	got, err := captureRecoveryStateBefore(action, state)
	if err != nil {
		t.Fatalf("captureRecoveryStateBefore returned error: %v", err)
	}
	if !got.Managed || got.ContentHash != sharedHash {
		t.Fatalf("state before = %#v, want managed previous state", got)
	}
}

func TestCaptureRecoveryStateBeforeAcceptsUnchangedIdentity(t *testing.T) {
	sharedHash := testContentHashString("shared")
	previousState := resourceState(defaultRecoveryEntry(), sharedHash)
	action := pathMutationFromManagedState(t, previousState)
	state := recoveryManagedStateMap(previousState)

	got, err := captureRecoveryStateBefore(action, state)
	if err != nil {
		t.Fatalf("captureRecoveryStateBefore returned error: %v", err)
	}
	if !got.Managed || got.ContentHash != sharedHash {
		t.Fatalf("state before = %#v, want managed unchanged state", got)
	}
}

func TestCaptureRecoveryStateBeforeRejectsMissingPreviousIdentity(t *testing.T) {
	sharedHash := testContentHashString("shared")
	previousState := resourceState(defaultRecoveryEntry(), sharedHash)
	action := pathMutationFromManagedState(t, previousState)
	postEntry := recoveryEntryFor(
		"relocated",
		"CLAUDE.md",
		sharedHash,
		sharedHash,
		"backup-0000",
	)
	postState := resourceState(postEntry, sharedHash)
	state := recoveryManagedStateMap(postState)

	_, err := captureRecoveryStateBefore(action, state)
	if err == nil || !strings.Contains(err.Error(), "missing state observation") {
		t.Fatalf("error = %v, want missing previous identity rejection", err)
	}
}

func TestCaptureRecoveryStateBeforeRejectsPreviousHashMismatch(t *testing.T) {
	previousState := resourceState(defaultRecoveryEntry(), testContentHashString("expected"))
	action := pathMutationFromManagedState(t, previousState)
	observed := resourceState(defaultRecoveryEntry(), testContentHashString("observed"))
	state := recoveryManagedStateMap(observed)

	_, err := captureRecoveryStateBefore(action, state)
	if err == nil || !strings.Contains(err.Error(), "does not match action previous state hash") {
		t.Fatalf("error = %v, want previous hash mismatch rejection", err)
	}
}

func pathMutationFromManagedState(t *testing.T, previous durable.ManagedPathState) pathMutation {
	t.Helper()
	return pathMutation{
		Kind:            CaptureRecord,
		Subject:         previous.Subject(),
		ConsumerTargets: previous.ConsumerTargets(),
		Scope:           previous.Scope(),
		Destination:     previous.Destination(),
		DesiredHash:     previous.ContentHash(),
		LiveHash:        previous.ContentHash(),
		LivePathHash:    previous.ContentHash(),
		ContentKind:     previous.ContentKind(),
		PreviousState:   previousPathStateFromManaged(previous),
	}
}

func recoveryManagedStateMap(states ...durable.ManagedPathState) map[recoveryStateKey]recoveryManagedState {
	result := make(map[recoveryStateKey]recoveryManagedState, len(states))
	for _, state := range states {
		result[recoveryStateKeyForManagedPath(state)] = recoveryManagedState{
			identity:    recoveryStateIdentityFromManagedPath(state),
			contentHash: string(state.ContentHash()),
		}
	}
	return result
}
