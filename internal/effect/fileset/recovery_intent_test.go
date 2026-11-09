package fileset

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestContainingIntentRefusesDifferentMarkerWithoutRestoringFiles(t *testing.T) {
	for _, change := range []struct {
		name   string
		mutate func([]RecoveryTarget) []RecoveryTarget
	}{
		{"before hash", func(values []RecoveryTarget) []RecoveryTarget {
			values[0].BeforeHash = hashBytes([]byte("other"))
			return values
		}},
		{"before mode", func(values []RecoveryTarget) []RecoveryTarget { values[0].BeforeMode ^= 0o100; return values }},
		{"after hash", func(values []RecoveryTarget) []RecoveryTarget {
			values[0].AfterHash = hashBytes([]byte("other"))
			return values
		}},
		{"absent before", func(values []RecoveryTarget) []RecoveryTarget {
			values[0].BeforeExists, values[0].BeforeHash, values[0].BeforeMode = false, "", 0
			return values
		}},
		{"retained", func(values []RecoveryTarget) []RecoveryTarget {
			values[0].Write, values[0].AfterHash = false, ""
			return values
		}},
		{"commit point", func(values []RecoveryTarget) []RecoveryTarget { values[1].CommitPoint = false; return values }},
		{"target", func(values []RecoveryTarget) []RecoveryTarget {
			values[0].Path = filepath.Join(filepath.Dir(values[0].Path), "other")
			return values
		}},
		{"missing target", func(values []RecoveryTarget) []RecoveryTarget { return values[:1] }},
		{"duplicate", func(values []RecoveryTarget) []RecoveryTarget { values[0] = values[1]; return values }},
		{"malformed hash", func(values []RecoveryTarget) []RecoveryTarget { values[0].AfterHash = "invalid"; return values }},
	} {
		t.Run(change.name, func(t *testing.T) {
			stateDir, declaration, state, expected := recoveryIntentFixture(t)
			writeFixture(t, declaration.Path(), "declaration-after", 0o640)
			values := change.mutate(append([]RecoveryTarget(nil), expected...))
			if disposition, err := InspectFileSetRecoveryForIntent(t.Context(), stateDir, values); !errors.Is(err, ErrRecoveryIntentMismatch) || disposition != "" {
				t.Error("different containing intent returned a recovery decision", disposition, err)
			}
			if err := RecoverFileSetForIntent(t.Context(), stateDir, values); !errors.Is(err, ErrRecoveryIntentMismatch) {
				t.Fatal("different containing intent authorized restoration", err)
			}
			assertContent(t, declaration.Path(), "declaration-after")
			assertContent(t, state.Path(), "state-before")
			if _, err := os.Stat(markerPath(stateDir)); err != nil {
				t.Fatal("refused recovery lost its marker", err)
			}
		})
	}
}

func TestContainingIntentSettlesFileSetBeforeChoosingSnapshotPhase(t *testing.T) {
	stateDir, declaration, state, expected := recoveryIntentFixture(t)
	writeFixture(t, state.Path(), "state-after", expected[1].BeforeMode)
	disposition, err := InspectFileSetRecoveryForIntent(t.Context(), stateDir, []RecoveryTarget{expected[1], expected[0]})
	if err != nil || disposition != RecoveryRollback {
		t.Fatal("partial rollback with provisional state after did not select restoration", disposition, err)
	}
	if err := RecoverFileSetForIntent(t.Context(), stateDir, expected); err != nil {
		t.Fatal(err)
	}
	assertContent(t, declaration.Path(), "declaration-before")
	assertContent(t, state.Path(), "state-before")
	assertMode(t, declaration.Path(), expected[0].BeforeMode)
	assertMode(t, state.Path(), expected[1].BeforeMode)
	assertMissing(t, transactionDir(stateDir))
	if err := RecoverFileSetForIntent(t.Context(), stateDir, expected); err != nil {
		t.Fatal("completed recovery was not an evidence-clear no-op", err)
	}
	if disposition, err := InspectFileSetRecoveryForIntent(t.Context(), stateDir, expected); err == nil || disposition != "" {
		t.Fatal("absent marker produced a recovery decision", disposition, err)
	}
	invalid := append([]RecoveryTarget(nil), expected...)
	invalid[0].BeforeMode = 0
	if disposition, err := InspectFileSetRecoveryForIntent(t.Context(), stateDir, invalid); !errors.Is(err, ErrRecoveryIntentMismatch) || disposition != "" {
		t.Fatal("invalid mode became marker-absence evidence", disposition, err)
	}
	if err := RecoverFileSetForIntent(t.Context(), stateDir, invalid); !errors.Is(err, ErrRecoveryIntentMismatch) {
		t.Fatal("invalid intent was accepted as a completed no-op", err)
	}
}

func TestContainingIntentFinalizesAllAfter(t *testing.T) {
	stateDir, declaration, state, expected := recoveryIntentFixture(t)
	writeFixture(t, declaration.Path(), "declaration-after", expected[0].BeforeMode)
	writeFixture(t, state.Path(), "state-after", expected[1].BeforeMode)
	disposition, err := InspectFileSetRecoveryForIntent(t.Context(), stateDir, expected)
	if err != nil || disposition != RecoveryFinalize {
		t.Fatal("complete local publication was not finalizable", disposition, err)
	}
	if err := RecoverFileSetForIntent(t.Context(), stateDir, expected); err != nil {
		t.Fatal(err)
	}
	assertContent(t, declaration.Path(), "declaration-after")
	assertContent(t, state.Path(), "state-after")
	assertMissing(t, transactionDir(stateDir))
}

func recoveryIntentFixture(t *testing.T) (string, FileTarget, FileTarget, []RecoveryTarget) {
	t.Helper()
	stateDir, declaration, state, expected := intentTargetFixture(t)
	targets, err := canonicalTargets([]FileTarget{state, declaration})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := prepareMarker(t.Context(), stateDir, targets); err != nil {
		t.Fatal(err)
	}
	return stateDir, declaration, state, expected
}

func intentTargetFixture(t *testing.T) (string, FileTarget, FileTarget, []RecoveryTarget) {
	t.Helper()
	root := t.TempDir()
	stateDir := mustCanonicalStateDir(t, filepath.Join(root, ".daem"))
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	declaration := mustWriteTarget(t, filepath.Join(root, "manifest.toml"), "declaration-after")
	state := mustCommitPointTarget(t, filepath.Join(stateDir, "state.json"), "state-after")
	writeFixture(t, declaration.Path(), "declaration-before", 0o640)
	writeFixture(t, state.Path(), "state-before", 0o640)
	declarationInfo, err := os.Stat(declaration.Path())
	if err != nil {
		t.Fatal(err)
	}
	stateInfo, err := os.Stat(state.Path())
	if err != nil {
		t.Fatal(err)
	}
	expected := []RecoveryTarget{
		{Path: declaration.Path(), BeforeExists: true, BeforeHash: hashBytes([]byte("declaration-before")), BeforeMode: declarationInfo.Mode().Perm(), AfterHash: hashBytes([]byte("declaration-after")), Write: true},
		{Path: state.Path(), BeforeExists: true, BeforeHash: hashBytes([]byte("state-before")), BeforeMode: stateInfo.Mode().Perm(), AfterHash: hashBytes([]byte("state-after")), Write: true, CommitPoint: true},
	}
	return stateDir, declaration, state, expected
}
