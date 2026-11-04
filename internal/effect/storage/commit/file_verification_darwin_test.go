package commit

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func TestDarwinRetainedDescriptorInspectsUnreadableMetadata(t *testing.T) {
	for _, mode := range []fs.FileMode{0o000, 0o200} {
		t.Run(fmt.Sprintf("%04o", mode), func(t *testing.T) {
			file, err := os.CreateTemp(t.TempDir(), "metadata")
			if err != nil {
				t.Fatal(err)
			}
			defer file.Close()
			t.Cleanup(func() { _ = os.Chmod(file.Name(), 0o600) })
			if err := file.Chmod(mode); err != nil {
				t.Fatal(err)
			}
			if err := verifyPreservedMetadata(int(file.Fd()), preservedMetadata{}); err != nil {
				t.Fatal("retained descriptor could not inspect metadata", err)
			}
		})
	}
}

func TestDarwinUnreadablePublicationRevalidatesFileFlags(t *testing.T) {
	root := canonicalTempDir(t)
	path := filepath.Join(root, "config")
	t.Cleanup(func() { _ = unix.Chflags(path, 0); _ = os.Chmod(path, 0o600) })
	request, err := NewFileCreate(path, []byte("after"), 0o200)
	if err != nil {
		t.Fatal(err)
	}
	var mutationErr error
	faults := faultPlan{actions: map[phase]func(){phaseSyncParent: func() {
		mutationErr = unix.Chflags(path, unix.UF_NODUMP)
	}}}
	err = commitFileWithFaults(t.Context(), request, faults)
	if mutationErr != nil {
		t.Fatal(mutationErr)
	}
	assertFailure(t, err, failureIndeterminateCommit, phaseVerifyEntry)
	assertNoPrivateEntries(t, root)
}
