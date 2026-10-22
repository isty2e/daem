package transaction

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestLinkedDeclarationsPreserveSelectedEntries(t *testing.T) {
	for _, parentLink := range []bool{false, true} {
		for _, manifestLink := range []bool{false, true} {
			for _, lockLink := range []bool{false, true} {
				name := fmt.Sprintf("parent=%t/manifest=%t/lock=%t", parentLink, manifestLink, lockLink)
				t.Run(name, func(t *testing.T) {
					root := t.TempDir()
					project := filepath.Join(root, "project")
					dot := filepath.Join(root, "dot")
					for _, dir := range []string{project, dot} {
						if err := os.Mkdir(dir, 0o700); err != nil {
							t.Fatal(err)
						}
					}
					selectedRoot := project
					if parentLink {
						selectedRoot = filepath.Join(root, "alias")
						if err := os.Symlink("project", selectedRoot); err != nil {
							t.Fatal(err)
						}
					}
					manifest := filepath.Join(selectedRoot, "daem.toml")
					lockfile := filepath.Join(selectedRoot, "daem.lock.toml")
					for _, file := range []struct {
						path string
						link bool
					}{
						{manifest, manifestLink}, {lockfile, lockLink},
					} {
						actual := file.path
						if file.link {
							actual = filepath.Join(dot, filepath.Base(file.path))
							if err := os.Symlink(filepath.Join("..", "dot", filepath.Base(file.path)), file.path); err != nil {
								t.Fatal(err)
							}
						}
						writeAdapterFixture(t, actual, "before", 0o640)
					}

					err := CommitTransaction(context.Background(), TransactionInput{
						ManifestTarget: captureAdapterTarget(t, manifest),
						LockfileTarget: captureAdapterTarget(t, lockfile),
						StateDir:       filepath.Join(selectedRoot, ".daem"),
						ManifestBytes:  []byte("manifest-after"),
						LockfileBytes:  []byte("lock-after"),
					})
					if err != nil {
						t.Fatal(err)
					}
					assertAdapterContent(t, manifest, "manifest-after")
					assertAdapterContent(t, lockfile, "lock-after")
					for _, file := range []struct {
						path string
						link bool
					}{
						{manifest, manifestLink}, {lockfile, lockLink},
					} {
						info, err := os.Lstat(file.path)
						if err != nil {
							t.Fatal(err)
						}
						if (info.Mode()&os.ModeSymlink != 0) != file.link {
							t.Fatalf("selected entry type changed: %s", file.path)
						}
						if file.link {
							link, err := os.Readlink(file.path)
							if err != nil || link != filepath.Join("..", "dot", filepath.Base(file.path)) {
								t.Fatalf("link changed: %q, %v", link, err)
							}
						}
						actual, err := os.Stat(file.path)
						if err != nil || actual.Mode().Perm() != 0o640 {
							t.Fatalf("referent mode changed: %v, %v", actual, err)
						}
					}
				})
			}
		}
	}
}
