package fileset

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestRollbackSkipsUnchangedReadonlyTarget(t *testing.T) {
	for _, readonlyName := range []string{"a", "b"} {
		t.Run(readonlyName, func(t *testing.T) {
			root := t.TempDir()
			stateDir := mustCanonicalStateDir(t, filepath.Join(root, ".daem"))
			first := filepath.Join(root, "a")
			second := filepath.Join(root, "b")
			writeFixture(t, first, "first-before", 0o640)
			writeFixture(t, second, "second-before", 0o600)
			targets := []FileTarget{
				mustWriteTarget(t, first, "first-after"),
				mustWriteTarget(t, second, "second-after"),
			}
			readonly := targets[0].Path()
			writable := targets[1].Path()
			if readonlyName == "b" {
				readonly, writable = writable, readonly
			}

			err := commitWithOperations(context.Background(), FileSetInput{
				StateDir: stateDir,
				Targets:  targets,
			}, operations{writeFile: func(ctx context.Context, path string, content []byte, mode os.FileMode) error {
				if path == readonly {
					return &os.PathError{Op: "write", Path: path, Err: syscall.EROFS}
				}
				return commitFile(ctx, path, content, mode)
			}})
			if !errors.Is(err, syscall.EROFS) {
				t.Fatalf("commit error = %v, want original readonly failure", err)
			}
			assertContent(t, first, "first-before")
			assertContent(t, second, "second-before")
			assertMode(t, first, 0o640)
			assertMode(t, second, 0o600)
			assertMissing(t, transactionDir(stateDir))

			err = CommitFileSet(context.Background(), FileSetInput{
				StateDir: stateDir,
				Targets: []FileTarget{
					mustWriteTarget(t, writable, "next-write"),
					mustRetainedTarget(t, readonly),
				},
			})
			if err != nil {
				t.Fatalf("subsequent write blocked: %v", err)
			}
			assertContent(t, writable, "next-write")
			assertMissing(t, transactionDir(stateDir))
		})
	}
}
