package migrate

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/effect/fileset"
	"github.com/isty2e/daem/internal/effect/mutation"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/recoverygate"
	"github.com/isty2e/daem/test/testkit/metadatatx"
)

func captureImage(t *testing.T, path string) *metadatatx.Image {
	t.Helper()
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return &metadatatx.Image{Content: content, Mode: info.Mode().Perm()}
}

func interruptMigration(t *testing.T, paths, legacy daempaths.Paths, prefix int) []metadatatx.Write {
	t.Helper()
	prepared, err := PlanState(t.Context(), StateInput{ManifestPath: paths.ManifestPath})
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.Close()
	writes := make([]metadatatx.Write, 0, 4)
	for _, path := range prepared.targetPaths() {
		writes = append(writes, metadatatx.Write{Path: path, Before: captureImage(t, path), CommitPoint: path == legacy.StatefilePath})
	}
	if _, err := prepared.Execute(t.Context()); err != nil {
		t.Fatal(err)
	}
	for index := range writes {
		writes[index].After = captureImage(t, writes[index].Path).Content
	}
	metadatatx.WritePrefix(t, paths.StateDir, writes, prefix)
	return writes
}

func TestStateMigrationRecoversEveryVisibleWritePrefix(t *testing.T) {
	for _, registryOnly := range []bool{false, true} {
		for prefix := 0; prefix <= 4; prefix++ {
			t.Run(fmt.Sprintf("registry_only_%t/prefix_%d", registryOnly, prefix), func(t *testing.T) {
				paths, legacy := migrationFixture(t)
				pathsForFence := paths
				pathsForFence.LegacyUserStateDir = ""
				foreignOutput, _, installed := seedClaims(t, paths, legacy)
				if registryOnly {
					keepForeignOutputOnly(t, paths, foreignOutput)
					if err := os.Remove(legacy.StatefilePath); err != nil {
						t.Fatal(err)
					}
				}
				outputBefore, err := os.Lstat(installed)
				if err != nil {
					t.Fatal(err)
				}

				writes := interruptMigration(t, paths, legacy, prefix)
				if err := recoverygate.RequireClear(t.Context(), paths); err == nil {
					t.Fatal("partial migration did not fence reads")
				}
				if authority, err := recoverygate.NewEffectAuthority(t.Context(), paths); err == nil {
					if err := authority.Validate(t.Context()); err == nil {
						t.Fatal("partial migration admitted normal effects")
					}
				}
				if _, err := PlanState(t.Context(), StateInput{ManifestPath: paths.ManifestPath}); err == nil {
					t.Fatal("retry bypassed recovery")
				}
				recovery, err := PlanState(t.Context(), StateInput{ManifestPath: paths.ManifestPath, Recover: true})
				if err != nil {
					t.Fatal(err)
				}
				defer recovery.Close()
				want := string(fileset.RecoveryRollback)
				if prefix == 4 {
					want = string(fileset.RecoveryFinalize)
				}
				if recovery.Disclosure().Action != want {
					t.Fatalf("action = %s, want %s", recovery.Disclosure().Action, want)
				}
				if err := recoverygate.RequireFileSetClear(t.Context(), pathsForFence); err == nil {
					t.Fatal("recovery preview removed evidence")
				}
				if _, err := recovery.Execute(t.Context()); err != nil {
					t.Fatal(err)
				}
				if err := recoverygate.RequireFileSetClear(t.Context(), pathsForFence); err != nil {
					t.Fatal(err)
				}
				for _, write := range writes {
					got := captureImage(t, write.Path)
					if prefix == 4 {
						if got == nil || !bytes.Equal(got.Content, write.After) {
							t.Fatalf("finalize changed %s", write.Path)
						}
					} else if write.Before == nil {
						if got != nil {
							t.Fatalf("rollback retained new target %s", write.Path)
						}
					} else if got == nil || !bytes.Equal(got.Content, write.Before.Content) || got.Mode != write.Before.Mode {
						t.Fatalf("rollback did not restore %s", write.Path)
					}
				}
				if prefix < 4 {
					retry, err := PlanState(t.Context(), StateInput{ManifestPath: paths.ManifestPath})
					if err != nil {
						t.Fatal(err)
					}
					defer retry.Close()
					if _, err := retry.Execute(t.Context()); err != nil {
						t.Fatal(err)
					}
				}
				outputAfter, err := os.Lstat(installed)
				if err != nil || !os.SameFile(outputBefore, outputAfter) || outputBefore.Mode() != outputAfter.Mode() {
					t.Fatalf("recovery changed installed output: %v", err)
				}
				content, err := os.ReadFile(installed)
				if err != nil || string(content) != "installed payload\n" {
					t.Fatalf("recovery changed payload: %v", err)
				}
			})
		}
	}
}

func TestStateMigrationRecoveryRejectsChangedTargetsAndBackups(t *testing.T) {
	for _, changed := range []string{"target_before_preview", "backup_before_preview", "target_after_preview"} {
		t.Run(changed, func(t *testing.T) {
			paths, legacy := migrationFixture(t)
			seedClaims(t, paths, legacy)
			interruptMigration(t, paths, legacy, 2)
			input := StateInput{ManifestPath: paths.ManifestPath, Recover: true}
			var prepared *PreparedState
			var err error
			if changed == "target_after_preview" {
				prepared, err = PlanState(t.Context(), input)
				if err != nil {
					t.Fatal(err)
				}
				defer prepared.Close()
			}

			corrupt := paths.CarrierClaimRegistryPath
			if changed == "backup_before_preview" {
				root, err := fileset.FileSetAuthorityPath(paths.StateDir)
				if err != nil {
					t.Fatal(err)
				}
				entries, err := os.ReadDir(root)
				if err != nil {
					t.Fatal(err)
				}
				corrupt = ""
				for _, entry := range entries {
					if strings.HasSuffix(entry.Name(), ".before") {
						corrupt = filepath.Join(root, entry.Name())
						break
					}
				}
				if corrupt == "" {
					t.Fatal("fixture has no backup")
				}
			}
			writeFixture(t, corrupt, []byte("changed evidence"))
			before := captureMetadata(t, paths, legacy)
			if prepared == nil {
				if _, err := PlanState(t.Context(), input); err == nil {
					t.Fatal("damaged evidence admitted")
				}
			} else {
				_, err := prepared.Execute(t.Context())
				var stale mutation.StaleSnapshotError
				if !errors.As(err, &stale) {
					t.Fatalf("error = %v, want stale snapshot", err)
				}
			}
			assertMetadata(t, before)
			paths.LegacyUserStateDir = ""
			if err := recoverygate.RequireFileSetClear(t.Context(), paths); err == nil {
				t.Fatal("damaged evidence was removed")
			}
		})
	}
}

func TestStateMigrationRecoveryRejectsUnrelatedTransaction(t *testing.T) {
	paths, _ := migrationFixture(t)
	pathsForFence := paths
	pathsForFence.LegacyUserStateDir = ""
	metadatatx.WriteInterruptedForAbsentTarget(t, paths.StateDir, filepath.Join(paths.StateDir, "unrelated"))
	if _, err := PlanState(t.Context(), StateInput{ManifestPath: paths.ManifestPath, Recover: true}); err == nil {
		t.Fatal("unrelated transaction admitted")
	}
	if err := recoverygate.RequireFileSetClear(t.Context(), pathsForFence); err == nil {
		t.Fatal("unrelated evidence removed")
	}
}
