package fileset

import (
	"errors"
	"testing"

	"github.com/isty2e/daem/internal/contractversion"
)

func TestContainingIntentPublishesFreshMatchingFiles(t *testing.T) {
	stateDir, declaration, state, expected := intentTargetFixture(t)
	if err := CommitFreshFileSetForIntent(t.Context(), FileSetInput{StateDir: stateDir, Targets: []FileTarget{state, declaration}}, expected); err != nil {
		t.Fatal(err)
	}
	assertContent(t, declaration.Path(), "declaration-after")
	assertContent(t, state.Path(), "state-after")
	assertMode(t, state.Path(), expected[1].BeforeMode)
	assertMissing(t, transactionDir(stateDir))
}

func TestContainingIntentRefusesDifferentCaptureBeforePublication(t *testing.T) {
	t.Run("external before edit", func(t *testing.T) {
		stateDir, declaration, state, expected := intentTargetFixture(t)
		writeFixture(t, declaration.Path(), "editor-change", expected[0].BeforeMode)
		err := CommitFreshFileSetForIntent(t.Context(), FileSetInput{StateDir: stateDir, Targets: []FileTarget{declaration, state}}, expected)
		if !errors.Is(err, ErrRecoveryIntentMismatch) {
			t.Fatal("changed before-image was not refused at evidence capture", err)
		}
		assertContent(t, declaration.Path(), "editor-change")
		assertContent(t, state.Path(), "state-before")
		assertMissing(t, transactionDir(stateDir))
		if err := rejectAbandonedFileSetResidue(t.Context(), stateDir); err != nil {
			t.Fatal("rejected capture left unowned preparation evidence", err)
		}
	})
	t.Run("different planned after", func(t *testing.T) {
		stateDir, declaration, state, expected := intentTargetFixture(t)
		different := mustWriteTarget(t, declaration.Path(), "different-after")
		err := CommitFreshFileSetForIntent(t.Context(), FileSetInput{StateDir: stateDir, Targets: []FileTarget{different, state}}, expected)
		if !errors.Is(err, ErrRecoveryIntentMismatch) {
			t.Fatal("different after-image was not refused before evidence publication", err)
		}
		assertContent(t, declaration.Path(), "declaration-before")
		assertContent(t, state.Path(), "state-before")
		assertMissing(t, transactionDir(stateDir))
		if err := rejectAbandonedFileSetResidue(t.Context(), stateDir); err != nil {
			t.Fatal("rejected after-image left unowned preparation evidence", err)
		}
	})
}

func TestContainingIntentDoesNotRecoverThenStartAnotherPublication(t *testing.T) {
	stateDir, declaration, state, expected := recoveryIntentFixture(t)
	writeFixture(t, declaration.Path(), "declaration-after", expected[0].BeforeMode)
	if err := CommitFreshFileSetForIntent(t.Context(), FileSetInput{StateDir: stateDir, Targets: []FileTarget{declaration, state}}, expected); !errors.Is(err, ErrInterruptedFileSetTransaction) {
		t.Fatal("fresh intent did not preserve the interrupted-operation refusal", err)
	}
	assertContent(t, declaration.Path(), "declaration-after")
	assertContent(t, state.Path(), "state-before")
	if _, err := loadMarker(t.Context(), markerPath(stateDir)); err != nil {
		t.Fatal("fresh refusal lost existing recovery evidence", err)
	}
}

func TestContainingIntentDoesNotSelectAnotherMarkerGeneration(t *testing.T) {
	stateDir, declaration, state, expected := intentTargetFixture(t)
	targets, err := canonicalRecoveryTargets(expected)
	if err != nil {
		t.Fatal(err)
	}
	err = commitWithOperationsIntentVersion(t.Context(), FileSetInput{StateDir: stateDir, Targets: []FileTarget{declaration, state}}, operations{writeFile: commitFile}, contractversion.MetadataTransactionReference, false, targets)
	if err == nil {
		t.Fatal("generation-1 intent selected a different marker/fence grammar")
	}
	assertContent(t, declaration.Path(), "declaration-before")
	assertContent(t, state.Path(), "state-before")
	assertMissing(t, transactionDir(stateDir))
}
