//go:build darwin || linux

package recover

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/effect/fileset"
	journal "github.com/isty2e/daem/internal/effect/journal/recoveryoperation"
	"github.com/isty2e/daem/internal/effect/mutation"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/recoverygate"
	"github.com/isty2e/daem/test/testkit/metadatatx"
)

func TestDeclarationRecoveryRestoresPublishedManifest(t *testing.T) {
	paths := declarationRecoveryPaths(t)
	marker := metadatatx.WriteInterruptedPair(t, paths.StateDir, paths.ManifestPath, paths.LockfilePath, "manifest after\n", "lock after\n", false)
	prepared := prepareDeclarationRecovery(t, paths)
	plan, ok := prepared.DeclarationDisclosure()
	if !ok || plan.Action != fileset.RecoveryRollback || plan.RemovesLockfile {
		t.Fatalf("unexpected preview: %+v", plan)
	}

	result, err := Execute(t.Context(), prepared, ExecuteOptions{})
	if err != nil || result.DeclarationRecoveryPhase() != DeclarationRecoveryCompleted {
		t.Fatalf("rollback = %s, %v", result.DeclarationRecoveryPhase(), err)
	}
	assertRecoveryBytes(t, paths.ManifestPath, "manifest after\n# before\n")
	assertRecoveryBytes(t, paths.LockfilePath, "lock after\n# before\n")
	if _, err := os.Lstat(marker); !os.IsNotExist(err) {
		t.Fatalf("settled record remains: %v", err)
	}
	if next, err := Plan(t.Context(), PlanInput{ManifestPath: paths.ManifestPath, Declarations: true}); next != nil || !errors.Is(err, ErrNoDeclarationTransaction) {
		t.Fatalf("settled workspace selected a recovery: %v, %v", next, err)
	}
}

func TestDeclarationRecoveryRefusesChangedPreparedEvidence(t *testing.T) {
	for _, scenario := range []string{"changed disposition", "new record with same disposition", "replaced file with same bytes"} {
		t.Run(scenario, func(t *testing.T) {
			paths := declarationRecoveryPaths(t)
			marker := metadatatx.WriteInterruptedPair(t, paths.StateDir, paths.ManifestPath, paths.LockfilePath, "manifest after\n", "lock after\n", scenario == "changed disposition")
			prepared := prepareDeclarationRecovery(t, paths)

			switch scenario {
			case "changed disposition":
				writeRecoverTestFile(t, paths.LockfilePath, []byte("lock after\n# before\n"))
			case "new record with same disposition":
				record := filepath.Dir(marker)
				retiredRecord := filepath.Join(paths.ManifestRoot, "other-record")
				if err := os.Rename(record, retiredRecord); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(record, 0o700); err != nil {
					t.Fatal(err)
				}
				entries, err := os.ReadDir(retiredRecord)
				if err != nil {
					t.Fatal(err)
				}
				for _, entry := range entries {
					content, err := os.ReadFile(filepath.Join(retiredRecord, entry.Name()))
					if err != nil {
						t.Fatal(err)
					}
					writeRecoverTestFile(t, filepath.Join(record, entry.Name()), content)
				}
			case "replaced file with same bytes":
				if err := os.Rename(paths.ManifestPath, paths.ManifestPath+".previous"); err != nil {
					t.Fatal(err)
				}
				writeRecoverTestFile(t, paths.ManifestPath, []byte("manifest after\n"))
			}
			markerBytes, err := os.ReadFile(marker)
			if err != nil {
				t.Fatal(err)
			}

			result, err := Execute(t.Context(), prepared, ExecuteOptions{})
			var stale mutation.StaleSnapshotError
			if !errors.As(err, &stale) || result.DeclarationRecoveryPhase() != DeclarationRecoveryRefused {
				t.Fatalf("changed evidence = %s, %v", result.DeclarationRecoveryPhase(), err)
			}
			assertRecoveryBytes(t, marker, string(markerBytes))
			assertRecoveryBytes(t, paths.ManifestPath, "manifest after\n")
			assertRecoveryBytes(t, paths.LockfilePath, "lock after\n# before\n")
		})
	}
}

func TestDeclarationRecoveryDoesNotSelectApplyJournal(t *testing.T) {
	fixture := prepareRecoveryFixture(t, true)
	input := fixture.input
	input.Declarations = true
	prepared, err := Plan(t.Context(), input)
	state := recoverygate.StateOf(err)
	if prepared != nil || !state.JournalKnown() || state.Journal() != journal.InterruptionActiveApply {
		t.Fatalf("explicit declaration selection did not stop at the active journal: %v, %v", prepared, err)
	}
	if _, err := os.Stat(fixture.operationDir); err != nil {
		t.Fatalf("declaration selection did not preserve the journal: %v", err)
	}
}

func declarationRecoveryPaths(t *testing.T) daempaths.Paths {
	t.Helper()
	root := t.TempDir()
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
	paths, err := daempaths.Resolve(filepath.Join(root, "daem.toml"))
	if err != nil {
		t.Fatal(err)
	}
	return paths
}

func prepareDeclarationRecovery(t *testing.T, paths daempaths.Paths) *PreparedRecovery {
	t.Helper()
	prepared, err := Plan(t.Context(), PlanInput{ManifestPath: paths.ManifestPath, Declarations: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = prepared.Close() })
	return prepared
}

func assertRecoveryBytes(t *testing.T, path, want string) {
	t.Helper()
	if got, err := os.ReadFile(path); err != nil || string(got) != want {
		t.Fatalf("read %q = %q, %v; want %q", path, got, err, want)
	}
}
