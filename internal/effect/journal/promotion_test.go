package journal

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/assurance/pathauthority"
	"github.com/isty2e/daem/internal/assurance/pathauthority/pathtest"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	ownershipmutation "github.com/isty2e/daem/internal/effect/mutation/ownership"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/output"
	outputownership "github.com/isty2e/daem/internal/output/ownership"
)

func TestPromoteProvisionalAcquireReplacesExactlyOneIntent(t *testing.T) {
	fixture := newPromotionFixture(t, journalTestFilesystem())
	before, err := os.ReadFile(fixture.recordPath)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := PromoteProvisionalAcquire(
		t.Context(),
		fixture.filesystem,
		fixture.authority,
		fixture.directoryAuthority,
		fixture.activeAuthority,
		"sha256:"+strings.Repeat("0", 64),
		fixture.intent,
		fixture.transition,
		testStateCodec(),
	); err == nil || !strings.Contains(err.Error(), "changed before ownership intent promotion") {
		t.Fatalf("stale promotion error = %v", err)
	}
	unchanged, err := os.ReadFile(fixture.recordPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(unchanged) != string(before) {
		t.Fatal("stale promotion changed the journal record")
	}

	promotion, err := PromoteProvisionalAcquire(
		t.Context(),
		fixture.filesystem,
		fixture.authority,
		fixture.directoryAuthority,
		fixture.activeAuthority,
		fixture.fingerprint,
		fixture.intent,
		fixture.transition,
		testStateCodec(),
	)
	if err != nil {
		t.Fatalf("PromoteProvisionalAcquire returned error: %v", err)
	}
	refreshedAuthority, available := promotion.ActiveJournalAuthority()
	if !available {
		t.Fatal("PromoteProvisionalAcquire did not return refreshed active authority")
	}
	if err := ValidateActiveJournalAuthority(
		t.Context(),
		fixture.filesystem,
		fixture.directoryAuthority,
		refreshedAuthority,
	); err != nil {
		t.Fatalf("refreshed active authority: %v", err)
	}
	if err := ValidateActiveJournalAuthority(
		t.Context(),
		fixture.filesystem,
		fixture.directoryAuthority,
		fixture.activeAuthority,
	); err == nil {
		t.Fatal("pre-promotion active authority remained valid after record replacement")
	}
	content, err := os.ReadFile(fixture.recordPath)
	if err != nil {
		t.Fatal(err)
	}
	if promotion.RecordFingerprint() != recoveryJournalRecordFingerprint(content) ||
		promotion.RecordFingerprint() == fixture.fingerprint {
		t.Fatalf(
			"promoted fingerprint = %q, initial = %q",
			promotion.RecordFingerprint(),
			fixture.fingerprint,
		)
	}
	capability, _, loaded, _, err := readPromotionRecord(
		t.Context(), fixture.filesystem, fixture.authority, testStateCodec(),
	)
	if err != nil {
		t.Fatalf("load promoted journal: %v", err)
	}
	if err := capability.Close(); err != nil {
		t.Fatal(err)
	}
	intents, err := canonicalProvisionalAcquireIntents(loaded.ProvisionalAcquires)
	if err != nil {
		t.Fatal(err)
	}
	transitions, err := canonicalClaimTransitions(loaded.ClaimTransitions)
	if err != nil {
		t.Fatal(err)
	}
	if len(intents) != 0 || len(transitions) != 1 || !transitions[0].Equal(fixture.transition) {
		t.Fatalf("promoted journal intents = %d, transitions = %#v", len(intents), transitions)
	}
}

func TestPromoteProvisionalAcquireIndeterminateReplacementRetainsClassifiableJournal(t *testing.T) {
	base := journalTestFilesystem()
	filesystem := &indeterminatePromotionFilesystem{Store: base}
	fixture := newPromotionFixture(t, filesystem)

	promotion, err := PromoteProvisionalAcquire(
		t.Context(),
		filesystem,
		fixture.authority,
		fixture.directoryAuthority,
		fixture.activeAuthority,
		fixture.fingerprint,
		fixture.intent,
		fixture.transition,
		testStateCodec(),
	)
	if err == nil || !strings.Contains(err.Error(), "indeterminate") {
		t.Fatalf("indeterminate promotion error = %v", err)
	}
	if _, available := promotion.ActiveJournalAuthority(); !available {
		t.Fatal("indeterminate promotion lost refreshed active journal authority")
	}
	capability, _, loaded, _, loadErr := readPromotionRecord(
		t.Context(), base, fixture.authority, testStateCodec(),
	)
	if loadErr != nil {
		t.Fatalf("load journal after indeterminate replacement: %v", loadErr)
	}
	if closeErr := capability.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	intents, canonicalErr := canonicalProvisionalAcquireIntents(loaded.ProvisionalAcquires)
	if canonicalErr != nil {
		t.Fatal(canonicalErr)
	}
	transitions, canonicalErr := canonicalClaimTransitions(loaded.ClaimTransitions)
	if canonicalErr != nil {
		t.Fatal(canonicalErr)
	}
	if len(intents) != 0 || len(transitions) != 1 || !transitions[0].Equal(fixture.transition) {
		t.Fatalf("indeterminate journal intents = %d, transitions = %#v", len(intents), transitions)
	}
}

type promotionFixture struct {
	filesystem         mutationfs.Store
	authority          *rootedpath.EntryAuthority
	directoryAuthority *rootedpath.EntryAuthority
	activeAuthority    ActiveJournalAuthority
	recordPath         string
	fingerprint        string
	intent             outputownership.ProvisionalAcquireIntent
	transition         ownershipmutation.ClaimTransition
}

func newPromotionFixture(t *testing.T, filesystem mutationfs.Store) promotionFixture {
	t.Helper()
	selectedRoot := t.TempDir()
	operationDir := filepath.Join(selectedRoot, "recovery", testOperationID)
	if err := os.MkdirAll(operationDir, 0o700); err != nil {
		t.Fatal(err)
	}
	recordPath := filepath.Join(operationDir, recoveryJournalFileName)
	destination, err := output.Parse("~/.codex/AGENTS.md")
	if err != nil {
		t.Fatal(err)
	}
	namespace := filepath.Join(selectedRoot, "global")
	if err := os.Mkdir(namespace, 0o700); err != nil {
		t.Fatal(err)
	}
	candidate := filepath.Join(namespace, "AG\u00c9NTS.md")
	provisional, err := pathauthority.NewProvisional(
		candidate,
		pathtest.DarwinCaseSensitive(candidate).Witness(),
		namespace,
		pathtest.DarwinCaseSensitive(namespace).Witness(),
	)
	if err != nil {
		t.Fatal(err)
	}
	statefile, err := pathauthority.NewExact(filepath.Join(selectedRoot, "state.json"), "exact-v1:")
	if err != nil {
		t.Fatal(err)
	}
	owner, err := stateauthority.New(statefile, filepath.Join(selectedRoot, "daem.toml"))
	if err != nil {
		t.Fatal(err)
	}
	intent, err := outputownership.NewProvisionalAcquireIntent(
		destination,
		"",
		provisional,
		owner,
		testOperationID,
	)
	if err != nil {
		t.Fatal(err)
	}
	exact, err := pathauthority.NewExact(candidate, pathtest.DarwinCaseSensitive(candidate).Witness())
	if err != nil {
		t.Fatal(err)
	}
	address, err := outputownership.NewManagedAddress(exact, "")
	if err != nil {
		t.Fatal(err)
	}
	transition, err := ownershipmutation.NewAcquireTransitionFromIntent(intent, address)
	if err != nil {
		t.Fatal(err)
	}

	entry := globalAcquireRecoveryEntry(t)
	journal := recoveryJournalFor(entry)
	journal.ProvisionalAcquires, err = recoveryProvisionalAcquireIntents(
		[]outputownership.ProvisionalAcquireIntent{intent},
	)
	if err != nil {
		t.Fatal(err)
	}
	content, err := marshalRecoveryJournalWithStateContentForPromotion(journal, testStateCodec())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(recordPath, content, recoveryJournalMode); err != nil {
		t.Fatal(err)
	}
	capturedRoot, err := rootedpath.CaptureRoot(selectedRoot)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = capturedRoot.Close() })
	authority, err := rootedpath.BindSelectedEntryAuthority(capturedRoot, selectedRoot, recordPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = authority.Close() })
	directoryAuthority, err := rootedpath.BindSelectedEntryAuthority(
		capturedRoot,
		selectedRoot,
		operationDir,
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = directoryAuthority.Close() })
	activeAuthority, err := CaptureActiveJournalAuthority(
		t.Context(),
		filesystem,
		directoryAuthority,
	)
	if err != nil {
		t.Fatal(err)
	}
	return promotionFixture{
		filesystem:         filesystem,
		authority:          authority,
		directoryAuthority: directoryAuthority,
		activeAuthority:    activeAuthority,
		recordPath:         recordPath,
		fingerprint:        recoveryJournalRecordFingerprint(content),
		intent:             intent,
		transition:         transition,
	}
}

type indeterminatePromotionFilesystem struct {
	mutationfs.Store
}

func (filesystem *indeterminatePromotionFilesystem) ReplaceRootedFileAndRefreshParent(
	ctx context.Context,
	capability rootedpath.CommitCapability,
	content []byte,
	mode os.FileMode,
	expected mutationfs.EntryIdentity,
	expectedParent mutationfs.EntryIdentity,
) (mutationfs.CommitOutcome, mutationfs.EntryIdentity, error) {
	_, refreshedParent, err := filesystem.Store.ReplaceRootedFileAndRefreshParent(
		ctx,
		capability,
		content,
		mode,
		expected,
		expectedParent,
	)
	if err != nil {
		return mutationfs.CommitOutcome{}, refreshedParent, err
	}
	outcome, err := mutationfs.NewCommitOutcome(mutationfs.CommitOutcomeIndeterminate, nil)
	if err != nil {
		return mutationfs.CommitOutcome{}, refreshedParent, err
	}
	return outcome, refreshedParent, errors.New("injected indeterminate journal promotion")
}
