package lock

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/effect/mutation"
)

func TestLockResultPreparationPrecedesPublication(t *testing.T) {
	for _, previous := range []bool{false, true} {
		for _, changeManifest := range []bool{false, true} {
			name := "reject response"
			if changeManifest {
				name = "changed input"
			}
			if previous {
				name += " with prior lock"
			}
			t.Run(name, func(t *testing.T) {
				root := t.TempDir()
				t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
				manifest := filepath.Join(root, "daem.toml")
				const declaration = "version = 1\ntargets = [\"codex\"]\n\n[instructions.project]\nsource = \"instructions.md\"\n"
				writeWorkflowTestFile(t, root, "daem.toml", declaration)
				writeWorkflowTestFile(t, root, "instructions.md", "before\n")
				lockPath := filepath.Join(root, "daem.lock.toml")
				var before []byte
				if previous {
					if _, err := RunLock(t.Context(), LockInput{ManifestPath: manifest}); err != nil {
						t.Fatal(err)
					}
					var err error
					before, err = os.ReadFile(lockPath)
					if err != nil {
						t.Fatal(err)
					}
					writeWorkflowTestFile(t, root, "instructions.md", "after\n")
				}

				preparationError := errors.New("response preparation refused")
				calls := 0
				_, err := RunLock(t.Context(), LockInput{
					ManifestPath: manifest,
					PrepareResult: func(result Result) error {
						calls++
						if result.LockfilePath != lockPath || !result.Delta.HasChanges() {
							t.Fatal("preparation did not receive the actual changed lock result")
						}
						if changeManifest {
							writeWorkflowTestFile(t, root, "daem.toml", declaration+"\n# new revision\n")
							return nil
						}
						return preparationError
					},
				})
				if calls != 1 {
					t.Fatalf("preparation ran %d times", calls)
				}
				if changeManifest {
					var stale mutation.StaleSnapshotError
					if !errors.As(err, &stale) {
						t.Fatalf("changed input was not refused as stale: %v", err)
					}
				} else if !errors.Is(err, preparationError) {
					t.Fatalf("preparation error was not preserved: %v", err)
				}

				after, readErr := os.ReadFile(lockPath)
				if previous {
					if readErr != nil || !bytes.Equal(before, after) {
						t.Fatal("refused result changed the existing lockfile", readErr)
					}
				} else if !errors.Is(readErr, os.ErrNotExist) {
					t.Fatal("refused result published a new lockfile", readErr)
				}
			})
		}
	}
}
