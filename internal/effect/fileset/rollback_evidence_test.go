package fileset

import (
	"context"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestRollbackDoesNotSkipChangedModeKindOrInvalidBackup(t *testing.T) {
	for _, change := range []string{"mode", "kind", "backup"} {
		t.Run(change, func(t *testing.T) {
			root := t.TempDir()
			stateDir := mustCanonicalStateDir(t, filepath.Join(root, ".daem"))
			first := filepath.Join(root, "a")
			second := filepath.Join(root, "b")
			other := filepath.Join(root, "other")
			writeFixture(t, first, "a-before", 0o600)
			writeFixture(t, second, "b-before", 0o600)
			writeFixture(t, other, "b-before", 0o600)
			targets := []FileTarget{mustWriteTarget(t, first, "a-after"), mustWriteTarget(t, second, "b-after")}
			err := commitWithOperations(t.Context(), FileSetInput{StateDir: stateDir, Targets: targets}, operations{
				writeFile: func(ctx context.Context, path string, content []byte, mode os.FileMode) error {
					if path != targets[1].Path() {
						return commitFile(ctx, path, content, mode)
					}
					switch change {
					case "mode":
						if err := os.Chmod(second, 0o640); err != nil {
							t.Fatal(err)
						}
					case "kind":
						if err := os.Remove(second); err != nil {
							t.Fatal(err)
						}
						if err := os.Symlink("other", second); err != nil {
							t.Fatal(err)
						}
					case "backup":
						marker, err := loadMarker(ctx, markerPath(stateDir))
						if err != nil {
							t.Fatal(err)
						}
						writeFixture(t, marker.Targets[1].Before.BackupPath, "corrupted", 0o600)
					}
					return syscall.EROFS
				},
			})
			if err == nil {
				t.Fatal("unsafe rollback was accepted")
			}
			if _, err := os.Lstat(transactionDir(stateDir)); err != nil {
				t.Fatalf("unresolved evidence was removed: %v", err)
			}
			assertContent(t, other, "b-before")
			assertContent(t, first, "a-after")
		})
	}
}
