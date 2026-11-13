//go:build darwin || linux

package commit

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRootedEmptyDirectoryCleanupNeverAdmitsChildren(t *testing.T) {
	for _, populated := range []bool{false, true} {
		t.Run(map[bool]string{false: "empty", true: "nonempty"}[populated], func(t *testing.T) {
			root := canonicalTempDir(t)
			name := ".daem-tmp-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
			path := filepath.Join(root, name)
			if err := os.Mkdir(path, 0o700); err != nil {
				t.Fatal(err)
			}
			if populated {
				writeTestFile(t, filepath.Join(path, "preserve"), "data", 0o600)
			}
			captured := captureRootForCommitTest(t, root)
			capability := rootedCapabilityForCommitTest(t, captured, name)
			identity, err := CaptureRootedEntryIdentity(t.Context(), capability)
			if err != nil {
				t.Fatal(err)
			}
			request, err := NewRootedEmptyDirectoryCleanup(capability, identity)
			if err != nil {
				t.Fatal(err)
			}
			_, err = CommitRootedEntryCleanup(t.Context(), request)
			if populated {
				if err == nil {
					t.Fatal("empty cleanup removed descendants")
				}
				assertFile(t, filepath.Join(path, "preserve"), "data", 0o600)
			} else if err != nil {
				t.Fatal(err)
			} else if _, err := os.Lstat(path); !os.IsNotExist(err) {
				t.Fatal("empty reserved directory remains", err)
			}
		})
	}
}
